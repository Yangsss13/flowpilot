package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxQdrantResponseBytes = 4 << 20

var collectionNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

type QdrantStore struct {
	baseURL    string
	collection string
	apiKey     string
	httpClient *http.Client
}

type qdrantPayload struct {
	DocumentID string `json:"document_id"`
	VersionID  uint64 `json:"version_id"`
	Source     string `json:"source"`
	Section    string `json:"section,omitempty"`
	Page       int    `json:"page,omitempty"`
	Slide      int    `json:"slide,omitempty"`
	StartMS    int64  `json:"start_ms,omitempty"`
	EndMS      int64  `json:"end_ms,omitempty"`
	ChunkIndex int    `json:"chunk_index"`
	Text       string `json:"text"`
}

type qdrantVectorParams struct {
	Size     int    `json:"size"`
	Distance string `json:"distance"`
}

func NewQdrantStore(baseURL, collection, apiKey string, client *http.Client) (*QdrantStore, error) {
	baseURL = strings.TrimSpace(baseURL)
	collection = strings.TrimSpace(collection)
	target, err := url.Parse(baseURL)
	if baseURL == "" || err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil, fmt.Errorf("Qdrant URL must be an absolute HTTP or HTTPS URL")
	}
	if target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, fmt.Errorf("Qdrant URL must not contain credentials, query, or fragment")
	}
	if !collectionNamePattern.MatchString(collection) {
		return nil, fmt.Errorf("Qdrant collection must contain only letters, numbers, underscores, or hyphens")
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &QdrantStore{
		baseURL:    strings.TrimRight(baseURL, "/"),
		collection: collection,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: client,
	}, nil
}

