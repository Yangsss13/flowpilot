package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

var ErrRetryJob = errors.New("retry ingestion job")

type IndexEngine interface {
	IndexDocument(ctx context.Context, documentID string, versionID uint64, source string, chunks []rag.Chunk) error
	DeleteDocument(ctx context.Context, documentID string) error
	DeleteVersion(ctx context.Context, documentID string, versionID uint64) error
}

type Worker struct {
	repository *GormRepository
	storage    ObjectStorage
	parser     Parser
	index      IndexEngine
	config     config.KnowledgeConfig
	media      *MediaPipeline
}

func NewWorker(repository *GormRepository, storage ObjectStorage, parser Parser, index IndexEngine, cfg config.KnowledgeConfig, media ...*MediaPipeline) *Worker {
	worker := &Worker{repository: repository, storage: storage, parser: parser, index: index, config: cfg}
	if len(media) > 0 {
		worker.media = media[0]
	}
	return worker
}

func (w *Worker) Process(ctx context.Context, jobID uint64) (resultErr error) {
	job, document, version, err := w.repository.ClaimJob(ctx, jobID)
	if errors.Is(err, ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	processingCtx, cancelProcessing := context.WithCancel(ctx)
	if IsMediaFormat(Extension(version.Filename)) {
		var cancelTimeout context.CancelFunc
		processingCtx, cancelTimeout = context.WithTimeout(processingCtx, w.config.MediaJobTimeout)
		defer cancelTimeout()
	}
	defer cancelProcessing()
	var cancellationObserved atomic.Bool
	watchDone := make(chan struct{})
	go w.watchCancellation(processingCtx, job.ID, &cancellationObserved, cancelProcessing, watchDone)
	defer close(watchDone)
	defer func() {
		if resultErr == nil || errors.Is(resultErr, ErrRetryJob) {
			return
		}
		requested, _ := w.repository.IsCancelRequested(context.Background(), job.ID)
		if cancellationObserved.Load() || requested {
			w.cleanupCanceledJob(job, document, version)
			resultErr = nil
			return
		}
		retry, retryErr := w.repository.RequeueJob(context.Background(), job.ID, w.config.MaxRetries, "worker_interrupted", "knowledge worker was interrupted")
		if retryErr == nil && retry {
			resultErr = ErrRetryJob
		}
	}()
	extension := Extension(version.Filename)
	temporaryPath, err := w.materialize(processingCtx, version.StorageKey, extension)
	if err != nil {
		return w.handleJobFailure(job.ID, "storage_unavailable", "stored document is temporarily unavailable", true, err)
	}
	defer os.Remove(temporaryPath)
	var chunks []rag.Chunk
	if IsMediaFormat(extension) {
		if w.media == nil {
			return w.handleJobFailure(job.ID, "media_runtime_unavailable", "media processing is not configured", false, ErrMediaRuntime)
		}
		chunks, err = w.media.Process(processingCtx, MediaInput{Job: job, Document: document, Version: version, Path: temporaryPath, Extension: extension}, func(stage domain.IngestionStage, progress int) error {
			return w.repository.UpdateJobStage(processingCtx, job.ID, stage, progress)
		})
		if err != nil {
			code, message, retryable := classifyMediaError(err)
			return w.handleJobFailure(job.ID, code, message, retryable, err)
		}
	} else {
		if err := w.repository.UpdateJobStage(processingCtx, job.ID, domain.IngestionStageParse, 15); err != nil {
			return err
		}
		blocks, parseErr := w.parser.Parse(processingCtx, temporaryPath, extension, ParserLimits{
			MaxArchiveFiles: w.config.MaxArchiveFiles, MaxArchiveBytes: w.config.MaxArchiveBytes,
			MaxArchiveRatio: w.config.MaxArchiveRatio, MaxArchiveDepth: w.config.MaxArchiveDepth,
			MaxPDFPages: w.config.MaxPDFPages, MaxPPTSlides: w.config.MaxPPTSlides,
			Timeout: w.config.ParseTimeout,
		})
		if parseErr != nil {
			return w.handleJobFailure(job.ID, "parse_failed", "document could not be parsed safely", false, parseErr)
		}
		if err := w.repository.UpdateJobStage(processingCtx, job.ID, domain.IngestionStageChunk, 35); err != nil {
			return err
		}
		chunks, err = BuildChunks(blocks, w.config.ChunkMaxRunes)
		if err != nil {
			return w.handleJobFailure(job.ID, "chunk_failed", "document contains no usable text", false, err)
		}
	}
	if err := w.repository.UpdateJobStage(processingCtx, job.ID, domain.IngestionStageEmbedding, 82); err != nil {
		return err
	}
	documentKey := strconv.FormatUint(document.ID, 10)
	if err := w.index.IndexDocument(processingCtx, documentKey, version.Version, version.Filename, chunks); err != nil {
		retryable := errors.Is(err, rag.ErrEmbedding) || errors.Is(err, rag.ErrVectorStore) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		return w.handleJobFailure(job.ID, "indexing_unavailable", "embedding or vector indexing is temporarily unavailable", retryable, err)
	}
	if err := w.repository.UpdateJobStage(processingCtx, job.ID, domain.IngestionStageIndexing, 92); err != nil {
		return err
	}
	previous, err := w.repository.CompleteJob(processingCtx, job.ID, len(chunks))
	if err != nil {
		return err
	}
	if previous != nil {
		if err := w.index.DeleteVersion(context.Background(), documentKey, previous.Version); err != nil {
			log.Printf("knowledge cleanup old version document=%d version=%d: %v", document.ID, previous.Version, err)
		}
		if w.media != nil {
			if err := w.media.CleanupVersionArtifacts(context.Background(), previous.ID); err != nil {
				log.Printf("knowledge cleanup old media artifacts document=%d version=%d: %v", document.ID, previous.Version, err)
			}
		}
	}
	return nil
}

func (w *Worker) CleanupDeleting(ctx context.Context, limit int) error {
	documents, err := w.repository.ListDeleting(ctx, limit)
	if err != nil {
		return err
	}
	for _, document := range documents {
		versions, err := w.repository.ListVersions(ctx, document.ID)
		if err != nil {
			return err
		}
		if err := w.index.DeleteDocument(ctx, strconv.FormatUint(document.ID, 10)); err != nil {
			return err
		}
		artifacts, err := w.repository.ListArtifactsForDocument(ctx, document.ID)
		if err != nil {
			return err
		}
		for _, artifact := range artifacts {
			if err := w.storage.Delete(ctx, artifact.StorageKey); err != nil {
				return err
			}
		}
		for _, version := range versions {
			if err := w.storage.Delete(ctx, version.StorageKey); err != nil {
				return err
			}
		}
		if err := w.repository.DeleteMetadata(ctx, document.ID); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) CleanupSuperseded(ctx context.Context, limit int) error {
	documents, err := w.repository.ListReadyDocuments(ctx, limit)
	if err != nil {
		return err
	}
	for _, document := range documents {
		versions, err := w.repository.ListVersions(ctx, document.ID)
		if err != nil {
			return err
		}
		for _, version := range versions {
			if version.Version >= document.CurrentVersion {
				continue
			}
			if err := w.index.DeleteVersion(ctx, strconv.FormatUint(document.ID, 10), version.Version); err != nil {
				return err
			}
			if w.media != nil {
				if err := w.media.CleanupVersionArtifacts(ctx, version.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (w *Worker) CleanupCanceled(ctx context.Context, limit int) error {
	jobs, err := w.repository.ListCanceledJobsForCleanup(ctx, limit)
	if err != nil {
		return err
	}
	for _, job := range jobs {
		version, err := w.repository.GetVersion(ctx, job.VersionID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		if err := w.index.DeleteVersion(ctx, strconv.FormatUint(job.DocumentID, 10), version.Version); err != nil {
			return err
		}
		if w.media != nil {
			if err := w.media.CleanupVersionArtifacts(ctx, version.ID); err != nil {
				return err
			}
		}
		if err := w.repository.MarkJobCleanupComplete(ctx, job.ID); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) materialize(ctx context.Context, key, extension string) (string, error) {
	source, _, err := w.storage.Open(ctx, key)
	if err != nil {
		return "", err
	}
	defer source.Close()
	temporary, err := os.CreateTemp("", "flowpilot-knowledge-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create parser input: %w", err)
	}
	name := temporary.Name()
	ok := false
	defer func() {
		_ = temporary.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return "", fmt.Errorf("protect parser input: %w", err)
	}
	if _, err := copyWithContext(ctx, temporary, source); err != nil {
		return "", fmt.Errorf("materialize parser input: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close parser input: %w", err)
	}
	ok = true
	return name, nil
}

func (w *Worker) handleFailure(jobID uint64, code, safeMessage string, retryable bool, cause error) error {
	log.Printf("knowledge ingestion job %d failed (%s): %v", jobID, code, cause)
	ctx := context.Background()
	if retryable {
		retry, err := w.repository.RequeueJob(ctx, jobID, w.config.MaxRetries, code, safeMessage)
		if err != nil {
			return err
		}
		if retry {
			return ErrRetryJob
		}
		return nil
	}
	return w.repository.FailJob(ctx, jobID, code, safeMessage)
}

func (w *Worker) handleJobFailure(jobID uint64, code, safeMessage string, retryable bool, cause error) error {
	requested, _ := w.repository.IsCancelRequested(context.Background(), jobID)
	if requested {
		return cause
	}
	return w.handleFailure(jobID, code, safeMessage, retryable, cause)
}

func (w *Worker) watchCancellation(ctx context.Context, jobID uint64, observed *atomic.Bool, cancel context.CancelFunc, done <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			requested, err := w.repository.IsCancelRequested(context.Background(), jobID)
			if err != nil {
				continue
			}
			if requested {
				observed.Store(true)
				cancel()
				return
			}
		}
	}
}

func (w *Worker) cleanupCanceledJob(job domain.IngestionJob, document domain.Document, version domain.DocumentVersion) {
	documentKey := strconv.FormatUint(document.ID, 10)
	cleanupComplete := true
	if err := w.index.DeleteVersion(context.Background(), documentKey, version.Version); err != nil {
		cleanupComplete = false
		log.Printf("cleanup canceled knowledge vectors job=%d: %v", job.ID, err)
	}
	if w.media != nil {
		if err := w.media.CleanupVersionArtifacts(context.Background(), version.ID); err != nil {
			cleanupComplete = false
			log.Printf("cleanup canceled media artifacts job=%d: %v", job.ID, err)
		}
	}
	if err := w.repository.CancelJob(context.Background(), job.ID); err != nil && !errors.Is(err, ErrConflict) {
		log.Printf("finish knowledge cancellation job=%d: %v", job.ID, err)
		return
	}
	if cleanupComplete {
		_ = w.repository.MarkJobCleanupComplete(context.Background(), job.ID)
	}
}

func classifyMediaError(err error) (string, string, bool) {
	switch {
	case errors.Is(err, ErrMediaInvalid):
		return "media_invalid", "media could not be decoded into searchable content", false
	case errors.Is(err, ErrMediaRuntime):
		return "media_runtime_unavailable", "a required local media runtime is unavailable", false
	case errors.Is(err, ErrMediaTimeout), errors.Is(err, context.DeadlineExceeded):
		return "media_timeout", "media processing exceeded its time limit", true
	default:
		return "media_processing_failed", "media processing failed temporarily", true
	}
}
