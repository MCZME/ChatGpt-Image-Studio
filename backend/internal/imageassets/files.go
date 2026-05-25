package imageassets

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultImageDir    = "data/images"
	LegacyImageDir     = "data/tmp/image"
	ImageFileURLPrefix = "/v1/files/image/"
	defaultImageMIME   = "image/png"
)

type FileSaveOptions struct {
	SourceKind   string
	MIMEType     string
	OriginalName string
	OriginalURL  string
}

type FileInfo struct {
	Filename    string
	URL         string
	Path        string
	MIMEType    string
	SizeBytes   int64
	SHA256      string
	StorageKind string
	SourceKind  string
	OriginalURL string
}

func SaveImageBytes(dir string, payload []byte, options FileSaveOptions) (FileInfo, error) {
	if len(payload) == 0 {
		return FileInfo{}, fmt.Errorf("image is empty")
	}
	targetDir := strings.TrimSpace(dir)
	if targetDir == "" {
		targetDir = filepath.FromSlash(DefaultImageDir)
	}
	mimeType := DetectImageMIME(payload, options.MIMEType, options.OriginalName)
	shaHex := SHA256Hex(payload)
	sourceKind := sanitizeSourceKind(options.SourceKind)
	filename := fmt.Sprintf("%s-%s%s", sourceKind, shaHex, ExtensionForMIME(mimeType))
	path := filepath.Join(targetDir, filename)
	if err := writeFileAtomic(path, payload); err != nil {
		return FileInfo{}, err
	}
	return FileInfo{
		Filename:    filename,
		URL:         ImageFileURL(filename),
		Path:        path,
		MIMEType:    mimeType,
		SizeBytes:   int64(len(payload)),
		SHA256:      shaHex,
		StorageKind: "local",
		SourceKind:  sourceKind,
		OriginalURL: strings.TrimSpace(options.OriginalURL),
	}, nil
}

func SaveImageBase64(dir, raw string, options FileSaveOptions) (FileInfo, error) {
	payload, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return FileInfo{}, fmt.Errorf("decode image: %w", err)
	}
	return SaveImageBytes(dir, payload, options)
}

func SaveImageDataURL(dir, raw string, options FileSaveOptions) (FileInfo, error) {
	payload, mimeType, err := DecodeImageDataURL(raw)
	if err != nil {
		return FileInfo{}, err
	}
	if strings.TrimSpace(options.MIMEType) == "" {
		options.MIMEType = mimeType
	}
	return SaveImageBytes(dir, payload, options)
}

func DecodeImageDataURL(raw string) ([]byte, string, error) {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := raw[:comma]
	if !strings.Contains(strings.ToLower(meta), ";base64") {
		return nil, "", fmt.Errorf("only base64 data urls are supported")
	}
	mimeType := strings.TrimPrefix(strings.Split(meta, ";")[0], "data:")
	payload, err := base64.StdEncoding.DecodeString(raw[comma+1:])
	if err != nil {
		return nil, "", fmt.Errorf("decode data url: %w", err)
	}
	return payload, normalizeImageMIME(mimeType), nil
}

func InspectStoredImageURL(primaryDir string, fallbackDirs []string, rawURL string) FileInfo {
	filename := FilenameFromImageURL(rawURL)
	if filename == "" {
		return FileInfo{}
	}
	info := FileInfo{
		Filename:    filename,
		URL:         ImageFileURL(filename),
		StorageKind: "local",
		MIMEType:    DetectImageMIME(nil, "", filename),
		SourceKind:  sourceKindFromFilename(filename),
	}
	for _, dir := range uniqueDirs(append([]string{primaryDir}, fallbackDirs...)) {
		path := filepath.Join(dir, filename)
		stat, err := os.Stat(path)
		if err != nil || !stat.Mode().IsRegular() {
			continue
		}
		info.Path = path
		info.SizeBytes = stat.Size()
		if payload, err := os.ReadFile(path); err == nil {
			info.SHA256 = SHA256Hex(payload)
			info.MIMEType = DetectImageMIME(payload, info.MIMEType, filename)
			info.SizeBytes = int64(len(payload))
		}
		return info
	}
	return info
}

func CandidateImageDirs(rootDir, primaryDir string) []string {
	root := strings.TrimSpace(rootDir)
	if root == "" {
		root = "."
	}
	resolve := func(value string) string {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			return ""
		}
		if filepath.IsAbs(cleaned) {
			return filepath.Clean(cleaned)
		}
		return filepath.Clean(filepath.Join(root, filepath.FromSlash(cleaned)))
	}
	return uniqueDirs([]string{
		primaryDir,
		resolve(DefaultImageDir),
		resolve(LegacyImageDir),
	})
}

func DefaultImageDirPath(rootDir string) string {
	return resolveImageDir(rootDir, DefaultImageDir)
}

func LegacyImageDirPath(rootDir string) string {
	return resolveImageDir(rootDir, LegacyImageDir)
}

func IsDefaultImageDir(rootDir, dir string) bool {
	return samePath(resolveImageDir(rootDir, dir), DefaultImageDirPath(rootDir))
}