func (s *QdrantStore) Upsert(ctx context.Context, vectorSize int, points []VectorPoint) error {
	if vectorSize <= 0 || len(points) == 0 {
		return fmt.Errorf("vector size and points are required")
	}
	if err := s.ensureCollection(ctx, vectorSize); err != nil {
		return err
	}
	type point struct {
		ID      string        `json:"id"`
		Vector  []float32     `json:"vector"`
		Payload qdrantPayload `json:"payload"`
	}
	requestPoints := make([]point, len(points))
	for index, value := range points {
		if len(value.Vector) != vectorSize {
			return fmt.Errorf("point %d vector dimension is %d, want %d", index, len(value.Vector), vectorSize)
		}
		requestPoints[index] = point{
			ID:     value.ID,
			Vector: value.Vector,
			Payload: qdrantPayload{
				DocumentID: value.DocumentID,
				VersionID:  value.VersionID,
				Source:     value.Source,
				Section:    value.Section,
				Page:       value.Page,
				Slide:      value.Slide,
				StartMS:    value.StartMS,
				EndMS:      value.EndMS,
				ChunkIndex: value.ChunkIndex,
				Text:       value.Text,
			},
		}
	}
	status, _, err := s.doJSON(ctx, http.MethodPut, s.collectionPath()+"/points?wait=true", struct {
		Points []point `json:"points"`
	}{Points: requestPoints})
	if err != nil {
		return fmt.Errorf("upsert Qdrant points: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("upsert Qdrant points returned HTTP status %d", status)
	}
	return nil
}

func (s *QdrantStore) Query(ctx context.Context, vector []float32, limit int) ([]SearchResult, error) {
	return s.QueryWithThreshold(ctx, vector, limit, 0)
}

func (s *QdrantStore) QueryWithThreshold(ctx context.Context, vector []float32, limit int, minScore float64) ([]SearchResult, error) {
	if len(vector) == 0 || limit <= 0 {
		return nil, fmt.Errorf("query vector and positive limit are required")
	}
	request := struct {
		Query          []float32 `json:"query"`
		Limit          int       `json:"limit"`
		WithPayload    bool      `json:"with_payload"`
		WithVector     bool      `json:"with_vector"`
		ScoreThreshold *float64  `json:"score_threshold,omitempty"`
	}{Query: vector, Limit: limit, WithPayload: true, WithVector: false}
	if minScore > 0 {
		request.ScoreThreshold = &minScore
	}
	status, body, err := s.doJSON(ctx, http.MethodPost, s.collectionPath()+"/points/query", request)
	if err != nil {
		return nil, fmt.Errorf("query Qdrant points: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("query Qdrant points returned HTTP status %d", status)
	}
	var response struct {
		Result struct {
			Points []struct {
				Score   float64       `json:"score"`
				Payload qdrantPayload `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode Qdrant query response: %w", err)
	}
	results := make([]SearchResult, len(response.Result.Points))
	for index, point := range response.Result.Points {
		result := SearchResult{
			DocumentID: point.Payload.DocumentID,
			VersionID:  point.Payload.VersionID,
			Source:     point.Payload.Source,
			Section:    point.Payload.Section,
			Page:       point.Payload.Page,
			Slide:      point.Payload.Slide,
			StartMS:    point.Payload.StartMS,
			EndMS:      point.Payload.EndMS,
			ChunkIndex: point.Payload.ChunkIndex,
			Text:       point.Payload.Text,
			Score:      point.Score,
		}
		if point.Payload.EndMS > 0 {
			result.StartTime = FormatTimestamp(point.Payload.StartMS)
			result.EndTime = FormatTimestamp(point.Payload.EndMS)
		}
		results[index] = result
	}
	return results, nil
}

func FormatTimestamp(milliseconds int64) string {
	if milliseconds < 0 {
		milliseconds = 0
	}
	totalSeconds := milliseconds / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func (s *QdrantStore) DeleteDocument(ctx context.Context, documentID string) error {
	return s.deleteByFilter(ctx, []any{qdrantMatch("document_id", documentID)})
}

func (s *QdrantStore) DeleteVersion(ctx context.Context, documentID string, versionID uint64) error {
	return s.deleteByFilter(ctx, []any{qdrantMatch("document_id", documentID), qdrantMatch("version_id", versionID)})
}

func qdrantMatch(key string, value any) map[string]any {
	return map[string]any{"key": key, "match": map[string]any{"value": value}}
}

func (s *QdrantStore) deleteByFilter(ctx context.Context, must []any) error {
	status, _, err := s.doJSON(ctx, http.MethodPost, s.collectionPath()+"/points/delete?wait=true", map[string]any{
		"filter": map[string]any{"must": must},
	})
	if err != nil {
		return fmt.Errorf("delete Qdrant points: %w", err)
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("delete Qdrant points returned HTTP status %d", status)
	}
	return nil
}

func (s *QdrantStore) Health(ctx context.Context) error {
	status, _, err := s.doJSON(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return fmt.Errorf("check Qdrant health: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("Qdrant health returned HTTP status %d", status)
	}
	return nil
}

func (s *QdrantStore) ensureCollection(ctx context.Context, vectorSize int) error {
	status, body, err := s.doJSON(ctx, http.MethodGet, s.collectionPath(), nil)
	if err != nil {
		return fmt.Errorf("get Qdrant collection: %w", err)
	}
	if status == http.StatusOK {
		var response struct {
			Result struct {
				Config struct {
					Params struct {
						Vectors struct {
							Size int `json:"size"`
						} `json:"vectors"`
					} `json:"params"`
				} `json:"config"`
			} `json:"result"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return fmt.Errorf("decode Qdrant collection response: %w", err)
		}
		existingSize := response.Result.Config.Params.Vectors.Size
		if existingSize != vectorSize {
			return fmt.Errorf("Qdrant collection vector size is %d, embedding size is %d", existingSize, vectorSize)
		}
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("get Qdrant collection returned HTTP status %d", status)
	}
	status, _, err = s.doJSON(ctx, http.MethodPut, s.collectionPath(), struct {
		Vectors qdrantVectorParams `json:"vectors"`
	}{Vectors: qdrantVectorParams{Size: vectorSize, Distance: "Cosine"}})
	if err != nil {
		return fmt.Errorf("create Qdrant collection: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("create Qdrant collection returned HTTP status %d", status)
	}
	return nil
}

func (s *QdrantStore) collectionPath() string {
	return "/collections/" + url.PathEscape(s.collection)
}

func (s *QdrantStore) doJSON(ctx context.Context, method, path string, input any) (int, []byte, error) {
	var body io.Reader
	if input != nil {
		payload, err := json.Marshal(input)
		if err != nil {
			return 0, nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if s.apiKey != "" {
		request.Header.Set("api-key", s.apiKey)
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxQdrantResponseBytes+1))
	if err != nil {
		return 0, nil, fmt.Errorf("read response: %w", err)
	}
	if len(responseBody) > maxQdrantResponseBytes {
		return 0, nil, fmt.Errorf("response exceeds %d bytes", maxQdrantResponseBytes)
	}
	return response.StatusCode, responseBody, nil
}
