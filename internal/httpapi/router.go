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
	api.DELETE("/tasks/:id", taskHandler.Delete)
	api.POST("/tasks/:id/run", executionHandler.Run)
	api.GET("/tasks/:id/logs", executionHandler.Logs)
	api.GET("/capabilities", capabilityHandler.Get)
	if agentHandler != nil {
		api.POST("/agent/tasks", agentHandler.Create)
		api.POST("/agent/tasks/:id/run", agentHandler.Run)
	}
	if knowledgeHandler != nil {
		api.POST("/knowledge/documents", knowledgeHandler.Import)
		api.POST("/knowledge/documents/:id/versions", knowledgeHandler.UploadVersion)
		api.POST("/knowledge/documents/:id/reindex", knowledgeHandler.Reindex)
		api.GET("/knowledge/documents", knowledgeHandler.List)
		api.GET("/knowledge/documents/:id", knowledgeHandler.Get)
		api.DELETE("/knowledge/documents/:id", knowledgeHandler.Delete)
		api.GET("/knowledge/jobs/:id", knowledgeHandler.GetJob)
		api.POST("/knowledge/jobs/:id/retry", knowledgeHandler.Retry)
		api.POST("/knowledge/jobs/:id/cancel", knowledgeHandler.Cancel)
		api.POST("/knowledge/search", knowledgeHandler.Search)
	}

	return router
}
