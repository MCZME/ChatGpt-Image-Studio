package api

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"chatgpt2api/internal/imageassets"
)

const defaultImageDir = imageassets.DefaultImageDir

// downloadAndCache downloads an upstream image using the image client's transport
// (Chrome TLS fingerprint), saves to local disk, and returns the local filename.
func downloadAndCache(client imageDownloader, upstreamURL string, cacheDir string) (string, error) {
	dir := firstNonEmpty(cacheDir, defaultImageDir)

	data, err := client.DownloadBytes(upstreamURL)
	if err != nil {
		return "", fmt.Errorf("download upstream image: %w", err)
	}
	info, err := imageassets.SaveImageBytes(dir, data, imageassets.FileSaveOptions{
		SourceKind:  "download",
		OriginalURL: upstreamURL,
	})
	if err != nil {
		return "", err
	}

	slog.Info("cached image", "file", info.Filename, "size", info.SizeBytes)
	return info.Filename, nil
}

// gatewayImageURL builds the public URL for a cached image.
func gatewayImageURL(r *http.Request, filename string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	return fmt.Sprintf("%s://%s/v1/files/image/%s", scheme, host, filename)
}

func (s *Server) resolveImageFilePath(name string) string {
	baseName := filepath.Base(strings.TrimSpace(name))
	primaryDir := s.cfg.ResolvePath(s.cfg.Storage.ImageDir)
	for _, dir := range imageassets.CandidateImageDirs(s.cfg.RootDir(), primaryDir) {
		candidate := filepath.Join(dir, baseName)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return s.searchImageFilePathFallback(baseName)
}

func (s *Server) searchImageFilePathFallback(name string) string {
	baseName := filepath.Base(strings.TrimSpace(name))
	if baseName == "" || s == nil || s.cfg == nil {
		return ""
	}

	dataRoot := filepath.Join(s.cfg.Paths().Root, "data")
	info, err := os.Stat(dataRoot)
	if err != nil || !info.IsDir() {
		return ""
	}

	var found string
	_ = filepath.WalkDir(dataRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || found != "" {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(entry.Name(), baseName) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		found = path
		return fs.SkipAll
	})
	return found
}

// handleImageFile serves cached and server-stored images from storage.image_dir.
func (s *Server) handleImageFile(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/v1/files/image/")
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	path := s.resolveImageFilePath(name)
	if path == "" {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}

	ext := strings.ToLower(filepath.Ext(path))
	contentTypes := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".webp": "image/webp",
		".gif":  "image/gif",
	}
	ct := contentTypes[ext]
	if ct == "" {
		ct = "image/png"
	}

	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", ct)
	http.ServeFile(w, r, path)
}
