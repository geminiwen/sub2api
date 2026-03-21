package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetBetaHeader_InjectsRequiredDefaultBetas(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader("claude-sonnet-4-5", "claude-code-20250219,interleaved-thinking-2025-05-14")

	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		got,
	)
}

func TestGetBetaHeader_UsesExpectedDefaultsWhenIncomingBetaMissing(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader("claude-sonnet-4-5", "")

	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		got,
	)
}

func TestGetBetaHeader_HaikuUsesFixedOAuthOrder(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader("claude-haiku-4-5-20251001", "")

	require.Equal(
		t,
		"oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05",
		got,
	)
}

func TestGetBetaHeader_HaikuPreservesInterleavedThinkingWhenProvided(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader("claude-haiku-4-5-20251001", "interleaved-thinking-2025-05-14")

	require.Equal(
		t,
		"oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05",
		got,
	)
}

func TestGetBetaHeader_PreservesExtraTokensBeyondDefaults(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader(
		"claude-sonnet-4-5",
		"claude-code-20250219,custom-beta",
	)

	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24,custom-beta",
		got,
	)
}

func TestGetBetaHeader_NonHaikuWithSpecificIncomingBetas(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader(
		"claude-sonnet-4-5",
		"claude-code-20250219,interleaved-thinking-2025-05-14,prompt-caching-scope-2026-01-05,effort-2025-11-24",
	)

	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24",
		got,
	)
}

func TestGetBetaHeader_PlacesOptionalContext1MInCanonicalPosition(t *testing.T) {
	svc := &GatewayService{}

	got := svc.getBetaHeader(
		"claude-sonnet-4-5",
		"context-1m-2025-08-07,claude-code-20250219,custom-beta",
	)

	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,advanced-tool-use-2025-11-20,effort-2025-11-24,custom-beta",
		got,
	)
}

func TestBuildCountTokensRequest_HaikuAddsCountTokenBetas(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("anthropic-beta", "interleaved-thinking-2025-05-14")

	svc := &GatewayService{}
	account := &Account{
		Type: AccountTypeOAuth,
	}

	req, err := svc.buildCountTokensRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-haiku-4-5-20251001",
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "keep-alive", req.Header.Get("Connection"))
	require.Equal(
		t,
		"oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,token-counting-2024-11-01",
		testHeaderValue(req.Header, "anthropic-beta"),
	)
}

func TestBuildCountTokensRequest_HaikuWithoutIncomingBetaDoesNotInjectInterleavedThinking(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)

	svc := &GatewayService{}
	account := &Account{
		Type: AccountTypeOAuth,
	}

	req, err := svc.buildCountTokensRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-haiku-4-5-20251001","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-haiku-4-5-20251001",
		false,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		"oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,token-counting-2024-11-01",
		testHeaderValue(req.Header, "anthropic-beta"),
	)
}

func TestBuildCountTokensRequest_DefaultsForNonHaiku(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)

	svc := &GatewayService{}
	account := &Account{
		Type: AccountTypeOAuth,
	}

	req, err := svc.buildCountTokensRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-5",
		false,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,token-counting-2024-11-01",
		testHeaderValue(req.Header, "anthropic-beta"),
	)
}

func TestBuildCountTokensRequest_PlacesOptionalContext1MInCanonicalPosition(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)
	c.Request.Header.Set("anthropic-beta", "context-1m-2025-08-07,custom-beta")

	svc := &GatewayService{}
	account := &Account{
		Type: AccountTypeOAuth,
	}

	req, err := svc.buildCountTokensRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-5",
		false,
	)
	require.NoError(t, err)
	require.Equal(
		t,
		"claude-code-20250219,oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14,redact-thinking-2026-02-12,context-management-2025-06-27,prompt-caching-scope-2026-01-05,token-counting-2024-11-01,custom-beta",
		testHeaderValue(req.Header, "anthropic-beta"),
	)
}

func TestBuildUpstreamRequest_StreamDoesNotInjectStainlessHelperMethod(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	svc := &GatewayService{}
	account := &Account{
		Type: AccountTypeOAuth,
	}

	req, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-5",
		true,
		false,
	)
	require.NoError(t, err)
	require.Empty(t, testHeaderValue(req.Header, "x-stainless-helper-method"))

	c.Request.Header.Set("X-Stainless-Helper-Method", "stream")
	req, err = svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-5",
		true,
		false,
	)
	require.NoError(t, err)
	require.Equal(t, "stream", testHeaderValue(req.Header, "x-stainless-helper-method"))
}

