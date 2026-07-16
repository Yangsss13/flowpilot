package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
)

func TestIngestionJobConcurrentClaimAndRestartRecoveryIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatalf("open MySQL: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate MySQL: %v", err)
	}
	repository := NewGormRepository(db)
	document := domain.Document{Filename: "claim.txt", MediaType: "text/plain", SizeBytes: 4, Checksum: fmt.Sprintf("%064d", time.Now().UnixNano()), Status: domain.DocumentStatusQueued}
	version := domain.DocumentVersion{StorageKey: fmt.Sprintf("test/%d.txt", time.Now().UnixNano()), Filename: document.Filename, MediaType: document.MediaType, SizeBytes: document.SizeBytes, Checksum: document.Checksum, ParserVersion: ParserVersion}
	job := domain.IngestionJob{Status: domain.IngestionJobQueued, Stage: domain.IngestionStageUpload}
	if err := repository.CreateDocument(context.Background(), &document, &version, &job); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupKnowledgeRows(db, document.ID) })
	duplicate, err := repository.FindDuplicate(context.Background(), version.Checksum, ParserVersion)
	if err != nil || duplicate.Version.ID != version.ID {
		t.Fatalf("same parser duplicate=%#v error=%v", duplicate, err)
	}
	if _, err := repository.FindDuplicate(context.Background(), version.Checksum, "future-parser-version"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("different parser version duplicate error=%v, want ErrNotFound", err)
	}

	var winners atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _, _, claimErr := repository.ClaimJob(context.Background(), job.ID)
			if claimErr == nil {
				winners.Add(1)
			} else if !errors.Is(claimErr, ErrConflict) {
				t.Errorf("ClaimJob() error = %v", claimErr)
			}
		}()
	}
	wait.Wait()
	if winners.Load() != 1 {
		t.Fatalf("successful claims = %d, want 1", winners.Load())
	}
	if err := repository.RecoverInterrupted(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := repository.GetJob(context.Background(), job.ID)
	if err != nil || recovered.Status != domain.IngestionJobQueued || recovered.StartedAt != nil {
		t.Fatalf("recovered job = %#v, error = %v", recovered, err)
	}
}

func TestCompleteVersionReturnsOldVersionAndRetryIsAtomicIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(db)
	document := domain.Document{Filename: "versions.txt", MediaType: "text/plain", SizeBytes: 3, Checksum: fmt.Sprintf("%064d", time.Now().UnixNano()), Status: domain.DocumentStatusReady, CurrentVersion: 1}
	versionOne := domain.DocumentVersion{StorageKey: fmt.Sprintf("test/%d-v1.txt", time.Now().UnixNano()), Filename: document.Filename, MediaType: document.MediaType, SizeBytes: document.SizeBytes, Checksum: document.Checksum, ParserVersion: ParserVersion}
	initial := domain.IngestionJob{Status: domain.IngestionJobSuccess, Stage: domain.IngestionStageIndexing, Progress: 100}
	if err := repository.CreateDocument(context.Background(), &document, &versionOne, &initial); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanupKnowledgeRows(db, document.ID) })
	if err := db.Model(&domain.Document{}).Where("id = ?", document.ID).Update("current_version", 1).Error; err != nil {
		t.Fatal(err)
	}
	versionTwo := domain.DocumentVersion{StorageKey: fmt.Sprintf("test/%d-v2.txt", time.Now().UnixNano()), Filename: document.Filename, MediaType: document.MediaType, SizeBytes: 4, Checksum: fmt.Sprintf("%064d", time.Now().UnixNano()+1), ParserVersion: ParserVersion}
	job := domain.IngestionJob{Status: domain.IngestionJobRunning, Stage: domain.IngestionStageEmbedding}
	if err := repository.CreateVersion(context.Background(), document.ID, &versionTwo, &job); err != nil {
		t.Fatal(err)
	}
	beforeActivation, err := repository.GetDocument(context.Background(), document.ID)
	if err != nil || beforeActivation.Document.Checksum != versionOne.Checksum || beforeActivation.Document.SizeBytes != 3 {
		t.Fatalf("new version changed active metadata before success: detail=%#v error=%v", beforeActivation, err)
	}
	previous, err := repository.CompleteJob(context.Background(), job.ID, 2)
	if err != nil || previous == nil || previous.ID != versionOne.ID {
		t.Fatalf("previous = %#v, error = %v", previous, err)
	}
	detail, err := repository.GetDocument(context.Background(), document.ID)
	if err != nil || detail.Document.CurrentVersion != 2 || detail.Document.Checksum != versionTwo.Checksum || detail.Document.SizeBytes != 4 || detail.Current == nil || detail.Current.ChunkCount != 2 {
		t.Fatalf("detail = %#v, error = %v", detail, err)
	}

	failed := domain.IngestionJob{DocumentID: document.ID, VersionID: versionTwo.ID, Status: domain.IngestionJobFailed, Stage: domain.IngestionStageParse}
	if err := db.Create(&failed).Error; err != nil {
		t.Fatal(err)
	}
	var successes atomic.Int32
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, retryErr := repository.RetryJob(context.Background(), failed.ID, 3)
			if retryErr == nil {
				successes.Add(1)
			} else if !errors.Is(retryErr, ErrConflict) {
				t.Errorf("RetryJob() error = %v", retryErr)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful retries = %d, want 1", successes.Load())
	}
}

func TestIngestionCancellationQueuedAndRunningIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run MySQL integration tests")
	}
	db, err := database.OpenMySQL(config.Load().Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repository := NewGormRepository(db)
	create := func(name string) (domain.Document, domain.IngestionJob) {
		document := domain.Document{Filename: name + ".mp4", MediaType: "video/mp4", SizeBytes: 12, Checksum: fmt.Sprintf("%064d", time.Now().UnixNano()), Status: domain.DocumentStatusQueued}
		version := domain.DocumentVersion{StorageKey: fmt.Sprintf("test/%d.mp4", time.Now().UnixNano()), Filename: document.Filename, MediaType: document.MediaType, SizeBytes: document.SizeBytes, Checksum: document.Checksum, ParserVersion: ParserVersion}
		job := domain.IngestionJob{Status: domain.IngestionJobQueued, Stage: domain.IngestionStageUpload}
		if err := repository.CreateDocument(context.Background(), &document, &version, &job); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanupKnowledgeRows(db, document.ID) })
		return document, job
	}
	queuedDocument, queued := create("cancel-queued")
	canceled, err := repository.RequestCancel(context.Background(), queued.ID)
	if err != nil || canceled.Status != domain.IngestionJobCanceled || canceled.CancelRequestedAt == nil {
		t.Fatalf("queued cancel=%#v error=%v", canceled, err)
	}
	if _, _, _, err := repository.ClaimJob(context.Background(), queued.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("claim canceled job error=%v", err)
	}
	queuedDetail, err := repository.GetDocument(context.Background(), queuedDocument.ID)
	if err != nil || queuedDetail.Document.Status != domain.DocumentStatusCanceled {
		t.Fatalf("queued canceled document=%#v error=%v", queuedDetail.Document, err)
	}

	runningDocument, running := create("cancel-running")
	if _, _, _, err := repository.ClaimJob(context.Background(), running.ID); err != nil {
		t.Fatal(err)
	}
	requested, err := repository.RequestCancel(context.Background(), running.ID)
	if err != nil || requested.Status != domain.IngestionJobRunning || requested.CancelRequestedAt == nil {
		t.Fatalf("running cancel request=%#v error=%v", requested, err)
	}
	if err := repository.CancelJob(context.Background(), running.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := repository.GetJob(context.Background(), running.ID)
	if err != nil || finished.Status != domain.IngestionJobCanceled || finished.SafeErrorCode != "canceled" {
		t.Fatalf("finished cancellation=%#v error=%v", finished, err)
	}
	runningDetail, err := repository.GetDocument(context.Background(), runningDocument.ID)
	if err != nil || runningDetail.Document.Status != domain.DocumentStatusCanceled {
		t.Fatalf("running canceled document=%#v error=%v", runningDetail.Document, err)
	}
}

func cleanupKnowledgeRows(db *gorm.DB, documentID uint64) {
	db.Where("document_id = ?", documentID).Delete(&domain.DocumentArtifact{})
	db.Where("document_id = ?", documentID).Delete(&domain.IngestionJob{})
	db.Where("document_id = ?", documentID).Delete(&domain.DocumentVersion{})
	db.Delete(&domain.Document{}, documentID)
}
