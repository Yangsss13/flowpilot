package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/handler"
)

func NewRouter(
	taskHandler *handler.TaskHandler,
	executionHandler *handler.ExecutionHandler,
	agentHandler *handler.AgentHandler,
	knowledgeHandler *handler.KnowledgeHandler,
	capabilityHandler *handler.CapabilityHandler,
	healthHandler *handler.HealthHandler,
) *gin.Engine {
	router := gin.Default()
	router.GET("/health", healthHandler.Health)
	router.GET("/ready", healthHandler.Ready)

	api := router.Group("/api")
	api.POST("/tasks", taskHandler.Create)
	api.GET("/tasks", taskHandler.List)
	api.GET("/tasks/stats", taskHandler.Stats)
	api.GET("/tasks/:id", taskHandler.GetByID)
	api.POST("/tasks/:id/run", executionHandler.Run)
	api.GET("/tasks/:id/logs", executionHandler.Logs)
	api.GET("/capabilities", capabilityHandler.Get)
	if agentHandler != nil {
		api.POST("/agent/tasks", agentHandler.Create)
		api.POST("/agent/tasks/:id/run", agentHandler.Run)
	}
	if knowledgeHandler != nil {
		api.POST("/knowledge/documents", knowledgeHandler.Import)
		api.POST("/knowledge/search", knowledgeHandler.Search)
	}

	return router
}