func MoveImageFiles(oldDir, newDir string) error {
	sourceDir := strings.TrimSpace(oldDir)
	targetDir := strings.TrimSpace(newDir)
	if sourceDir == "" || targetDir == "" {
		return nil
	}
	sourceDir = filepath.Clean(sourceDir)
	targetDir = filepath.Clean(targetDir)
	if samePath(sourceDir, targetDir) {
		return nil
	}
	info, err := os.Stat(sourceDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return err
	}

	targetInsideSource := isPathWithin(targetDir, sourceDir)
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if targetInsideSource && isPathWithin(path, targetDir) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, rel)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		targetPath, duplicate, err := resolveMoveTarget(path, targetPath)
		if err != nil {
			return err
		}
		if duplicate {
			return os.Remove(path)
		}
		if err := os.Rename(path, targetPath); err == nil {
			return nil
		}
		if err := copyFile(path, targetPath); err != nil {
			return err
		}
		return os.Remove(path)
	})
}

func FilenameFromImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	index := strings.LastIndex(trimmed, ImageFileURLPrefix)
	if index >= 0 {
		return filepath.Base(trimmed[index+len(ImageFileURLPrefix):])
	}
	return ""
}

func ImageFileURL(filename string) string {
	baseName := filepath.Base(strings.TrimSpace(filename))
	if baseName == "." || baseName == "" {
		return ""
	}
	return ImageFileURLPrefix + baseName
}

func SHA256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func DetectImageMIME(payload []byte, preferred, name string) string {
	if normalized := normalizeImageMIME(preferred); normalized != "" {
		return normalized
	}
	if fromName := mimeFromFilename(name); fromName != "" {
		return fromName
	}
	if len(payload) > 0 {
		if normalized := normalizeImageMIME(http.DetectContentType(payload)); normalized != "" {
			return normalized
		}
	}
	return defaultImageMIME
}

func ExtensionForMIME(mimeType string) string {
	switch normalizeImageMIME(mimeType) {
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".png"
	}
}

func writeFileAtomic(path string, payload []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	removeTmp = false
	return nil
}

func normalizeImageMIME(value string) string {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	if mediaType, _, err := mime.ParseMediaType(cleaned); err == nil {
		cleaned = mediaType
	}
	switch cleaned {
	case "image/jpg":
		return "image/jpeg"
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return cleaned
	default:
		return ""
	}
}

func mimeFromFilename(name string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	if ext == "" {
		return ""
	}
	if ext == ".jpg" {
		ext = ".jpeg"
	}
	return normalizeImageMIME(mime.TypeByExtension(ext))
}

func sanitizeSourceKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "source", "mask", "result", "download", "import":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "image"
	}
}

func sourceKindFromFilename(filename string) string {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(filename)))
	if dash := strings.Index(name, "-"); dash > 0 {
		return sanitizeSourceKind(name[:dash])
	}
	return ""
}

func uniqueDirs(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		cleaned := strings.TrimSpace(value)
		if cleaned == "" {
			continue
		}
		cleaned = filepath.Clean(cleaned)
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, cleaned)
	}
	return result
}

func resolveImageDir(rootDir, dir string) string {
	cleaned := strings.TrimSpace(dir)
	if cleaned == "" {
		cleaned = DefaultImageDir
	}
	if filepath.IsAbs(cleaned) {
		return filepath.Clean(cleaned)
	}
	root := strings.TrimSpace(rootDir)
	if root == "" {
		root = "."
	}
	return filepath.Clean(filepath.Join(root, filepath.FromSlash(cleaned)))
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func isPathWithin(path, dir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	if samePath(cleanPath, cleanDir) {
		return true
	}
	return strings.HasPrefix(cleanPath+string(os.PathSeparator), cleanDir+string(os.PathSeparator))
}

func resolveMoveTarget(sourcePath, targetPath string) (string, bool, error) {
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return "", false, err
	}
	sourceHash, err := fileSHA256Hex(sourcePath)
	if err != nil {
		return "", false, err
	}
	targetInfo, err := os.Stat(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return targetPath, false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !targetInfo.Mode().IsRegular() {
		return "", false, fmt.Errorf("target path exists and is not a file: %s", targetPath)
	}
	if matches, err := fileMatchesHash(targetPath, targetInfo, sourceInfo.Size(), sourceHash); err != nil {
		return "", false, err
	} else if matches {
		return "", true, nil
	}

	dir := filepath.Dir(targetPath)
	ext := filepath.Ext(targetPath)
	stem := strings.TrimSuffix(filepath.Base(targetPath), ext)
	hashSuffix := sourceHash
	if len(hashSuffix) > 12 {
		hashSuffix = hashSuffix[:12]
	}
	for index := 1; ; index++ {
		suffix := hashSuffix
		if index > 1 {
			suffix = fmt.Sprintf("%s-%d", hashSuffix, index)
		}
		candidate := filepath.Join(dir, stem+"-"+suffix+ext)
		candidateInfo, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, false, nil
		}
		if err != nil {
			return "", false, err
		}
		if matches, err := fileMatchesHash(candidate, candidateInfo, sourceInfo.Size(), sourceHash); err != nil {
			return "", false, err
		} else if matches {
			return "", true, nil
		}
	}
}

func fileMatchesHash(path string, info os.FileInfo, size int64, hash string) (bool, error) {
	if !info.Mode().IsRegular() || info.Size() != size {
		return false, nil
	}
	otherHash, err := fileSHA256Hex(path)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(otherHash, hash), nil
}

func fileSHA256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(sourcePath, targetPath string) error {
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	targetFile, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer targetFile.Close()

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return err
	}
	return nil
}
