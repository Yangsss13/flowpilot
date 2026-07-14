package httpapi

import (
	"github.com/gin-gonic/gin"

	"minikvx-agent/internal/handler"
)

func NewRouter(taskHandler *handler.TaskHandler, executionHandler *handler.ExecutionHandler) *gin.Engine {
	router := gin.Default()

	api := router.Group("/api")
	api.POST("/tasks", taskHandler.Create)
	api.GET("/tasks", taskHandler.List)
	api.GET("/tasks/:id", taskHandler.GetByID)
	api.POST("/tasks/:id/run", executionHandler.Run)
	api.GET("/tasks/:id/logs", executionHandler.Logs)

	return router
}
