package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/service"
)

type ExecutionApplication interface {
	Submit(ctx context.Context, taskID uint64) error
	Logs(ctx context.Context, taskID uint64) ([]domain.ExecutionLog, error)
}

type ExecutionHandler struct {
	service ExecutionApplication
}

func NewExecutionHandler(service ExecutionApplication) *ExecutionHandler {
	return &ExecutionHandler{service: service}
}

func (h *ExecutionHandler) Run(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	err := h.service.Submit(c.Request.Context(), taskID)
	if errors.Is(err, service.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if errors.Is(err, service.ErrTaskConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "task cannot be executed in its current state"})
		return
	}
	if errors.Is(err, service.ErrQueueUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "task queue is unavailable"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"task_id": taskID, "status": "accepted"})
}

func (h *ExecutionHandler) Logs(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}

	logs, err := h.service.Logs(c.Request.Context(), taskID)
	if errors.Is(err, service.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, logs)
}

func parseTaskID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task id must be a positive integer"})
		return 0, false
	}
	return id, true
}
