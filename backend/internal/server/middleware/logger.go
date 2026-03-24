package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	ContextKeyClientMessagesSessionID      = "client_messages_session_id"
	ContextKeyClientEventLoggingSessionIDs = "client_event_logging_session_ids"
)

// Logger 请求日志中间件
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 开始时间
		startTime := time.Now()

		// 请求路径
		path := c.Request.URL.Path

		// 处理请求
		c.Next()

		// 跳过健康检查等高频探针路径的日志
		if path == "/health" || path == "/setup/status" {
			return
		}

		endTime := time.Now()
		latency := endTime.Sub(startTime)

		method := c.Request.Method
		statusCode := c.Writer.Status()
		clientIP := c.ClientIP()
		protocol := c.Request.Proto
		accountID, hasAccountID := c.Request.Context().Value(ctxkey.AccountID).(int64)
		platform, _ := c.Request.Context().Value(ctxkey.Platform).(string)
		model, _ := c.Request.Context().Value(ctxkey.Model).(string)

		fields := []zap.Field{
			zap.String("component", "http.access"),
			zap.Int("status_code", statusCode),
			zap.Int64("latency_ms", latency.Milliseconds()),
			zap.String("client_ip", clientIP),
			zap.String("protocol", protocol),
			zap.String("method", method),
			zap.String("path", path),
		}
		if hasAccountID && accountID > 0 {
			fields = append(fields, zap.Int64("account_id", accountID))
		}
		if platform != "" {
			fields = append(fields, zap.String("platform", platform))
		}
		if model != "" {
			fields = append(fields, zap.String("model", model))
		}
		if errorCode, ok := c.Get(string(ContextKeyErrorCode)); ok {
			if s, ok := errorCode.(string); ok && strings.TrimSpace(s) != "" {
				fields = append(fields, zap.String("error_code", s))
			}
		}
		if errorMessage, ok := c.Get(string(ContextKeyErrorMessage)); ok {
			if s, ok := errorMessage.(string); ok && strings.TrimSpace(s) != "" {
				fields = append(fields, zap.String("error_message", s))
			}
		}
		fields = append(fields, requestHeaderLogFields(path, c.Request)...)
		fields = append(fields, requestSessionLogFields(c, path)...)

		l := logger.FromContext(c.Request.Context()).With(fields...)
		l.Info("http request completed", zap.Time("completed_at", endTime))

		if statusCode >= 500 {
			errFields := []zap.Field{
				zap.Time("completed_at", endTime),
			}
			if len(c.Errors) > 0 {
				errFields = append(errFields, zap.String("errors", c.Errors.String()))
			}
			l.Error("http request failed", errFields...)
		}

		if len(c.Errors) > 0 {
			l.Warn("http request contains gin errors", zap.String("errors", c.Errors.String()))
		}
	}
}

func requestHeaderLogFields(path string, req *http.Request) []zap.Field {
	if req == nil || !shouldLogHeaderSummary(path) {
		return nil
	}

	authHeader := strings.TrimSpace(req.Header.Get("Authorization"))
	authScheme := ""
	authToken := ""
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		authScheme = strings.TrimSpace(parts[0])
		if len(parts) == 2 {
			authToken = strings.TrimSpace(parts[1])
		}
	}

	xAPIKey := strings.TrimSpace(req.Header.Get("x-api-key"))
	xGoogAPIKey := strings.TrimSpace(req.Header.Get("x-goog-api-key"))

	return []zap.Field{
		zap.String("hdr_content_type", strings.TrimSpace(req.Header.Get("Content-Type"))),
		zap.String("hdr_user_agent", strings.TrimSpace(req.Header.Get("User-Agent"))),
		zap.Bool("hdr_authorization_present", authHeader != ""),
		zap.String("hdr_authorization_scheme", authScheme),
		zap.String("hdr_authorization_token_fp", headerTokenFingerprint(authToken)),
		zap.Bool("hdr_x_api_key_present", xAPIKey != ""),
		zap.String("hdr_x_api_key_fp", headerTokenFingerprint(xAPIKey)),
		zap.Bool("hdr_x_goog_api_key_present", xGoogAPIKey != ""),
		zap.String("hdr_x_goog_api_key_fp", headerTokenFingerprint(xGoogAPIKey)),
	}
}

func shouldLogHeaderSummary(path string) bool {
	return path == "/v1/messages" || path == "/api/event_logging/v2/batch"
}

func requestSessionLogFields(c *gin.Context, path string) []zap.Field {
	if c == nil {
		return nil
	}

	switch path {
	case "/v1/messages":
		if value, ok := c.Get(ContextKeyClientMessagesSessionID); ok {
			if sessionID, ok := value.(string); ok && strings.TrimSpace(sessionID) != "" {
				return []zap.Field{zap.String("client_session_id", strings.TrimSpace(sessionID))}
			}
		}
	case "/api/event_logging/v2/batch":
		if value, ok := c.Get(ContextKeyClientEventLoggingSessionIDs); ok {
			if sessionIDs, ok := value.([]string); ok && len(sessionIDs) > 0 {
				return []zap.Field{
					zap.Strings("client_session_ids", sessionIDs),
					zap.Int("client_session_id_count", len(sessionIDs)),
				}
			}
		}
	}

	return nil
}

func headerTokenFingerprint(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	fp := hex.EncodeToString(sum[:6])
	return fp + ":" + strconv.Itoa(len(token))
}
