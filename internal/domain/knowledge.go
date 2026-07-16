package domain

import "time"

type DocumentStatus string
type IngestionJobStatus string
type IngestionStage string

const (
	DocumentStatusQueued     DocumentStatus = "Queued"
	DocumentStatusProcessing DocumentStatus = "Processing"
	DocumentStatusReady      DocumentStatus = "Ready"
	DocumentStatusFailed     DocumentStatus = "Failed"
	DocumentStatusCanceled   DocumentStatus = "Canceled"
	DocumentStatusDeleting   DocumentStatus = "Deleting"
)

const (
	IngestionJobQueued   IngestionJobStatus = "Queued"
	IngestionJobRunning  IngestionJobStatus = "Running"
	IngestionJobSuccess  IngestionJobStatus = "Success"
	IngestionJobFailed   IngestionJobStatus = "Failed"
	IngestionJobCanceled IngestionJobStatus = "Canceled"
)

const (
	IngestionStageUpload       IngestionStage = "upload"
	IngestionStageProbe        IngestionStage = "probe"
	IngestionStageExtractAudio IngestionStage = "extract_audio"
	IngestionStageTranscribe   IngestionStage = "transcribe"
	IngestionStageKeyframes    IngestionStage = "keyframes"
	IngestionStageOCR          IngestionStage = "ocr"
	IngestionStageMerge        IngestionStage = "merge"
	IngestionStageParse        IngestionStage = "parse"
	IngestionStageChunk        IngestionStage = "chunk"
	IngestionStageEmbedding    IngestionStage = "embedding"
	IngestionStageIndexing     IngestionStage = "indexing"
)

// Document is the user-visible knowledge resource. Its active version changes
// only after a new version has been indexed successfully.
type Document struct {
	ID             uint64         `gorm:"primaryKey" json:"id"`
	Filename       string         `gorm:"size:255;not null;index" json:"filename"`
	MediaType      string         `gorm:"size:100;not null;index" json:"media_type"`
	SizeBytes      int64          `gorm:"not null" json:"size_bytes"`
	Checksum       string         `gorm:"size:64;not null;index" json:"checksum"`
	Status         DocumentStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	CurrentVersion uint64         `gorm:"not null;default:0" json:"current_version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// DocumentVersion is an immutable reference to one stored source file.
type DocumentVersion struct {
	ID            uint64    `gorm:"primaryKey" json:"id"`
	DocumentID    uint64    `gorm:"not null;uniqueIndex:idx_document_version" json:"document_id"`
	Version       uint64    `gorm:"not null;uniqueIndex:idx_document_version" json:"version"`
	StorageKey    string    `gorm:"size:255;not null;uniqueIndex" json:"-"`
	Filename      string    `gorm:"size:255;not null" json:"filename"`
	MediaType     string    `gorm:"size:100;not null" json:"media_type"`
	SizeBytes     int64     `gorm:"not null" json:"size_bytes"`
	Checksum      string    `gorm:"size:64;not null;index" json:"checksum"`
	ParserVersion string    `gorm:"size:50;not null" json:"parser_version"`
	ChunkCount    int       `gorm:"not null;default:0" json:"chunk_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// IngestionJob records a recoverable asynchronous ingestion attempt. Safe
// error fields are intentionally separate from server logs so API responses do
// not expose storage paths, provider payloads, or credentials.
type IngestionJob struct {
	ID                 uint64             `gorm:"primaryKey" json:"id"`
	DocumentID         uint64             `gorm:"not null;index" json:"document_id"`
	VersionID          uint64             `gorm:"not null;index" json:"version_id"`
	Status             IngestionJobStatus `gorm:"type:varchar(20);not null;index" json:"status"`
	Stage              IngestionStage     `gorm:"type:varchar(20);not null" json:"stage"`
	Progress           int                `gorm:"not null;default:0" json:"progress"`
	SafeErrorCode      string             `gorm:"size:50;not null;default:''" json:"safe_error_code,omitempty"`
	SafeErrorMessage   string             `gorm:"size:255;not null;default:''" json:"safe_error_message,omitempty"`
	RetryCount         int                `gorm:"not null;default:0" json:"retry_count"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
	StartedAt          *time.Time         `json:"started_at,omitempty"`
	FinishedAt         *time.Time         `json:"finished_at,omitempty"`
	DispatchedAt       *time.Time         `gorm:"index" json:"-"`
	CancelRequestedAt  *time.Time         `gorm:"index" json:"cancel_requested_at,omitempty"`
	CleanupCompletedAt *time.Time         `gorm:"index" json:"-"`
}

type ArtifactKind string

const (
	ArtifactAudio      ArtifactKind = "audio"
	ArtifactKeyframe   ArtifactKind = "keyframe"
	ArtifactTranscript ArtifactKind = "transcript"
)

// DocumentArtifact tracks derived files so retries, cancellation, version
// replacement, and document deletion can clean them deterministically.
type DocumentArtifact struct {
	ID         uint64       `gorm:"primaryKey" json:"id"`
	DocumentID uint64       `gorm:"not null;index" json:"document_id"`
	VersionID  uint64       `gorm:"not null;index" json:"version_id"`
	JobID      uint64       `gorm:"not null;index" json:"job_id"`
	Kind       ArtifactKind `gorm:"type:varchar(20);not null;index" json:"kind"`
	StorageKey string       `gorm:"size:255;not null;uniqueIndex" json:"-"`
	MediaType  string       `gorm:"size:100;not null" json:"media_type"`
	SizeBytes  int64        `gorm:"not null" json:"size_bytes"`
	Checksum   string       `gorm:"size:64;not null" json:"checksum"`
	StartMS    int64        `gorm:"not null;default:0" json:"start_ms,omitempty"`
	EndMS      int64        `gorm:"not null;default:0" json:"end_ms,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}
