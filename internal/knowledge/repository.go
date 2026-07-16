package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Yangsss13/flowpilot/internal/domain"
)

type DocumentFilter struct {
	Page      int
	PageSize  int
	Status    domain.DocumentStatus
	Extension string
	Query     string
}

type DocumentList struct {
	Items    []domain.Document
	Total    int64
	Page     int
	PageSize int
}

type DocumentDetail struct {
	Document  domain.Document         `json:"document"`
	Current   *domain.DocumentVersion `json:"current_version,omitempty"`
	LatestJob *domain.IngestionJob    `json:"latest_job,omitempty"`
}

type DuplicateUpload struct {
	Document domain.Document
	Version  domain.DocumentVersion
	Job      domain.IngestionJob
}

type GormRepository struct {
	db *gorm.DB
}

func NewGormRepository(db *gorm.DB) *GormRepository {
	return &GormRepository{db: db}
}

func (r *GormRepository) FindDuplicate(ctx context.Context, checksum, parserVersion string) (DuplicateUpload, error) {
	var version domain.DocumentVersion
	err := r.db.WithContext(ctx).
		Table("document_versions").
		Joins("JOIN documents ON documents.id = document_versions.document_id AND documents.status <> ?", domain.DocumentStatusDeleting).
		Where("document_versions.checksum = ? AND document_versions.parser_version = ?", checksum, parserVersion).
		Order("document_versions.id DESC").
		First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DuplicateUpload{}, ErrNotFound
	}
	if err != nil {
		return DuplicateUpload{}, fmt.Errorf("find duplicate document: %w", err)
	}
	var document domain.Document
	if err := r.db.WithContext(ctx).First(&document, version.DocumentID).Error; err != nil {
		return DuplicateUpload{}, fmt.Errorf("load duplicate document: %w", err)
	}
	var job domain.IngestionJob
	if err := r.db.WithContext(ctx).Where("version_id = ?", version.ID).Order("id DESC").First(&job).Error; err != nil {
		return DuplicateUpload{}, fmt.Errorf("load duplicate ingestion job: %w", err)
	}
	return DuplicateUpload{Document: document, Version: version, Job: job}, nil
}

func (r *GormRepository) CreateDocument(ctx context.Context, document *domain.Document, version *domain.DocumentVersion, job *domain.IngestionJob) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(document).Error; err != nil {
			return fmt.Errorf("create document: %w", err)
		}
		version.DocumentID = document.ID
		version.Version = 1
		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create document version: %w", err)
		}
		job.DocumentID = document.ID
		job.VersionID = version.ID
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("create ingestion job: %w", err)
		}
		return nil
	})
}

func (r *GormRepository) CreateVersion(ctx context.Context, documentID uint64, version *domain.DocumentVersion, job *domain.IngestionJob) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var document domain.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&document, documentID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock document: %w", err)
		}
		if document.Status == domain.DocumentStatusDeleting {
			return ErrConflict
		}
		var maximum uint64
		if err := tx.Model(&domain.DocumentVersion{}).Where("document_id = ?", documentID).Select("COALESCE(MAX(version), 0)").Scan(&maximum).Error; err != nil {
			return fmt.Errorf("find latest version: %w", err)
		}
		version.DocumentID = documentID
		version.Version = maximum + 1
		if err := tx.Create(version).Error; err != nil {
			return fmt.Errorf("create document version: %w", err)
		}
		job.DocumentID = documentID
		job.VersionID = version.ID
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("create ingestion job: %w", err)
		}
		if document.CurrentVersion == 0 {
			if err := tx.Model(&domain.Document{}).Where("id = ?", documentID).Update("status", domain.DocumentStatusQueued).Error; err != nil {
				return fmt.Errorf("queue document version: %w", err)
			}
		}
		return nil
	})
}

