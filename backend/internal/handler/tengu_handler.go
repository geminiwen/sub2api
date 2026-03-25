package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TenguHandler handles Tengu telemetry proxy requests.
type TenguHandler struct {
	tenguService *service.TenguProxyService
}

// NewTenguHandler creates a new TenguHandler.
func NewTenguHandler(tenguService *service.TenguProxyService) *TenguHandler {
	return &TenguHandler{
		tenguService: tenguService,
	}
}

// BatchEvents handles POST /api/event_logging/batch.
// For each event in the batch, it uses event_data.session_id to look up the
// sticky-session-bound account, enriches identity fields, and forwards to
// api.anthropic.com. Events without a resolvable session_id are dropped.
func (h *TenguHandler) BatchEvents(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Failed to read request body",
			},
		})
		return
	}

	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": "Empty request body",
			},
		})
		return
	}

	if sessionIDs := extractTelemetryClientSessionIDs(body); len(sessionIDs) > 0 {
		c.Set(middleware.ContextKeyClientEventLoggingSessionIDs, sessionIDs)
	}

	result, err := h.tenguService.ProcessAndForward(
		c.Request.Context(),
		body,
		nil,
		c.Request.Header,
	)
	if err != nil {
		logger.L().Error("tengu: process and forward failed",
			zap.Error(err),
		)
		c.JSON(http.StatusBadGateway, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "api_error",
				"message": "Failed to process telemetry batch",
			},
		})
		return
	}

	service.LogTenguRequest(result)

	// Return same format as Anthropic upstream.
	// Absorb dropped events into accepted_count so the client sees all events as accepted.
	c.JSON(http.StatusOK, gin.H{
		"accepted_count": result.AcceptedCount + result.DroppedEvents,
		"rejected_count": result.RejectedCount,
	})
}

func extractTelemetryClientSessionIDs(body []byte) []string {
	var batch struct {
		Events []struct {
			EventData map[string]any `json:"event_data"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil
	}

	seen := make(map[string]struct{}, len(batch.Events))
	sessionIDs := make([]string, 0, len(batch.Events))
	for _, event := range batch.Events {
		sessionID, _ := event.EventData["session_id"].(string)
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			continue
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}
	sort.Strings(sessionIDs)
	return sessionIDs
}
