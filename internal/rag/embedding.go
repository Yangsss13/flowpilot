package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxEmbeddingResponseBytes = 16 << 20

type OpenAICompatibleEmbedder struct {
	endpoint   string
	apiKey     string
	model      string
	httpClient *http.Client
}

type embeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

func NewOpenAICompatibleEmbedder(baseURL, apiKey, model string, client *http.Client) (*OpenAICompatibleEmbedder, error) {
	baseURL = strings.TrimSpace(baseURL)
	apiKey = strings.TrimSpace(apiKey)
	model = strings.TrimSpace(model)
	target, err := url.Parse(baseURL)
	if baseURL == "" || err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("AI base URL must be an absolute HTTP or HTTPS URL")
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, fmt.Errorf("AI base URL must not contain credentials, query, or fragment")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("AI API key is required")
	}
	if model == "" {
		return nil, fmt.Errorf("AI embedding model is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAICompatibleEmbedder{
		endpoint:   strings.TrimRight(baseURL, "/") + "/embeddings",
		apiKey:     apiKey,
		model:      model,
		httpClient: client,
	}, nil
}

func (e *OpenAICompatibleEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("embedding input is empty")
	}
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("embedding input contains empty text")
		}
	}
	payload, err := json.Marshal(embeddingRequest{Model: e.model, Input: texts, EncodingFormat: "float"})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+e.apiKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := e.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("embedding request returned HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxEmbeddingResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if len(body) > maxEmbeddingResponseBytes {
		return nil, fmt.Errorf("embedding response exceeds %d bytes", maxEmbeddingResponseBytes)
	}
	var result embeddingResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if len(result.Data) != len(texts) {
		return nil, fmt.Errorf("embedding response contains %d vectors for %d inputs", len(result.Data), len(texts))
	}
	vectors := make([][]float32, len(texts))
	dimension := 0
	for _, item := range result.Data {
		if item.Index < 0 || item.Index >= len(texts) || vectors[item.Index] != nil {
			return nil, fmt.Errorf("embedding response contains invalid index %d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("embedding response contains an empty vector")
		}
		if dimension == 0 {
			dimension = len(item.Embedding)
		} else if len(item.Embedding) != dimension {
			return nil, fmt.Errorf("embedding response contains inconsistent dimensions")
		}
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}
