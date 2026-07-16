package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/knowledge"
	"github.com/Yangsss13/flowpilot/internal/rag"
)

type KnowledgeApplication interface {
	Upload(ctx context.Context, input knowledge.UploadInput) (knowledge.UploadResult, error)
	UploadVersion(ctx context.Context, documentID uint64, input knowledge.UploadInput) (knowledge.UploadResult, error)
	List(ctx context.Context, filter knowledge.DocumentFilter) (knowledge.DocumentList, error)
	Get(ctx context.Context, id uint64) (knowledge.DocumentDetail, error)
	Delete(ctx context.Context, id uint64) error
	GetJob(ctx context.Context, id uint64) (domain.IngestionJob, error)
	Retry(ctx context.Context, id uint64) (domain.IngestionJob, error)
	Reindex(ctx context.Context, documentID uint64) (domain.IngestionJob, error)
	Cancel(ctx context.Context, id uint64) (domain.IngestionJob, error)
	Search(ctx context.Context, request knowledge.SearchRequest) ([]rag.SearchResult, error)
}

type KnowledgeHandler struct {
	service        KnowledgeApplication
	maxUploadBytes int64
}

func NewKnowledgeHandler(service KnowledgeApplication, maxUploadBytes int64) *KnowledgeHandler {
	return &KnowledgeHandler{service: service, maxUploadBytes: maxUploadBytes}
}

func (h *KnowledgeHandler) Import(c *gin.Context) {
	h.upload(c, 0)
}

func (h *KnowledgeHandler) UploadVersion(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	h.upload(c, id)
}

func (h *KnowledgeHandler) upload(c *gin.Context, documentID uint64) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUploadBytes+(1<<20))
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a supported document file is required"})
		return
	}
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot open uploaded file"})
		return
	}
	defer file.Close()
	input := knowledge.UploadInput{
		Filename: fileHeader.Filename, DeclaredType: fileHeader.Header.Get("Content-Type"), Source: file,
	}
	var result knowledge.UploadResult
	if documentID == 0 {
		result, err = h.service.Upload(c.Request.Context(), input)
	} else {
		result, err = h.service.UploadVersion(c.Request.Context(), documentID, input)
	}
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusAccepted, result)
}

func (h *KnowledgeHandler) List(c *gin.Context) {
	page, err := optionalKnowledgePositiveInt(c.Query("page"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page"})
		return
	}
	pageSize, err := optionalKnowledgePositiveInt(c.Query("page_size"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page_size"})
		return
	}
	result, err := h.service.List(c.Request.Context(), knowledge.DocumentFilter{
		Page: page, PageSize: pageSize, Status: domain.DocumentStatus(c.Query("status")),
		Extension: c.Query("format"), Query: c.Query("query"),
	})
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": result.Items, "total": result.Total, "page": result.Page, "page_size": result.PageSize})
}

func (h *KnowledgeHandler) Get(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	result, err := h.service.Get(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *KnowledgeHandler) Delete(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	if h.writeError(c, h.service.Delete(c.Request.Context(), id)) {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"document_id": id, "status": domain.DocumentStatusDeleting})
}

func (h *KnowledgeHandler) GetJob(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	job, err := h.service.GetJob(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, job)
}

func (h *KnowledgeHandler) Retry(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	job, err := h.service.Retry(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *KnowledgeHandler) Reindex(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	job, err := h.service.Reindex(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *KnowledgeHandler) Cancel(c *gin.Context) {
	id, ok := parseKnowledgeID(c, "id")
	if !ok {
		return
	}
	job, err := h.service.Cancel(c.Request.Context(), id)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusAccepted, job)
}

func (h *KnowledgeHandler) Search(c *gin.Context) {
	var request knowledge.SearchRequest
	decoder := json.NewDecoder(io.LimitReader(c.Request.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(&struct{}{}) != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}
	results, err := h.service.Search(c.Request.Context(), request)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (h *KnowledgeHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, knowledge.ErrInvalidInput), errors.Is(err, rag.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": safeInputMessage(err)})
	case errors.Is(err, knowledge.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "knowledge resource not found"})
	case errors.Is(err, knowledge.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "knowledge resource is not in the required state"})
	case errors.Is(err, rag.ErrEmbedding):
		c.JSON(http.StatusBadGateway, gin.H{"error": "embedding provider is unavailable"})
	case errors.Is(err, knowledge.ErrUnavailable), errors.Is(err, rag.ErrVectorStore):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "knowledge service is unavailable"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
	return true
}

func safeInputMessage(err error) string {
	message := err.Error()
	if index := strings.Index(message, ": "); index >= 0 {
		message = message[index+2:]
	}
	if len(message) > 200 {
		return "invalid knowledge input"
	}
	return message
}

func parseKnowledgeID(c *gin.Context, name string) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func optionalKnowledgePositiveInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("value must be positive")
	}
	return parsed, nil
}
