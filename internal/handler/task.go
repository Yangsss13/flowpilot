package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type TaskApplication interface {
	Create(ctx context.Context, input service.CreateTaskInput) (*domain.Task, error)
	List(ctx context.Context, input service.ListTasksInput) (service.TaskListResult, error)
	Stats(ctx context.Context) (service.TaskStatsResult, error)
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
	page, err := optionalPositiveInt(c.Query("page"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page must be a positive integer"})
		return
	}
	pageSize, err := optionalPositiveInt(c.Query("page_size"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "page_size must be a positive integer"})
		return
	}
	result, err := h.service.List(c.Request.Context(), service.ListTasksInput{
		Page: page, PageSize: pageSize, TaskType: c.Query("task_type"),
		Status: c.Query("status"), Query: c.Query("query"),
	})
	if errors.Is(err, service.ErrInvalidInput) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *TaskHandler) Stats(c *gin.Context) {
	stats, err := h.service.Stats(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusOK, stats)
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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxTaskRequestBytes)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
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

func optionalPositiveInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, errors.New("not a positive integer")
	}
	return parsed, nil
}