func TestDefaultBetaPolicySettings_DoesNotFilterContext1M(t *testing.T) {
	settings := DefaultBetaPolicySettings()
	for _, rule := range settings.Rules {
		require.NotEqual(t, claude.BetaContext1M, rule.BetaToken)
	}
}

func TestEvaluateBetaPolicy_AnthropicOAuthBypassesRules(t *testing.T) {
	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken:    claude.BetaFastMode,
				Action:       BetaPolicyActionBlock,
				Scope:        BetaPolicyScopeAll,
				ErrorMessage: "fast mode blocked",
			},
			{
				BetaToken: claude.BetaContext1M,
				Action:    BetaPolicyActionFilter,
				Scope:     BetaPolicyScopeAll,
			},
		},
	}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	policy := svc.evaluateBetaPolicy(
		context.Background(),
		claude.BetaFastMode+","+claude.BetaContext1M,
		account,
	)

	require.Nil(t, policy.blockErr)
	require.Empty(t, policy.filterSet)
}

func TestBuildUpstreamRequest_AnthropicOAuthBypassesBetaPolicyFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	settings := &BetaPolicySettings{
		Rules: []BetaPolicyRule{
			{
				BetaToken: claude.BetaFastMode,
				Action:    BetaPolicyActionFilter,
				Scope:     BetaPolicyScopeAll,
			},
		},
	}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("anthropic-beta", claude.BetaFastMode)

	svc := &GatewayService{
		settingService: NewSettingService(
			&betaPolicySettingRepoStub{values: map[string]string{
				SettingKeyBetaPolicySettings: string(raw),
			}},
			&config.Config{},
		),
	}
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	req, err := svc.buildUpstreamRequest(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		"oauth-token",
		"oauth",
		"claude-sonnet-4-5",
		false,
		false,
	)
	require.NoError(t, err)
	require.Contains(t, testHeaderValue(req.Header, "anthropic-beta"), claude.BetaFastMode)
}

func TestMergeAnthropicBeta(t *testing.T) {
	got := mergeAnthropicBeta(
		[]string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"},
		"foo, oauth-2025-04-20,bar, foo",
	)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,foo,bar", got)
}

func TestMergeAnthropicBeta_EmptyIncoming(t *testing.T) {
	got := mergeAnthropicBeta(
		[]string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"},
		"",
	)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14", got)
}