func (r *GormRepository) GetDocument(ctx context.Context, id uint64) (DocumentDetail, error) {
	var document domain.Document
	if err := r.db.WithContext(ctx).First(&document, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DocumentDetail{}, ErrNotFound
		}
		return DocumentDetail{}, fmt.Errorf("get document: %w", err)
	}
	detail := DocumentDetail{Document: document}
	if document.CurrentVersion > 0 {
		var version domain.DocumentVersion
		if err := r.db.WithContext(ctx).Where("document_id = ? AND version = ?", id, document.CurrentVersion).First(&version).Error; err != nil {
			return DocumentDetail{}, fmt.Errorf("get current document version: %w", err)
		}
		detail.Current = &version
	}
	var job domain.IngestionJob
	if err := r.db.WithContext(ctx).Where("document_id = ?", id).Order("id DESC").First(&job).Error; err == nil {
		detail.LatestJob = &job
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return DocumentDetail{}, fmt.Errorf("get latest ingestion job: %w", err)
	}
	return detail, nil
}

func (r *GormRepository) ListDocuments(ctx context.Context, filter DocumentFilter) (DocumentList, error) {
	query := r.db.WithContext(ctx).Model(&domain.Document{}).Where("status <> ?", domain.DocumentStatusDeleting)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Extension != "" {
		query = query.Where("LOWER(filename) LIKE ?", "%"+strings.ToLower(filter.Extension))
	}
	if filter.Query != "" {
		query = query.Where("filename LIKE ?", "%"+filter.Query+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return DocumentList{}, fmt.Errorf("count documents: %w", err)
	}
	var items []domain.Document
	if err := query.Order("id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&items).Error; err != nil {
		return DocumentList{}, fmt.Errorf("list documents: %w", err)
	}
	return DocumentList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}

func (r *GormRepository) GetJob(ctx context.Context, id uint64) (domain.IngestionJob, error) {
	var job domain.IngestionJob
	if err := r.db.WithContext(ctx).First(&job, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.IngestionJob{}, ErrNotFound
		}
		return domain.IngestionJob{}, fmt.Errorf("get ingestion job: %w", err)
	}
	return job, nil
}

func (r *GormRepository) GetVersion(ctx context.Context, id uint64) (domain.DocumentVersion, error) {
	var version domain.DocumentVersion
	if err := r.db.WithContext(ctx).First(&version, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.DocumentVersion{}, ErrNotFound
		}
		return domain.DocumentVersion{}, fmt.Errorf("get document version: %w", err)
	}
	return version, nil
}

func (r *GormRepository) ClaimJob(ctx context.Context, id uint64) (domain.IngestionJob, domain.Document, domain.DocumentVersion, error) {
	var job domain.IngestionJob
	var document domain.Document
	var version domain.DocumentVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		result := tx.Model(&domain.IngestionJob{}).
			Where("id = ? AND status = ?", id, domain.IngestionJobQueued).
			Updates(map[string]any{"status": domain.IngestionJobRunning, "stage": domain.IngestionStageUpload, "progress": 5, "started_at": now, "finished_at": nil})
		if result.Error != nil {
			return fmt.Errorf("claim ingestion job: %w", result.Error)
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		if err := tx.First(&job, id).Error; err != nil {
			return fmt.Errorf("load claimed job: %w", err)
		}
		if err := tx.First(&document, job.DocumentID).Error; err != nil {
			return fmt.Errorf("load job document: %w", err)
		}
		if document.Status == domain.DocumentStatusDeleting {
			return ErrConflict
		}
		if err := tx.First(&version, job.VersionID).Error; err != nil {
			return fmt.Errorf("load job version: %w", err)
		}
		if document.CurrentVersion == 0 {
			if err := tx.Model(&domain.Document{}).Where("id = ?", document.ID).Update("status", domain.DocumentStatusProcessing).Error; err != nil {
				return fmt.Errorf("mark document processing: %w", err)
			}
		}
		return nil
	})
	return job, document, version, err
}

