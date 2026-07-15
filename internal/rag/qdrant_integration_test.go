package rag

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Yangsss13/flowpilot/internal/config"
)

type integrationEmbedder struct{}

func (e *integrationEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
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

func TestQdrantStoreIntegration(t *testing.T) {
	if os.Getenv("FLOWPILOT_INTEGRATION") != "1" {
		t.Skip("set FLOWPILOT_INTEGRATION=1 to run Qdrant integration tests")
	}
	cfg := config.Load().Qdrant
	collection := fmt.Sprintf("flowpilot_test_%d", time.Now().UnixNano())
	store, err := NewQdrantStore(cfg.URL, collection, cfg.APIKey, nil)
	if err != nil {
		t.Fatalf("create Qdrant store: %v", err)
	}
	t.Cleanup(func() {
		request, requestErr := http.NewRequest(http.MethodDelete, strings.TrimRight(cfg.URL, "/")+"/collections/"+url.PathEscape(collection), nil)
		if requestErr != nil {
			t.Errorf("create cleanup request: %v", requestErr)
			return
		}
		if cfg.APIKey != "" {
			request.Header.Set("api-key", cfg.APIKey)
		}
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
		}
	})
	service := NewService(&integrationEmbedder{}, store)
	if _, err := service.Import(context.Background(), "refund.md", []byte("refund policy allows returns within seven days")); err != nil {
		t.Fatalf("import refund document: %v", err)
	}
	if _, err := service.Import(context.Background(), "shipping.md", []byte("shipping usually takes three days")); err != nil {
		t.Fatalf("import shipping document: %v", err)
	}
	results, err := service.Search(context.Background(), "refund rules", 1)
	if err != nil {
		t.Fatalf("Query() returned error: %v", err)
	}
	if len(results) != 1 || results[0].Text != "refund policy allows returns within seven days" || results[0].Source != "refund.md" {
		t.Fatalf("Query() results = %#v", results)
	}
}