func TestStripBetaTokens(t *testing.T) {
	tests := []struct {
		name   string
		header string
		tokens []string
		want   string
	}{
		{
			name:   "single token in middle",
			header: "oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "single token at start",
			header: "context-1m-2025-08-07,oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "single token at end",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-1m-2025-08-07",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "token not present",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "empty header",
			header: "",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "",
		},
		{
			name:   "with spaces",
			header: "oauth-2025-04-20, context-1m-2025-08-07 , interleaved-thinking-2025-05-14",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "only token",
			header: "context-1m-2025-08-07",
			tokens: []string{"context-1m-2025-08-07"},
			want:   "",
		},
		{
			name:   "nil tokens",
			header: "oauth-2025-04-20,interleaved-thinking-2025-05-14",
			tokens: nil,
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "multiple tokens removed",
			header: "oauth-2025-04-20,context-1m-2025-08-07,interleaved-thinking-2025-05-14,fast-mode-2026-02-01",
			tokens: []string{"context-1m-2025-08-07", "fast-mode-2026-02-01"},
			want:   "oauth-2025-04-20,interleaved-thinking-2025-05-14",
		},
		{
			name:   "DroppedBetas is empty (filtering moved to configurable beta policy)",
			header: "oauth-2025-04-20,context-1m-2025-08-07,fast-mode-2026-02-01,interleaved-thinking-2025-05-14",
			tokens: claude.DroppedBetas,
			want:   "oauth-2025-04-20,context-1m-2025-08-07,fast-mode-2026-02-01,interleaved-thinking-2025-05-14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBetaTokens(tt.header, tt.tokens)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMergeAnthropicBetaDropping_Context1M(t *testing.T) {
	required := []string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"}
	incoming := "context-1m-2025-08-07,foo-beta,oauth-2025-04-20"
	drop := map[string]struct{}{"context-1m-2025-08-07": {}}

	got := mergeAnthropicBetaDropping(required, incoming, drop)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,foo-beta", got)
	require.NotContains(t, got, "context-1m-2025-08-07")
}

func TestMergeAnthropicBetaDropping_DroppedBetas(t *testing.T) {
	required := []string{"oauth-2025-04-20", "interleaved-thinking-2025-05-14"}
	incoming := "context-1m-2025-08-07,fast-mode-2026-02-01,foo-beta,oauth-2025-04-20"
	// DroppedBetas is now empty — filtering moved to configurable beta policy.
	// Without a policy filter set, nothing gets dropped from the static set.
	drop := droppedBetaSet()

	got := mergeAnthropicBetaDropping(required, incoming, drop)
	require.Equal(t, "oauth-2025-04-20,interleaved-thinking-2025-05-14,context-1m-2025-08-07,fast-mode-2026-02-01,foo-beta", got)
	require.Contains(t, got, "context-1m-2025-08-07")
	require.Contains(t, got, "fast-mode-2026-02-01")
}

func TestDroppedBetaSet(t *testing.T) {
	// Base set contains DroppedBetas (now empty — filtering moved to configurable beta policy)
	base := droppedBetaSet()
	require.Len(t, base, len(claude.DroppedBetas))

	// With extra tokens
	extended := droppedBetaSet(claude.BetaClaudeCode)
	require.Contains(t, extended, claude.BetaClaudeCode)
	require.Len(t, extended, len(claude.DroppedBetas)+1)
}

func TestBuildBetaTokenSet(t *testing.T) {
	got := buildBetaTokenSet([]string{"foo", "", "bar", "foo"})
	require.Len(t, got, 2)
	require.Contains(t, got, "foo")
	require.Contains(t, got, "bar")
	require.NotContains(t, got, "")

	empty := buildBetaTokenSet(nil)
	require.Empty(t, empty)
}

func TestContainsBetaToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		token  string
		want   bool
	}{
		{"present in middle", "oauth-2025-04-20,fast-mode-2026-02-01,interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", true},
		{"present at start", "fast-mode-2026-02-01,oauth-2025-04-20", "fast-mode-2026-02-01", true},
		{"present at end", "oauth-2025-04-20,fast-mode-2026-02-01", "fast-mode-2026-02-01", true},
		{"only token", "fast-mode-2026-02-01", "fast-mode-2026-02-01", true},
		{"not present", "oauth-2025-04-20,interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", false},
		{"with spaces", "oauth-2025-04-20, fast-mode-2026-02-01 , interleaved-thinking-2025-05-14", "fast-mode-2026-02-01", true},
		{"empty header", "", "fast-mode-2026-02-01", false},
		{"empty token", "fast-mode-2026-02-01", "", false},
		{"partial match", "fast-mode-2026-02-01-extra", "fast-mode-2026-02-01", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsBetaToken(tt.header, tt.token)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestStripBetaTokensWithSet_EmptyDropSet(t *testing.T) {
	header := "oauth-2025-04-20,interleaved-thinking-2025-05-14"
	got := stripBetaTokensWithSet(header, map[string]struct{}{})
	require.Equal(t, header, got)
}

func TestIsCountTokensUnsupported404(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "exact endpoint not found",
			statusCode: 404,
			body:       `{"error":{"message":"Not found: /v1/messages/count_tokens","type":"not_found_error"}}`,
			want:       true,
		},
		{
			name:       "contains count_tokens and not found",
			statusCode: 404,
			body:       `{"error":{"message":"count_tokens route not found","type":"not_found_error"}}`,
			want:       true,
		},
		{
			name:       "generic 404",
			statusCode: 404,
			body:       `{"error":{"message":"resource not found","type":"not_found_error"}}`,
			want:       false,
		},
		{
			name:       "404 with empty error message",
			statusCode: 404,
			body:       `{"error":{"message":"","type":"not_found_error"}}`,
			want:       false,
		},
		{
			name:       "non-404 status",
			statusCode: 400,
			body:       `{"error":{"message":"Not found: /v1/messages/count_tokens","type":"invalid_request_error"}}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCountTokensUnsupported404(tt.statusCode, []byte(tt.body))
			require.Equal(t, tt.want, got)
		})
	}
}
