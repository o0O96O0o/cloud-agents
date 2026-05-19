package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/l-lab/cloud-agents/internal/db"
)

func corsMiddleware(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// tokenLookup is the minimal interface the schedule token middleware requires.
type tokenLookup interface {
	LookupScheduleByToken(ctx context.Context, rawToken string) (*db.ScheduledTask, error)
}

const scheduleTokenCtxKey = "scheduleToken"

// scheduleTokenAuthMiddleware authenticates the /public/schedules/:id/fire endpoint.
// It verifies the Bearer token, confirms it matches the schedule ID in the path,
// and stores the matched *db.ScheduledTask in the Gin context.
func scheduleTokenAuthMiddleware(store tokenLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "missing bearer token"})
			return
		}
		rawToken := strings.TrimPrefix(authHeader, "Bearer ")

		sched, err := store.LookupScheduleByToken(c.Request.Context(), rawToken)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "invalid or revoked token"})
			return
		}

		// Verify the token is scoped to the schedule ID in the path.
		if sched.ID != c.Param("id") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "token does not match schedule"})
			return
		}

		c.Set(scheduleTokenCtxKey, sched)
		c.Next()
	}
}
