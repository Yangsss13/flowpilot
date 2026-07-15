package handler

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/rag"
)

type KnowledgeApplication interface {
	Import(ctx context.Context, filename string, content []byte) (rag.ImportResult, error)
	Search(ctx context.Context, query string, topK int) ([]rag.SearchResult, error)
}

type KnowledgeHandler struct {
	service KnowledgeApplication
}

type searchKnowledgeRequest struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k"`
}

func NewKnowledgeHandler(service KnowledgeApplication) *KnowledgeHandler {
	return &KnowledgeHandler{service: service}
}

func (h *KnowledgeHandler) Import(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, rag.MaxDocumentBytes+(64<<10))
	fileHeader, err := c.FormFile("file")
	if err != nil || fileHeader.Size > rag.MaxDocumentBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a .txt or .md file up to 1 MiB is required"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot open uploaded file"})
		return
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, rag.MaxDocumentBytes+1))
	if err != nil || len(content) > rag.MaxDocumentBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cannot read uploaded file"})
		return
	}
	result, err := h.service.Import(c.Request.Context(), fileHeader.Filename, content)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusCreated, result)
}

func (h *KnowledgeHandler) Search(c *gin.Context) {
	var request searchKnowledgeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}
	results, err := h.service.Search(c.Request.Context(), request.Query, request.TopK)
	if h.writeError(c, err) {
		return
	}
	c.JSON(http.StatusOK, gin.H{"results": results})
}

func (h *KnowledgeHandler) writeError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, rag.ErrInvalidInput) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return true
	}
	if errors.Is(err, rag.ErrEmbedding) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "embedding provider is unavailable"})
		return true
	}
	if errors.Is(err, rag.ErrVectorStore) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "vector store is unavailable"})
		return true
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	return true
}
