package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

const DefaultPageSize = 20
const MaxPageSize = 100

type JobPublisher interface {
	Publish(ctx context.Context, jobID uint64) error
}

type SearchEngine interface {
	SearchAdvanced(ctx context.Context, query string, topK int, minScore float64) ([]rag.SearchResult, error)
}

type UploadInput struct {
	Filename     string
	DeclaredType string
	Source       io.Reader
}

type UploadResult struct {
	DocumentID   uint64                    `json:"document_id"`
	VersionID    uint64                    `json:"version_id"`
	JobID        uint64                    `json:"job_id"`
	Status       domain.IngestionJobStatus `json:"status"`
	Deduplicated bool                      `json:"deduplicated"`
}

type SearchRequest struct {
	Query    string  `json:"query"`
	TopK     int     `json:"top_k"`
	MinScore float64 `json:"min_score"`
}

type Service struct {
	repository   *GormRepository
	storage      ObjectStorage
	publisher    JobPublisher
	search       SearchEngine
	config       config.KnowledgeConfig
	mediaEnabled bool
}

type AgentSearcher struct {
	service *Service
}

func NewAgentSearcher(service *Service) *AgentSearcher {
	return &AgentSearcher{service: service}
}

func (s *AgentSearcher) Search(ctx context.Context, query string, topK int) ([]rag.SearchResult, error) {
	return s.service.Search(ctx, SearchRequest{Query: query, TopK: topK})
}

func NewService(repository *GormRepository, storage ObjectStorage, publisher JobPublisher, search SearchEngine, cfg config.KnowledgeConfig, mediaEnabled ...bool) *Service {
	service := &Service{repository: repository, storage: storage, publisher: publisher, search: search, config: cfg}
	if len(mediaEnabled) > 0 {
		service.mediaEnabled = mediaEnabled[0]
	}
	return service
}

func (s *Service) Upload(ctx context.Context, input UploadInput) (UploadResult, error) {
	return s.upload(ctx, 0, input)
}

