package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"minikvx-agent/internal/domain"
	"minikvx-agent/internal/service"
)

type TaskCreator interface {
	Create(ctx context.Context, input service.CreateTaskInput) (*domain.Task, error)
}

type TaskHandler struct {
	service TaskCreator
}

type createTaskRequest struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Steps       []createTaskStepRequest `json:"steps"`
}

type createTaskStepRequest struct {
	Name          string          `json:"name"`
	ActionType    string          `json:"action_type"`
	ActionPayload json.RawMessage `json:"action_payload"`
}

func NewTaskHandler(service TaskCreator) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) Create(c *gin.Context) {
	var request createTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}

	input := service.CreateTaskInput{
		Name:        request.Name,
		Description: request.Description,
		Steps:       make([]service.CreateTaskStepInput, 0, len(request.Steps)),
	}
	for _, step := range request.Steps {
		input.Steps = append(input.Steps, service.CreateTaskStepInput{
			Name:          step.Name,
			ActionType:    step.ActionType,
			ActionPayload: step.ActionPayload,
		})
	}

	task, err := h.service.Create(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, service.ErrInvalidInput) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, task)
}
