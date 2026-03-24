package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type codeHubAccessSettingRepoStub struct {
	values map[string]string
}

func (s *codeHubAccessSettingRepoStub) Get(context.Context, string) (*service.Setting, error) {
	panic("unexpected Get call")
}

func (s *codeHubAccessSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", service.ErrSettingNotFound
}

func (s *codeHubAccessSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected Set call")
}

func (s *codeHubAccessSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *codeHubAccessSettingRepoStub) SetMultiple(_ context.Context, settings map[string]string) error {
	if s.values == nil {
		s.values = make(map[string]string)
	}
	for k, v := range settings {
		s.values[k] = v
	}
	return nil
}

func (s *codeHubAccessSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *codeHubAccessSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected Delete call")
}

func newCodeHubRestrictedSettingService(enabled bool) *service.SettingService {
	repo := &codeHubAccessSettingRepoStub{
		values: map[string]string{},
	}
	svc := service.NewSettingService(repo, &config.Config{})
	_ = svc.UpdateSettings(context.Background(), &service.SystemSettings{
		RestrictCodeHubClientAccess: enabled,
	})
	return svc
}

func TestValidateCodeHubClientUserAgent_AllowsWhenRestrictionDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli)")

	called := false
	ok := validateCodeHubClientUserAgent(c, newCodeHubRestrictedSettingService(false), func(*gin.Context, int, string, string) {
		called = true
	})
	require.True(t, ok)
	require.False(t, called)
}

func TestValidateCodeHubClientUserAgent_AllowsCodeHubMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli, codehub)")

	called := false
	ok := validateCodeHubClientUserAgent(c, newCodeHubRestrictedSettingService(true), func(*gin.Context, int, string, string) {
		called = true
	})
	require.True(t, ok)
	require.False(t, called)
}

func TestValidateCodeHubClientUserAgent_RejectsMissingCodeHubMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("User-Agent", "claude-cli/2.1.81 (external, cli)")

	var (
		gotStatus  int
		gotType    string
		gotMessage string
	)
	ok := validateCodeHubClientUserAgent(c, newCodeHubRestrictedSettingService(true), func(_ *gin.Context, status int, errType, message string) {
		gotStatus = status
		gotType = errType
		gotMessage = message
	})
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, gotStatus)
	require.Equal(t, "invalid_request_error", gotType)
	require.Equal(t, "User-Agent must include codehub source marker", gotMessage)
}