func (r *GormRepository) UpdateJobStage(ctx context.Context, id uint64, stage domain.IngestionStage, progress int) error {
	result := r.db.WithContext(ctx).Model(&domain.IngestionJob{}).
		Where("id = ? AND status = ? AND cancel_requested_at IS NULL", id, domain.IngestionJobRunning).
		Updates(map[string]any{"stage": stage, "progress": progress})
	if result.Error != nil {
		return fmt.Errorf("update ingestion stage: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *GormRepository) CompleteJob(ctx context.Context, id uint64, chunkCount int) (*domain.DocumentVersion, error) {
	var previous *domain.DocumentVersion
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.IngestionJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			return fmt.Errorf("lock ingestion job: %w", err)
		}
		if job.Status != domain.IngestionJobRunning {
			return ErrConflict
		}
		if job.CancelRequestedAt != nil {
			return ErrConflict
		}
		var version domain.DocumentVersion
		if err := tx.First(&version, job.VersionID).Error; err != nil {
			return fmt.Errorf("load completed version: %w", err)
		}
		var document domain.Document
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&document, job.DocumentID).Error; err != nil {
			return fmt.Errorf("lock completed document: %w", err)
		}
		if document.Status == domain.DocumentStatusDeleting {
			return ErrConflict
		}
		if document.CurrentVersion > 0 && document.CurrentVersion != version.Version {
			var old domain.DocumentVersion
			if err := tx.Where("document_id = ? AND version = ?", document.ID, document.CurrentVersion).First(&old).Error; err == nil {
				previous = &old
			}
		}
		now := time.Now().UTC()
		if err := tx.Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobRunning).Updates(map[string]any{
			"status": domain.IngestionJobSuccess, "stage": domain.IngestionStageIndexing,
			"progress": 100, "finished_at": now, "safe_error_code": "", "safe_error_message": "",
		}).Error; err != nil {
			return fmt.Errorf("complete ingestion job: %w", err)
		}
		if err := tx.Model(&domain.DocumentVersion{}).Where("id = ?", version.ID).Update("chunk_count", chunkCount).Error; err != nil {
			return fmt.Errorf("update version chunk count: %w", err)
		}
		if err := tx.Model(&domain.Document{}).Where("id = ?", document.ID).Updates(map[string]any{
			"status": domain.DocumentStatusReady, "current_version": version.Version,
			"filename": version.Filename, "media_type": version.MediaType,
			"size_bytes": version.SizeBytes, "checksum": version.Checksum,
		}).Error; err != nil {
			return fmt.Errorf("activate document version: %w", err)
		}
		return nil
	})
	return previous, err
}

func (r *GormRepository) FailJob(ctx context.Context, id uint64, code, message string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.IngestionJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			return fmt.Errorf("lock failed job: %w", err)
		}
		if job.Status != domain.IngestionJobRunning {
			return ErrConflict
		}
		if job.CancelRequestedAt != nil {
			return ErrConflict
		}
		now := time.Now().UTC()
		if err := tx.Model(&domain.IngestionJob{}).Where("id = ?", id).Updates(map[string]any{
			"status": domain.IngestionJobFailed, "finished_at": now,
			"safe_error_code": code, "safe_error_message": message,
		}).Error; err != nil {
			return fmt.Errorf("fail ingestion job: %w", err)
		}
		var document domain.Document
		if err := tx.First(&document, job.DocumentID).Error; err != nil {
			return fmt.Errorf("load failed document: %w", err)
		}
		status := domain.DocumentStatusReady
		if document.CurrentVersion == 0 {
			status = domain.DocumentStatusFailed
		}
		return tx.Model(&domain.Document{}).Where("id = ? AND status <> ?", document.ID, domain.DocumentStatusDeleting).Update("status", status).Error
	})
}

func (r *GormRepository) RequeueJob(ctx context.Context, id uint64, maxRetries int, code, message string) (bool, error) {
	var retry bool
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.IngestionJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			return fmt.Errorf("lock retryable job: %w", err)
		}
		if job.Status != domain.IngestionJobRunning {
			return ErrConflict
		}
		if job.CancelRequestedAt != nil {
			return ErrConflict
		}
		if job.RetryCount >= maxRetries {
			now := time.Now().UTC()
			if err := tx.Model(&domain.IngestionJob{}).Where("id = ?", id).Updates(map[string]any{
				"status": domain.IngestionJobFailed, "finished_at": now,
				"safe_error_code": code, "safe_error_message": message,
			}).Error; err != nil {
				return fmt.Errorf("exhaust ingestion retries: %w", err)
			}
			var document domain.Document
			if err := tx.First(&document, job.DocumentID).Error; err != nil {
				return fmt.Errorf("load retryable document: %w", err)
			}
			if document.CurrentVersion == 0 {
				if err := tx.Model(&domain.Document{}).Where("id = ?", document.ID).Update("status", domain.DocumentStatusFailed).Error; err != nil {
					return fmt.Errorf("fail retried document: %w", err)
				}
			}
			return nil
		}
		retry = true
		if err := tx.Model(&domain.IngestionJob{}).Where("id = ?", id).Updates(map[string]any{
			"status": domain.IngestionJobQueued, "stage": domain.IngestionStageUpload, "progress": 0,
			"retry_count": gorm.Expr("retry_count + 1"), "started_at": nil, "finished_at": nil,
			"safe_error_code": "", "safe_error_message": "",
		}).Error; err != nil {
			return fmt.Errorf("requeue ingestion job: %w", err)
		}
		return nil
	})
	return retry, err
}

