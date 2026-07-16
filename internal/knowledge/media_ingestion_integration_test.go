package knowledge

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type noopJobPublisher struct{}

func (noopJobPublisher) Publish(context.Context, uint64) error { return nil }

type fixedTranscriber struct{}

func (fixedTranscriber) Transcribe(context.Context, string, string) ([]TranscriptSegment, error) {
	return []TranscriptSegment{{StartMS: 0, EndMS: 3000, Text: "FlowPilot media ingestion"}}, nil
}

type fixedOCR struct{}

func (fixedOCR) Extract(context.Context, string) (OCRResult, error) {
	return OCRResult{Text: "Quarterly Report"}, nil
}

type captureMediaIndex struct {
	mu     sync.Mutex
	chunks []rag.Chunk
}

func (i *captureMediaIndex) IndexDocument(_ context.Context, _ string, _ uint64, _ string, chunks []rag.Chunk) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.chunks = append([]rag.Chunk(nil), chunks...)
	return nil
}

func (*captureMediaIndex) DeleteDocument(context.Context, string) error        { return nil }
func (*captureMediaIndex) DeleteVersion(context.Context, string, uint64) error { return nil }

type blockingMediaProcessor struct{}

func (blockingMediaProcessor) Probe(ctx context.Context, _, _ string) (MediaInfo, error) {
	<-ctx.Done()
	return MediaInfo{}, ctx.Err()
}

func (blockingMediaProcessor) ExtractAudio(context.Context, string, string) error { return nil }
func (blockingMediaProcessor) ExtractKeyframes(context.Context, string, string, int64) ([]Keyframe, error) {
	return nil, nil
}

func TestMediaWorkerArtifactsTimelineAndCleanupIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	sample := os.Getenv("FLOWPILOT_MEDIA_SAMPLE")
	if sample == "" {
		t.Skip("set FLOWPILOT_MEDIA_SAMPLE to run media ingestion integration")
	}
	cfg := config.Load()
	db, err := database.OpenMySQL(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(db)
	storage, err := NewLocalObjectStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(sample)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	service := NewService(repository, storage, noopJobPublisher{}, nil, cfg.Knowledge, true)
	result, err := service.Upload(context.Background(), UploadInput{Filename: filepath.Base(sample), DeclaredType: "video/mp4", Source: io.Reader(file)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupKnowledgeRows(db, result.DocumentID) })
	index := &captureMediaIndex{}
	var transcriber Transcriber = fixedTranscriber{}
	var ocr OCRExtractor = fixedOCR{}
	if os.Getenv("FLOWPILOT_REAL_MEDIA") == "1" {
		transcriber = NewWhisperCPPTranscriber(cfg.Knowledge, nil)
		ocr = NewTesseractOCR(cfg.Knowledge, nil)
	}
	pipeline := NewMediaPipeline(storage, repository, NewFFmpegProcessor(cfg.Knowledge, nil), transcriber, ocr, cfg.Knowledge)
	worker := NewWorker(repository, storage, nil, index, cfg.Knowledge, pipeline)
	if err := worker.Process(context.Background(), result.JobID); err != nil {
		t.Fatal(err)
	}
	job, err := repository.GetJob(context.Background(), result.JobID)
	if err != nil || job.Status != domain.IngestionJobSuccess || job.Progress != 100 {
		t.Fatalf("job=%#v error=%v", job, err)
	}
	index.mu.Lock()
	chunks := append([]rag.Chunk(nil), index.chunks...)
	index.mu.Unlock()
	if len(chunks) == 0 || chunks[0].StartMS != 0 || chunks[0].EndMS <= chunks[0].StartMS {
		t.Fatalf("chunks=%#v", chunks)
	}
	if !strings.Contains(chunks[0].Text, "Quarterly") {
		t.Fatalf("OCR text was not merged into timeline chunk: %#v", chunks)
	}
	artifacts, err := repository.ListArtifactsForDocument(context.Background(), result.DocumentID)
	if err != nil || len(artifacts) < 3 {
		t.Fatalf("artifacts=%#v error=%v", artifacts, err)
	}
	if err := service.Delete(context.Background(), result.DocumentID); err != nil {
		t.Fatal(err)
	}
	if err := worker.CleanupDeleting(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	remaining, err := repository.ListArtifactsForDocument(context.Background(), result.DocumentID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining artifacts=%#v error=%v", remaining, err)
	}
}

func TestRunningMediaJobCancellationStopsWorkerIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	cfg := config.Load()
	db, err := database.OpenMySQL(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(db)
	storage, err := NewLocalObjectStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := append([]byte{0, 0, 0, 24, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, []byte(time.Now().String())...)
	service := NewService(repository, storage, noopJobPublisher{}, nil, cfg.Knowledge, true)
	result, err := service.Upload(context.Background(), UploadInput{Filename: "cancel.mp4", DeclaredType: "video/mp4", Source: bytes.NewReader(content)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupKnowledgeRows(db, result.DocumentID) })
	index := &captureMediaIndex{}
	pipeline := NewMediaPipeline(storage, repository, blockingMediaProcessor{}, fixedTranscriber{}, fixedOCR{}, cfg.Knowledge)
	worker := NewWorker(repository, storage, nil, index, cfg.Knowledge, pipeline)
	done := make(chan error, 1)
	go func() { done <- worker.Process(context.Background(), result.JobID) }()
	waitForKnowledgeStage(t, repository, result.JobID, domain.IngestionStageProbe)
	requested, err := service.Cancel(context.Background(), result.JobID)
	if err != nil || requested.CancelRequestedAt == nil {
		t.Fatalf("cancel request=%#v error=%v", requested, err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("worker after cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not stop after cancellation")
	}
	job, err := repository.GetJob(context.Background(), result.JobID)
	if err != nil || job.Status != domain.IngestionJobCanceled || job.SafeErrorCode != "canceled" {
		t.Fatalf("canceled job=%#v error=%v", job, err)
	}
}

func waitForKnowledgeStage(t *testing.T, repository *GormRepository, id uint64, stage domain.IngestionStage) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repository.GetJob(context.Background(), id)
		if err == nil && job.Status == domain.IngestionJobRunning && job.Stage == stage {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %d did not reach stage %s", id, stage)
}
