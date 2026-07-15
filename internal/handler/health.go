package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type ReadinessCheck func(ctx context.Context) error

type HealthHandler struct {
	checks map[string]ReadinessCheck
}

func NewHealthHandler(checks map[string]ReadinessCheck) *HealthHandler {
	return &HealthHandler{checks: checks}
}

func (h *HealthHandler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *HealthHandler) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	statuses := make(map[string]string, len(h.checks))
	ready := true
	for name, check := range h.checks {
		if err := check(ctx); err != nil {
			statuses[name] = "unavailable"
			ready = false
			continue
		}
		statuses[name] = "ok"
	}
	status := http.StatusOK
	state := "ready"
	if !ready {
		status = http.StatusServiceUnavailable
		state = "not_ready"
	}
	c.JSON(status, gin.H{"status": state, "checks": statuses})
}