func (r *GormRepository) RetryJob(ctx context.Context, id uint64, maxRetries int) (domain.IngestionJob, error) {
	var job domain.IngestionJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock retry job: %w", err)
		}
		if job.Status != domain.IngestionJobFailed || job.RetryCount >= maxRetries {
			return ErrConflict
		}
		if err := tx.Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobFailed).Updates(map[string]any{
			"status": domain.IngestionJobQueued, "stage": domain.IngestionStageUpload, "progress": 0,
			"safe_error_code": "", "safe_error_message": "", "retry_count": gorm.Expr("retry_count + 1"),
			"started_at": nil, "finished_at": nil, "dispatched_at": nil, "cancel_requested_at": nil,
		}).Error; err != nil {
			return fmt.Errorf("queue retry job: %w", err)
		}
		if err := tx.First(&job, id).Error; err != nil {
			return fmt.Errorf("reload retry job: %w", err)
		}
		return nil
	})
	return job, err
}

func (r *GormRepository) RecoverInterrupted(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Model(&domain.IngestionJob{}).Where("status = ? AND cancel_requested_at IS NOT NULL", domain.IngestionJobRunning).Updates(map[string]any{
			"status": domain.IngestionJobCanceled, "finished_at": now, "safe_error_code": "canceled", "safe_error_message": "ingestion was canceled",
		}).Error; err != nil {
			return fmt.Errorf("recover canceled ingestion jobs: %w", err)
		}
		canceledDocuments := tx.Model(&domain.IngestionJob{}).Select("document_id").Where("status = ?", domain.IngestionJobCanceled)
		if err := tx.Model(&domain.Document{}).Where(
			"current_version = 0 AND id IN (?) AND NOT EXISTS (SELECT 1 FROM ingestion_jobs active_jobs WHERE active_jobs.document_id = documents.id AND active_jobs.status IN ?)",
			canceledDocuments, []domain.IngestionJobStatus{domain.IngestionJobQueued, domain.IngestionJobRunning},
		).Update("status", domain.DocumentStatusCanceled).Error; err != nil {
			return fmt.Errorf("recover canceled documents: %w", err)
		}
		if err := tx.Model(&domain.IngestionJob{}).Where("status = ? AND cancel_requested_at IS NULL", domain.IngestionJobRunning).Updates(map[string]any{
			"status": domain.IngestionJobQueued, "stage": domain.IngestionStageUpload,
			"progress": 0, "started_at": nil, "finished_at": nil, "dispatched_at": nil,
		}).Error; err != nil {
			return fmt.Errorf("recover running ingestion jobs: %w", err)
		}
		if err := tx.Model(&domain.Document{}).Where("status = ? AND current_version = 0", domain.DocumentStatusProcessing).Update("status", domain.DocumentStatusQueued).Error; err != nil {
			return fmt.Errorf("recover processing documents: %w", err)
		}
		return nil
	})
}

func (r *GormRepository) RequestCancel(ctx context.Context, id uint64) (domain.IngestionJob, error) {
	var job domain.IngestionJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock canceled ingestion job: %w", err)
		}
		now := time.Now().UTC()
		switch job.Status {
		case domain.IngestionJobQueued:
			if err := tx.Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobQueued).Updates(map[string]any{
				"status": domain.IngestionJobCanceled, "finished_at": now, "cancel_requested_at": now,
				"safe_error_code": "canceled", "safe_error_message": "ingestion was canceled",
			}).Error; err != nil {
				return fmt.Errorf("cancel queued ingestion job: %w", err)
			}
			var document domain.Document
			if err := tx.First(&document, job.DocumentID).Error; err != nil {
				return fmt.Errorf("load canceled document: %w", err)
			}
			if document.CurrentVersion == 0 {
				if err := tx.Model(&domain.Document{}).Where("id = ?", document.ID).Update("status", domain.DocumentStatusCanceled).Error; err != nil {
					return fmt.Errorf("mark canceled document failed: %w", err)
				}
			}
		case domain.IngestionJobRunning:
			if job.CancelRequestedAt != nil {
				return ErrConflict
			}
			if err := tx.Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobRunning).Update("cancel_requested_at", now).Error; err != nil {
				return fmt.Errorf("request running ingestion cancellation: %w", err)
			}
		default:
			return ErrConflict
		}
		return tx.First(&job, id).Error
	})
	return job, err
}

