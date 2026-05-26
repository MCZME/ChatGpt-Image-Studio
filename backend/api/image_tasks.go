package api

import (
	"context"
	"strings"
	"time"

	"chatgpt2api/internal/accounts"
	"chatgpt2api/internal/imagehistory"
)

type imageTaskStatus string

const (
	imageTaskStatusQueued          imageTaskStatus = "queued"
	imageTaskStatusRunning         imageTaskStatus = "running"
	imageTaskStatusSucceeded       imageTaskStatus = "succeeded"
	imageTaskStatusFailed          imageTaskStatus = "failed"
	imageTaskStatusCancelRequested imageTaskStatus = "cancel_requested"
	imageTaskStatusCancelled       imageTaskStatus = "cancelled"
	imageTaskStatusExpired         imageTaskStatus = "expired"
)

type imageTaskWaitingReason string

const (
	imageTaskWaitingReasonNone                  imageTaskWaitingReason = ""
	imageTaskWaitingReasonGlobalConcurrency     imageTaskWaitingReason = "global_concurrency"
	imageTaskWaitingReasonPaidAccountBusy       imageTaskWaitingReason = "paid_account_busy"
	imageTaskWaitingReasonCompatibleAccountBusy imageTaskWaitingReason = "compatible_account_busy"
	imageTaskWaitingReasonSourceAccountBusy     imageTaskWaitingReason = "source_account_busy"
	imageTaskWaitingReasonRetryBackoff          imageTaskWaitingReason = "retry_backoff"
)

type imageTaskSourceImagePayload struct {
	ID       string                                        `json:"id"`
	Role     string                                        `json:"role"`
	Name     string                                        `json:"name"`
	DataURL  string                                        `json:"dataUrl,omitempty"`
	URL      string                                        `json:"url,omitempty"`
	Category string                                        `json:"category,omitempty"`
	Tags     []string                                      `json:"tags,omitempty"`
	Source   *imagehistory.ImageSourceOrigin               `json:"source,omitempty"`
}

type imageTaskSourceReferencePayload struct {
	OriginalFileID  string `json:"original_file_id"`
	OriginalGenID   string `json:"original_gen_id"`
	ConversationID  string `json:"conversation_id,omitempty"`
	ParentMessageID string `json:"parent_message_id,omitempty"`
	SourceAccountID string `json:"source_account_id"`
}

type createImageTaskRequest struct {
	TaskID           string                              `json:"taskId,omitempty"`
	ConversationID   string                              `json:"conversationId"`
	TurnID           string                              `json:"turnId"`
	Source           string                              `json:"source,omitempty"`
	Mode             string                              `json:"mode"`
	Prompt           string                              `json:"prompt"`
	Model            string                              `json:"model"`
	Count            int                                 `json:"count"`
	Size             string                              `json:"size,omitempty"`
	ResolutionAccess string                             `json:"resolutionAccess,omitempty"`
	Quality          string                              `json:"quality,omitempty"`
	Background       string                              `json:"background,omitempty"`
	ResponseFormat   string                              `json:"responseFormat,omitempty"`
	RetryImageIndex  *int                                `json:"retryImageIndex,omitempty"`
	Category         string                              `json:"category,omitempty"`
	Tags             []string                            `json:"tags,omitempty"`
	SourceImages     []imageTaskSourceImagePayload       `json:"sourceImages,omitempty"`
	SourceReference  *imageTaskSourceReferencePayload    `json:"sourceReference,omitempty"`
	Policy           *accounts.ImageAccountRoutingPolicy `json:"policy,omitempty"`
}

type imageTaskBlocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type imageTaskSourceSnapshot struct {
	Workspace int `json:"workspace"`
	Compat    int `json:"compat"`
}

type imageTaskFinalStatusSnapshot struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Expired   int `json:"expired"`
}

type imageTaskView struct {
	ID              string                 `json:"id"`
	ConversationID  string                 `json:"conversationId"`
	TurnID          string                 `json:"turnId"`
	Mode            string                 `json:"mode"`
	Category        string                 `json:"category,omitempty"`
	Tags            []string               `json:"tags,omitempty"`
	SourceImages    []imageTaskSourceImagePayload `json:"sourceImages,omitempty"`
	SourceReference *imageTaskSourceReferencePayload `json:"sourceReference,omitempty"`
	Status          imageTaskStatus        `json:"status"`
	CreatedAt       string                 `json:"createdAt"`
	StartedAt       string                 `json:"startedAt,omitempty"`
	FinishedAt      string                 `json:"finishedAt,omitempty"`
	Count           int                    `json:"count"`
	RetryImageIndex *int                   `json:"retryImageIndex,omitempty"`
	QueuePosition   int                    `json:"queuePosition,omitempty"`
	WaitingReason   imageTaskWaitingReason `json:"waitingReason,omitempty"`
	Blockers        []imageTaskBlocker     `json:"blockers,omitempty"`
	Images          []imagehistory.Image   `json:"images"`
	Error           string                 `json:"error,omitempty"`
	CancelRequested bool                   `json:"cancelRequested,omitempty"`
}