func (s *Service) UploadVersion(ctx context.Context, documentID uint64, input UploadInput) (UploadResult, error) {
	if documentID == 0 {
		return UploadResult{}, fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	return s.upload(ctx, documentID, input)
}

func (s *Service) upload(ctx context.Context, documentID uint64, input UploadInput) (UploadResult, error) {
	filename, extension, err := ValidateUploadName(input.Filename)
	if err != nil {
		return UploadResult{}, err
	}
	if IsMediaFormat(extension) && !s.mediaEnabled {
		return UploadResult{}, fmt.Errorf("%w: local media runtimes are not configured", ErrUnavailable)
	}
	if input.Source == nil {
		return UploadResult{}, fmt.Errorf("%w: file is required", ErrInvalidInput)
	}
	maxBytes := s.config.MaxBytesByFormat[extension]
	if maxBytes <= 0 {
		return UploadResult{}, fmt.Errorf("%w: file format is disabled", ErrInvalidInput)
	}
	object, err := s.storage.Put(ctx, extension, input.Source, maxBytes)
	if err != nil {
		return UploadResult{}, err
	}
	keepObject := false
	defer func() {
		if !keepObject {
			_ = s.storage.Delete(context.Background(), object.Key)
		}
	}()
	stored, size, err := s.storage.Open(ctx, object.Key)
	if err != nil {
		return UploadResult{}, fmt.Errorf("validate stored object: %w", err)
	}
	mediaType, validateErr := ValidateFile(stored, size, extension, input.DeclaredType, ArchiveLimits{
		MaxFiles: s.config.MaxArchiveFiles, MaxBytes: s.config.MaxArchiveBytes,
		MaxRatio: s.config.MaxArchiveRatio, MaxPathDepth: s.config.MaxArchiveDepth,
		MaxPPTSlides: s.config.MaxPPTSlides,
	})
	closeErr := stored.Close()
	if validateErr != nil {
		return UploadResult{}, validateErr
	}
	if closeErr != nil {
		return UploadResult{}, fmt.Errorf("close stored object: %w", closeErr)
	}
	parserVersion := ParserVersion
	if IsMediaFormat(extension) {
		parserVersion = MediaPipelineVersion
	}
	duplicate, duplicateErr := s.repository.FindDuplicate(ctx, object.Checksum, parserVersion)
	if duplicateErr == nil {
		return UploadResult{
			DocumentID: duplicate.Document.ID, VersionID: duplicate.Version.ID,
			JobID: duplicate.Job.ID, Status: duplicate.Job.Status, Deduplicated: true,
		}, nil
	}
	if !errors.Is(duplicateErr, ErrNotFound) {
		return UploadResult{}, duplicateErr
	}
	document := domain.Document{
		Filename: filename, MediaType: mediaType, SizeBytes: object.Size,
		Checksum: object.Checksum, Status: domain.DocumentStatusQueued,
	}
	version := domain.DocumentVersion{
		StorageKey: object.Key, Filename: filename, MediaType: mediaType, SizeBytes: object.Size,
		Checksum: object.Checksum, ParserVersion: parserVersion,
	}
	job := domain.IngestionJob{
		Status: domain.IngestionJobQueued, Stage: domain.IngestionStageUpload, Progress: 0,
	}
	if documentID == 0 {
		if err := s.repository.CreateDocument(ctx, &document, &version, &job); err != nil {
			return UploadResult{}, err
		}
	} else {
		if err := s.repository.CreateVersion(ctx, documentID, &version, &job); err != nil {
			return UploadResult{}, err
		}
		document.ID = documentID
	}
	keepObject = true
	// Publishing is intentionally best effort. The durable MySQL job is the
	// source of truth and the dispatcher republishes Queued jobs after a crash or
	// an uncertain RabbitMQ confirm.
	s.publishJob(ctx, job.ID)
	return UploadResult{DocumentID: document.ID, VersionID: version.ID, JobID: job.ID, Status: job.Status}, nil
}

func (s *Service) List(ctx context.Context, filter DocumentFilter) (DocumentList, error) {
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = DefaultPageSize
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > MaxPageSize {
		return DocumentList{}, fmt.Errorf("%w: invalid pagination", ErrInvalidInput)
	}
	if filter.Status != "" && !validDocumentStatus(filter.Status) {
		return DocumentList{}, fmt.Errorf("%w: invalid document status", ErrInvalidInput)
	}
	if filter.Extension != "" {
		filter.Extension = strings.ToLower(strings.TrimSpace(filter.Extension))
		if !strings.HasPrefix(filter.Extension, ".") {
			filter.Extension = "." + filter.Extension
		}
		if _, ok := SupportedFormats[filter.Extension]; !ok {
			return DocumentList{}, fmt.Errorf("%w: invalid document format", ErrInvalidInput)
		}
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if len([]rune(filter.Query)) > 100 {
		return DocumentList{}, fmt.Errorf("%w: query is too long", ErrInvalidInput)
	}
	return s.repository.ListDocuments(ctx, filter)
}

func (s *Service) Get(ctx context.Context, id uint64) (DocumentDetail, error) {
	if id == 0 {
		return DocumentDetail{}, fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	return s.repository.GetDocument(ctx, id)
}

func (s *Service) GetJob(ctx context.Context, id uint64) (domain.IngestionJob, error) {
	if id == 0 {
		return domain.IngestionJob{}, fmt.Errorf("%w: job id is required", ErrInvalidInput)
	}
	return s.repository.GetJob(ctx, id)
}

func (s *Service) Retry(ctx context.Context, id uint64) (domain.IngestionJob, error) {
	if id == 0 {
		return domain.IngestionJob{}, fmt.Errorf("%w: job id is required", ErrInvalidInput)
	}
	job, err := s.repository.RetryJob(ctx, id, s.config.MaxRetries)
	if err != nil {
		return domain.IngestionJob{}, err
	}
	s.publishJob(ctx, job.ID)
	return job, nil
}

func (s *Service) Cancel(ctx context.Context, id uint64) (domain.IngestionJob, error) {
	if id == 0 {
		return domain.IngestionJob{}, fmt.Errorf("%w: job id is required", ErrInvalidInput)
	}
	return s.repository.RequestCancel(ctx, id)
}

func (s *Service) publishJob(ctx context.Context, jobID uint64) {
	if err := s.publisher.Publish(ctx, jobID); err != nil {
		return
	}
	_ = s.repository.MarkJobDispatched(ctx, jobID)
}

func (s *Service) Delete(ctx context.Context, id uint64) error {
	if id == 0 {
		return fmt.Errorf("%w: document id is required", ErrInvalidInput)
	}
	return s.repository.MarkDeleting(ctx, id)
}

func (s *Service) Search(ctx context.Context, request SearchRequest) ([]rag.SearchResult, error) {
	if s.search == nil {
		return nil, ErrUnavailable
	}
	if request.MinScore == 0 {
		request.MinScore = s.config.SearchMinScore
	}
	results, err := s.search.SearchAdvanced(ctx, request.Query, request.TopK, request.MinScore)
	if err != nil {
		return nil, err
	}
	documentIDs := make([]uint64, 0, len(results))
	parsedIDs := make([]uint64, len(results))
	for index, result := range results {
		id, parseErr := strconv.ParseUint(result.DocumentID, 10, 64)
		if parseErr == nil && id > 0 {
			parsedIDs[index] = id
			documentIDs = append(documentIDs, id)
		}
	}
	active, err := s.repository.ActiveVersions(ctx, documentIDs)
	if err != nil {
		return nil, err
	}
	filtered := make([]rag.SearchResult, 0, len(results))
	for index, result := range results {
		if current, ok := active[parsedIDs[index]]; ok && current == result.VersionID {
			filtered = append(filtered, result)
		}
	}
	return filtered, nil
}

func validDocumentStatus(status domain.DocumentStatus) bool {
	switch status {
	case domain.DocumentStatusQueued, domain.DocumentStatusProcessing, domain.DocumentStatusReady, domain.DocumentStatusFailed, domain.DocumentStatusCanceled:
		return true
	default:
		return false
	}
}

func Extension(filename string) string {
	return strings.ToLower(filepath.Ext(filename))
}
