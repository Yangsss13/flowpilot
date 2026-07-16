package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
	"github.com/Yangsss13/flowpilot/internal/database"
	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type integrationParser struct {
	mu   sync.Mutex
	fail bool
}

func (p *integrationParser) Parse(_ context.Context, path, _ string, _ ParserLimits) ([]ParsedBlock, error) {
	p.mu.Lock()
	fail := p.fail
	p.mu.Unlock()
	if fail {
		return nil, fmt.Errorf("deliberate parser failure at %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return []ParsedBlock{{Text: string(content), Section: "integration"}}, nil
}

func (p *integrationParser) setFail(value bool) {
	p.mu.Lock()
	p.fail = value
	p.mu.Unlock()
}

type integrationKnowledgeEmbedder struct{}

func (integrationKnowledgeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for index, text := range texts {
		if strings.Contains(strings.ToLower(text), "refund") {
			vectors[index] = []float32{1, 0, 0}
		} else {
			vectors[index] = []float32{0, 1, 0}
		}
	}
	return vectors, nil
}

func TestAsyncKnowledgeIngestionRabbitMySQLQdrantIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run knowledge infrastructure integration tests")
	}
	cfg := config.Load()
	db, err := database.OpenMySQL(cfg.Database)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	rabbit, err := database.OpenRabbitMQ(cfg.RabbitMQ)
	if err != nil {
		t.Fatal(err)
	}
	defer rabbit.Close()
	channel, err := rabbit.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer channel.Close()
	if _, err := channel.QueueDeclare(QueueName, true, false, false, false, nil); err != nil {
		t.Fatal(err)
	}
	_, _ = channel.QueuePurge(QueueName, false)
	defer channel.QueuePurge(QueueName, false)

	collection := fmt.Sprintf("flowpilot_ingestion_test_%d", time.Now().UnixNano())
	store, err := rag.NewQdrantStore(cfg.Qdrant.URL, collection, cfg.Qdrant.APIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteIntegrationCollection(cfg.Qdrant, collection)
	engine := rag.NewService(integrationKnowledgeEmbedder{}, store)
	repository := NewGormRepository(db)
	storage, err := NewLocalObjectStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := NewRabbitPublisher(rabbit)
	if err != nil {
		t.Fatal(err)
	}
	defer publisher.Close()
	parser := &integrationParser{}
	cfg.Knowledge.ChunkMaxRunes = 1200
	cfg.Knowledge.MaxRetries = 2
	worker := NewWorker(repository, storage, parser, engine, cfg.Knowledge)
	consumer := NewConsumer(rabbit, worker)
	if err := consumer.Start(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = consumer.Stop(ctx)
	}()
	service := NewService(repository, storage, publisher, engine, cfg.Knowledge)

	result, err := service.Upload(context.Background(), UploadInput{Filename: "policy.txt", DeclaredType: "text/plain", Source: strings.NewReader("refund within seven days")})
	if err != nil || result.Status != domain.IngestionJobQueued {
		t.Fatalf("upload result=%#v error=%v", result, err)
	}
	defer cleanupKnowledgeRows(db, result.DocumentID)
	waitForKnowledgeJob(t, repository, result.JobID, domain.IngestionJobSuccess)
	detail, err := repository.GetDocument(context.Background(), result.DocumentID)
	if err != nil || detail.Document.Status != domain.DocumentStatusReady || detail.Current == nil || detail.Current.ChunkCount != 1 {
		t.Fatalf("detail=%#v error=%v", detail, err)
	}

	duplicate, err := service.Upload(context.Background(), UploadInput{Filename: "copy.txt", DeclaredType: "text/plain", Source: strings.NewReader("refund within seven days")})
	if err != nil || !duplicate.Deduplicated || duplicate.DocumentID != result.DocumentID || duplicate.JobID != result.JobID {
		t.Fatalf("duplicate=%#v error=%v", duplicate, err)
	}
	searchResults, err := service.Search(context.Background(), SearchRequest{Query: "refund", TopK: 5, MinScore: 0.5})
	if err != nil || len(searchResults) != 1 || searchResults[0].DocumentID != strconv.FormatUint(result.DocumentID, 10) {
		t.Fatalf("search=%#v error=%v", searchResults, err)
	}

	version, err := service.UploadVersion(context.Background(), result.DocumentID, UploadInput{Filename: "policy.txt", DeclaredType: "text/plain", Source: strings.NewReader("refund within fourteen days")})
	if err != nil {
		t.Fatal(err)
	}
	waitForKnowledgeJob(t, repository, version.JobID, domain.IngestionJobSuccess)
	detail, _ = repository.GetDocument(context.Background(), result.DocumentID)
	if detail.Document.CurrentVersion != 2 {
		t.Fatalf("current version = %d, want 2", detail.Document.CurrentVersion)
	}
	raw, err := store.Query(context.Background(), []float32{1, 0, 0}, 10)
	if err != nil || len(raw) != 1 || raw[0].VersionID != 2 {
		t.Fatalf("Qdrant versions after replacement = %#v, error=%v", raw, err)
	}

	parser.setFail(true)
	failed, err := service.Upload(context.Background(), UploadInput{Filename: "failure.txt", DeclaredType: "text/plain", Source: strings.NewReader("a different document")})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupKnowledgeRows(db, failed.DocumentID)
	waitForKnowledgeJob(t, repository, failed.JobID, domain.IngestionJobFailed)
	parser.setFail(false)
	retried, err := service.Retry(context.Background(), failed.JobID)
	if err != nil || retried.Status != domain.IngestionJobQueued {
		t.Fatalf("retry=%#v error=%v", retried, err)
	}
	waitForKnowledgeJob(t, repository, failed.JobID, domain.IngestionJobSuccess)

	if err := service.Delete(context.Background(), result.DocumentID); err != nil {
		t.Fatal(err)
	}
	searchResults, err = service.Search(context.Background(), SearchRequest{Query: "refund", TopK: 5, MinScore: 0.5})
	if err != nil || len(searchResults) != 0 {
		t.Fatalf("search after delete mark=%#v error=%v", searchResults, err)
	}
	if err := worker.CleanupDeleting(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	raw, err = store.Query(context.Background(), []float32{1, 0, 0}, 10)
	deletedID := strconv.FormatUint(result.DocumentID, 10)
	containsDeleted := false
	for _, item := range raw {
		containsDeleted = containsDeleted || item.DocumentID == deletedID
	}
	if err != nil || containsDeleted {
		t.Fatalf("Qdrant after delete=%#v error=%v", raw, err)
	}
}

func waitForKnowledgeJob(t *testing.T, repository *GormRepository, id uint64, status domain.IngestionJobStatus) domain.IngestionJob {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repository.GetJob(context.Background(), id)
		if err == nil && job.Status == status {
			return job
		}
		time.Sleep(25 * time.Millisecond)
	}
	job, err := repository.GetJob(context.Background(), id)
	t.Fatalf("job %d did not reach %s: job=%#v error=%v", id, status, job, err)
	return domain.IngestionJob{}
}

func deleteIntegrationCollection(cfg config.QdrantConfig, collection string) {
	request, err := http.NewRequest(http.MethodDelete, strings.TrimRight(cfg.URL, "/")+"/collections/"+url.PathEscape(collection), nil)
	if err != nil {
		return
	}
	if cfg.APIKey != "" {
		request.Header.Set("api-key", cfg.APIKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err == nil {
		_ = response.Body.Close()
	}
}
