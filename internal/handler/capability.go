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
	AgentEnabled     bool                 `json:"agent_enabled"`
	Tools            []CapabilityTool     `json:"tools"`
	KnowledgeEnabled bool                 `json:"knowledge_enabled"`
	Knowledge        *KnowledgeCapability `json:"knowledge,omitempty"`
}

type KnowledgeCapability struct {
	AsyncIngestion          bool             `json:"async_ingestion"`
	MediaIngestion          bool             `json:"media_ingestion"`
	SupportedFormats        []string         `json:"supported_formats"`
	MaxBytesByFormat        map[string]int64 `json:"max_bytes_by_format"`
	MaxMediaDurationSeconds int64            `json:"max_media_duration_seconds,omitempty"`
}

type CapabilityHandler struct {
	response CapabilityResponse
}

func NewCapabilityHandler(agentEnabled bool, definitions []agent.ToolDefinition, knowledgeEnabled bool, knowledge ...KnowledgeCapability) *CapabilityHandler {
	tools := make([]CapabilityTool, len(definitions))
	for index, definition := range definitions {
		tools[index] = CapabilityTool{Name: definition.Name, Description: definition.Description}
	}
	response := CapabilityResponse{
		AgentEnabled: agentEnabled, Tools: tools, KnowledgeEnabled: knowledgeEnabled,
	}
	if knowledgeEnabled && len(knowledge) > 0 {
		value := knowledge[0]
		response.Knowledge = &value
	}
	return &CapabilityHandler{response: response}
}

func (h *CapabilityHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, h.response)
}
