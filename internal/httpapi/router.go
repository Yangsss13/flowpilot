package httpapi

import (
	"github.com/gin-gonic/gin"

	"minikvx-agent/internal/handler"
)

func NewRouter(taskHandler *handler.TaskHandler) *gin.Engine {
	router := gin.Default()

	api := router.Group("/api")
	api.POST("/tasks", taskHandler.Create)

	return router
}
