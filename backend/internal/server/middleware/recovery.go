package middleware

import (
	"errors"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Recovery converts panics into the project's standard JSON error envelope.
//
// It preserves Gin's broken-pipe handling by not attempting to write a response
// when the client connection is already gone.
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(gin.DefaultErrorWriter, func(c *gin.Context, recovered any) {
		recoveredErr, _ := recovered.(error)

		if isBrokenPipe(recoveredErr) {
			logRecoveredPanic(c, recovered, true)
			if recoveredErr != nil {
				_ = c.Error(recoveredErr)
			}
			c.Abort()
			return
		}

		logRecoveredPanic(c, recovered, false)

		if c.Writer.Written() {
			c.Abort()
			return
		}

		response.ErrorWithDetails(
			c,
			http.StatusInternalServerError,
			infraerrors.UnknownMessage,
			infraerrors.UnknownReason,
			nil,
		)
		c.Abort()
	})
}

func logRecoveredPanic(c *gin.Context, recovered any, brokenPipe bool) {
	fields := []zap.Field{
		zap.String("component", "http.recovery"),
		zap.Any("panic", recovered),
		zap.Bool("broken_pipe", brokenPipe),
		zap.ByteString("stack", debug.Stack()),
	}
	if c != nil {
		if c.Request != nil {
			fields = append(fields,
				zap.String("method", c.Request.Method),
				zap.String("path", c.Request.URL.Path),
			)
		}
		fields = append(fields, zap.Int("status_code", http.StatusInternalServerError))
	}

	l := logger.L()
	if c != nil && c.Request != nil {
		l = logger.FromContext(c.Request.Context())
	}
	l.With(fields...).Error("panic recovered")
}

func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}

	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}

	var syscallErr *os.SyscallError
	if !errors.As(opErr.Err, &syscallErr) {
		return false
	}

	msg := strings.ToLower(syscallErr.Error())
	return strings.Contains(msg, "broken pipe") || strings.Contains(msg, "connection reset by peer")
}