func (r *GormRepository) CancelJob(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job domain.IngestionJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, id).Error; err != nil {
			return fmt.Errorf("lock ingestion cancellation: %w", err)
		}
		if job.Status == domain.IngestionJobCanceled {
			return nil
		}
		if job.Status != domain.IngestionJobRunning || job.CancelRequestedAt == nil {
			return ErrConflict
		}
		now := time.Now().UTC()
		if err := tx.Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobRunning).Updates(map[string]any{
			"status": domain.IngestionJobCanceled, "finished_at": now,
			"safe_error_code": "canceled", "safe_error_message": "ingestion was canceled",
		}).Error; err != nil {
			return fmt.Errorf("finish ingestion cancellation: %w", err)
		}
		var document domain.Document
		if err := tx.First(&document, job.DocumentID).Error; err != nil {
			return fmt.Errorf("load canceled document: %w", err)
		}
		if document.CurrentVersion == 0 {
			return tx.Model(&domain.Document{}).Where("id = ?", document.ID).Update("status", domain.DocumentStatusCanceled).Error
		}
		return nil
	})
}

func (r *GormRepository) IsCancelRequested(ctx context.Context, id uint64) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&domain.IngestionJob{}).
		Where("id = ? AND status = ? AND cancel_requested_at IS NOT NULL", id, domain.IngestionJobRunning).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("check ingestion cancellation: %w", err)
	}
	return count == 1, nil
}

func (r *GormRepository) CreateArtifact(ctx context.Context, artifact *domain.DocumentArtifact) error {
	if err := r.db.WithContext(ctx).Create(artifact).Error; err != nil {
		return fmt.Errorf("create document artifact: %w", err)
	}
	return nil
}

func (r *GormRepository) ListArtifactsForDocument(ctx context.Context, documentID uint64) ([]domain.DocumentArtifact, error) {
	var artifacts []domain.DocumentArtifact
	if err := r.db.WithContext(ctx).Where("document_id = ?", documentID).Order("id").Find(&artifacts).Error; err != nil {
		return nil, fmt.Errorf("list document artifacts: %w", err)
	}
	return artifacts, nil
}

func (r *GormRepository) ListArtifactsForVersion(ctx context.Context, versionID uint64) ([]domain.DocumentArtifact, error) {
	var artifacts []domain.DocumentArtifact
	if err := r.db.WithContext(ctx).Where("version_id = ?", versionID).Order("id").Find(&artifacts).Error; err != nil {
		return nil, fmt.Errorf("list version artifacts: %w", err)
	}
	return artifacts, nil
}

func (r *GormRepository) DeleteArtifactRecords(ctx context.Context, versionID uint64) error {
	if err := r.db.WithContext(ctx).Where("version_id = ?", versionID).Delete(&domain.DocumentArtifact{}).Error; err != nil {
		return fmt.Errorf("delete artifact records: %w", err)
	}
	return nil
}

func (r *GormRepository) ListQueuedJobIDs(ctx context.Context, limit int) ([]uint64, error) {
	var ids []uint64
	if err := r.db.WithContext(ctx).Model(&domain.IngestionJob{}).Where("status = ? AND dispatched_at IS NULL", domain.IngestionJobQueued).Order("id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("list queued ingestion jobs: %w", err)
	}
	return ids, nil
}

func (r *GormRepository) ListCanceledJobsForCleanup(ctx context.Context, limit int) ([]domain.IngestionJob, error) {
	var jobs []domain.IngestionJob
	if err := r.db.WithContext(ctx).Where("status = ? AND cleanup_completed_at IS NULL", domain.IngestionJobCanceled).Order("id").Limit(limit).Find(&jobs).Error; err != nil {
		return nil, fmt.Errorf("list canceled jobs for cleanup: %w", err)
	}
	return jobs, nil
}

