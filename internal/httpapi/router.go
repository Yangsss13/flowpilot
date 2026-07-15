package httpapi

import (
	"github.com/gin-gonic/gin"

	"github.com/Yangsss13/flowpilot/internal/handler"
)

func NewRouter(taskHandler *handler.TaskHandler, executionHandler *handler.ExecutionHandler, agentHandler *handler.AgentHandler, knowledgeHandler *handler.KnowledgeHandler) *gin.Engine {
	router := gin.Default()

	api := router.Group("/api")
	api.POST("/tasks", taskHandler.Create)
	api.GET("/tasks", taskHandler.List)
	api.GET("/tasks/:id", taskHandler.GetByID)
	api.POST("/tasks/:id/run", executionHandler.Run)
	api.GET("/tasks/:id/logs", executionHandler.Logs)
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