type imageTaskSnapshot struct {
	Running          int                          `json:"running"`
	MaxRunning       int                          `json:"maxRunning"`
	Queued           int                          `json:"queued"`
	Total            int                          `json:"total"`
	ActiveSources    imageTaskSourceSnapshot      `json:"activeSources"`
	FinalStatuses    imageTaskFinalStatusSnapshot `json:"finalStatuses"`
	RetentionSeconds int                          `json:"retentionSeconds"`
}

type imageTaskEvent struct {
	Type     string             `json:"type"`
	TaskID   string             `json:"taskId,omitempty"`
	Task     *imageTaskView     `json:"task,omitempty"`
	Snapshot *imageTaskSnapshot `json:"snapshot,omitempty"`
}

type imageTaskRequirement struct {
	NeedPaid        bool
	SourceAccountID string
	PolicySnapshot  *accounts.ImageAccountRoutingPolicy
}

type imageTaskSourceImage struct {
	ID       string
	Role     string
	Name     string
	DataURL  string
	URL      string
	Category string
	Tags     []string
	Source   *imagehistory.ImageSourceOrigin
}

type imageTaskSourceReference struct {
	OriginalFileID  string
	OriginalGenID   string
	ConversationID  string
	ParentMessageID string
	SourceAccountID string
}

type imageTaskUnit struct {
	Index         int
	Status        imageTaskStatus
	StartedAt     time.Time
	FinishedAt    time.Time
	Error         string
	DeferredCount int
	NextAttemptAt time.Time
	Cancel        context.CancelFunc
}

type imageTask struct {
	ID              string
	ConversationID  string
	TurnID          string
	Source          string
	Mode            string
	Prompt          string
	Model           string
	Count           int
	RetryImageIndex *int
	Size            string
	Quality         string
	Background      string
	ResponseFormat  string
	Category        string
	Tags            []string
	SourceImages    []imageTaskSourceImage
	SourceReference *imageTaskSourceReference
	Requirement     imageTaskRequirement
	CreatedAt       time.Time
	StartedAt       time.Time
	FinishedAt      time.Time
	Status          imageTaskStatus
	WaitingReason   imageTaskWaitingReason
	Blockers        []imageTaskBlocker
	Images          []imagehistory.Image
	Error           string
	Units           []imageTaskUnit
	ActiveUnits     int
	CancelRequested bool
}

func normalizeImageTaskTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func normalizeImageTaskSourceOrigin(origin *imagehistory.ImageSourceOrigin, fallbackURL string) *imagehistory.ImageSourceOrigin {
	if origin == nil {
		if trimmed := strings.TrimSpace(fallbackURL); trimmed != "" {
			return &imagehistory.ImageSourceOrigin{
				Type:      "url",
				Confirmed: true,
				URL:       trimmed,
			}
		}
		return nil
	}
	copy := *origin
	copy.Type = strings.TrimSpace(copy.Type)
	copy.URL = strings.TrimSpace(copy.URL)
	copy.FilePath = strings.TrimSpace(copy.FilePath)
	if copy.Gallery != nil {
		gallery := *copy.Gallery
		gallery.AssetID = strings.TrimSpace(gallery.AssetID)
		gallery.ConversationID = strings.TrimSpace(gallery.ConversationID)
		gallery.TurnID = strings.TrimSpace(gallery.TurnID)
		gallery.ImageID = strings.TrimSpace(gallery.ImageID)
		if gallery.AssetID == "" && gallery.ConversationID == "" && gallery.TurnID == "" && gallery.ImageID == "" && gallery.Index == nil {
			copy.Gallery = nil
		} else {
			copy.Gallery = &gallery
		}
	}
	if copy.Type == "" {
		switch {
		case copy.Gallery != nil:
			copy.Type = "gallery"
		case copy.FilePath != "":
			copy.Type = "file"
		case copy.URL != "":
			copy.Type = "url"
		case strings.TrimSpace(fallbackURL) != "":
			copy.Type = "url"
			copy.URL = strings.TrimSpace(fallbackURL)
			copy.Confirmed = true
		}
	}
	switch copy.Type {
	case "gallery":
		if copy.Gallery == nil {
			return nil
		}
	case "file":
		if copy.FilePath == "" {
			return nil
		}
	case "url":
		if copy.URL == "" {
			if trimmed := strings.TrimSpace(fallbackURL); trimmed != "" {
				copy.URL = trimmed
				copy.Confirmed = true
			} else {
				return nil
			}
		}
	default:
		if copy.Gallery == nil && copy.FilePath == "" && copy.URL == "" {
			return nil
		}
	}
	return &copy
}