func (r *GormRepository) MarkJobCleanupComplete(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	if err := r.db.WithContext(ctx).Model(&domain.IngestionJob{}).Where("id = ? AND status = ?", id, domain.IngestionJobCanceled).Update("cleanup_completed_at", now).Error; err != nil {
		return fmt.Errorf("mark ingestion cleanup complete: %w", err)
	}
	return nil
}

func (r *GormRepository) MarkJobDispatched(ctx context.Context, id uint64) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&domain.IngestionJob{}).
		Where("id = ? AND status = ? AND dispatched_at IS NULL", id, domain.IngestionJobQueued).
		Update("dispatched_at", now)
	if result.Error != nil {
		return fmt.Errorf("mark ingestion job dispatched: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func (r *GormRepository) MarkDeleting(ctx context.Context, id uint64) error {
	result := r.db.WithContext(ctx).Model(&domain.Document{}).
		Where("id = ? AND status <> ?", id, domain.DocumentStatusDeleting).
		Update("status", domain.DocumentStatusDeleting)
	if result.Error != nil {
		return fmt.Errorf("mark document deleting: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		var count int64
		if err := r.db.WithContext(ctx).Model(&domain.Document{}).Where("id = ?", id).Count(&count).Error; err != nil {
			return fmt.Errorf("check deleted document: %w", err)
		}
		if count == 0 {
			return ErrNotFound
		}
		return ErrConflict
	}
	return nil
}

func (r *GormRepository) ListDeleting(ctx context.Context, limit int) ([]domain.Document, error) {
	var documents []domain.Document
	if err := r.db.WithContext(ctx).Where("status = ?", domain.DocumentStatusDeleting).Order("id").Limit(limit).Find(&documents).Error; err != nil {
		return nil, fmt.Errorf("list deleting documents: %w", err)
	}
	return documents, nil
}

func (r *GormRepository) ListReadyDocuments(ctx context.Context, limit int) ([]domain.Document, error) {
	var documents []domain.Document
	if err := r.db.WithContext(ctx).Where("status = ? AND current_version > 1", domain.DocumentStatusReady).Order("id").Limit(limit).Find(&documents).Error; err != nil {
		return nil, fmt.Errorf("list versioned documents: %w", err)
	}
	return documents, nil
}

func (r *GormRepository) ListVersions(ctx context.Context, documentID uint64) ([]domain.DocumentVersion, error) {
	var versions []domain.DocumentVersion
	if err := r.db.WithContext(ctx).Where("document_id = ?", documentID).Order("version").Find(&versions).Error; err != nil {
		return nil, fmt.Errorf("list document versions: %w", err)
	}
	return versions, nil
}

func (r *GormRepository) DeleteMetadata(ctx context.Context, documentID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("document_id = ?", documentID).Delete(&domain.DocumentArtifact{}).Error; err != nil {
			return fmt.Errorf("delete document artifacts: %w", err)
		}
		if err := tx.Where("document_id = ?", documentID).Delete(&domain.IngestionJob{}).Error; err != nil {
			return fmt.Errorf("delete ingestion jobs: %w", err)
		}
		if err := tx.Where("document_id = ?", documentID).Delete(&domain.DocumentVersion{}).Error; err != nil {
			return fmt.Errorf("delete document versions: %w", err)
		}
		if err := tx.Where("id = ? AND status = ?", documentID, domain.DocumentStatusDeleting).Delete(&domain.Document{}).Error; err != nil {
			return fmt.Errorf("delete document: %w", err)
		}
		return nil
	})
}

func (r *GormRepository) ActiveVersions(ctx context.Context, documentIDs []uint64) (map[uint64]uint64, error) {
	if len(documentIDs) == 0 {
		return map[uint64]uint64{}, nil
	}
	type row struct {
		ID             uint64
		CurrentVersion uint64
	}
	var rows []row
	if err := r.db.WithContext(ctx).Model(&domain.Document{}).
		Select("id, current_version").
		Where("id IN ? AND status = ?", documentIDs, domain.DocumentStatusReady).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load active document versions: %w", err)
	}
	versions := make(map[uint64]uint64, len(rows))
	for _, value := range rows {
		versions[value.ID] = value.CurrentVersion
	}
	return versions, nil
}
