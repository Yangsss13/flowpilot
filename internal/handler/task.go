package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type TaskApplication interface {
	Create(ctx context.Context, input service.CreateTaskInput) (*domain.Task, error)
	List(ctx context.Context) ([]domain.Task, error)
	GetByID(ctx context.Context, id uint64) (*domain.Task, error)
}

type TaskHandler struct {
	service TaskApplication
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

func NewTaskHandler(service TaskApplication) *TaskHandler {
	return &TaskHandler{service: service}
}

func (h *TaskHandler) List(c *gin.Context) {
	tasks, err := h.service.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, tasks)
}

func (h *TaskHandler) GetByID(c *gin.Context) {
	id, ok := parseTaskID(c)
	if !ok {
		return
	}

	task, err := h.service.GetByID(c.Request.Context(), id)
	if errors.Is(err, service.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, task)
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
