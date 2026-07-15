package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/domain"
	"github.com/Yangsss13/flowpilot/internal/service"
)

type AgentApplication interface {
	Create(ctx context.Context, input service.CreateAgentTaskInput) (*domain.Task, error)
}

type AgentExecutionApplication interface {
	Submit(ctx context.Context, taskID uint64) error
}

type AgentHandler struct {
	service   AgentApplication
	execution AgentExecutionApplication
}

type createAgentTaskRequest struct {
	Name string `json:"name"`
	Goal string `json:"goal"`
}

func NewAgentHandler(service AgentApplication, execution AgentExecutionApplication) *AgentHandler {
	return &AgentHandler{service: service, execution: execution}
}

func (h *AgentHandler) Run(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	err := h.execution.Submit(c.Request.Context(), taskID)
	if errors.Is(err, service.ErrTaskNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	if errors.Is(err, service.ErrTaskConflict) {
		c.JSON(http.StatusConflict, gin.H{"error": "agent task cannot be executed in its current state"})
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

func (h *AgentHandler) Create(c *gin.Context) {
	var request createAgentTaskRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON request"})
		return
	}
	task, err := h.service.Create(c.Request.Context(), service.CreateAgentTaskInput{
		Name: request.Name,
		Goal: request.Goal,
	})
	if errors.Is(err, service.ErrInvalidInput) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, service.ErrAgentPlanGeneration) {
		c.JSON(http.StatusBadGateway, gin.H{"error": "AI provider failed to generate a valid plan"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	c.JSON(http.StatusCreated, task)
}
