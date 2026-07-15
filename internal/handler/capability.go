package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/agent"
)

type CapabilityTool struct {
	Name        agent.ToolName `json:"name"`
	Description string         `json:"description"`
}

type CapabilityResponse struct {
	AgentEnabled     bool             `json:"agent_enabled"`
	Tools            []CapabilityTool `json:"tools"`
	KnowledgeEnabled bool             `json:"knowledge_enabled"`
}

type CapabilityHandler struct {
	response CapabilityResponse
}

func NewCapabilityHandler(agentEnabled bool, definitions []agent.ToolDefinition, knowledgeEnabled bool) *CapabilityHandler {
	tools := make([]CapabilityTool, len(definitions))
	for index, definition := range definitions {
		tools[index] = CapabilityTool{Name: definition.Name, Description: definition.Description}
	}
	return &CapabilityHandler{response: CapabilityResponse{
		AgentEnabled: agentEnabled, Tools: tools, KnowledgeEnabled: knowledgeEnabled,
	}}
}

func (h *CapabilityHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, h.response)
}
