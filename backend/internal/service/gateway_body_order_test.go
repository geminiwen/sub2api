package service

import (
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

func assertJSONTokenOrder(t *testing.T, body string, tokens ...string) {
	t.Helper()

	last := -1
	for _, token := range tokens {
		pos := strings.Index(body, token)
		require.NotEqualf(t, -1, pos, "missing token %s in body %s", token, body)
		require.Greaterf(t, pos, last, "token %s should appear after previous tokens in body %s", token, body)
		last = pos
	}
}

func TestReplaceModelInBody_PreservesTopLevelFieldOrder(t *testing.T) {
	svc := &GatewayService{}
	body := []byte(`{"alpha":1,"model":"claude-3-5-sonnet-latest","messages":[],"omega":2}`)

	result := svc.replaceModelInBody(body, "claude-3-5-sonnet-20241022")
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"model"`, `"messages"`, `"omega"`)
	require.Contains(t, resultStr, `"model":"claude-3-5-sonnet-20241022"`)
}

func TestReplaceModelInBody_PreservesClientKeyCase(t *testing.T) {
	svc := &GatewayService{}
	body := []byte(`{"alpha":1,"Model":"claude-3-5-sonnet-latest","messages":[],"omega":2}`)

	result := svc.replaceModelInBody(body, "claude-3-5-sonnet-20241022")
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"Model"`, `"messages"`, `"omega"`)
	require.Contains(t, resultStr, `"Model":"claude-3-5-sonnet-20241022"`)
	require.NotContains(t, resultStr, `"model":"claude-3-5-sonnet-20241022"`)
}

func TestNormalizeClaudeOAuthRequestBody_PreservesTopLevelFieldOrder(t *testing.T) {
	body := []byte(`{"alpha":1,"model":"claude-3-5-sonnet-latest","temperature":0.2,"system":"You are OpenCode, the best coding agent on the planet.","messages":[],"tool_choice":{"type":"auto"},"omega":2}`)

	result, modelID := normalizeClaudeOAuthRequestBody(body, "claude-3-5-sonnet-latest", claudeOAuthNormalizeOptions{
		injectMetadata: true,
		metadataUserID: "user-1",
	})
	resultStr := string(result)

	require.Equal(t, "claude-3-5-sonnet-latest", modelID)
	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"model"`, `"system"`, `"messages"`, `"omega"`, `"tools"`, `"metadata"`)
	require.NotContains(t, resultStr, `"temperature"`)
	require.NotContains(t, resultStr, `"tool_choice"`)
	require.Contains(t, resultStr, `"model":"claude-3-5-sonnet-latest"`)
	require.Contains(t, resultStr, `"system":"`+claudeCodeSystemPrompt+`"`)
	require.Contains(t, resultStr, `"tools":[]`)
	require.Contains(t, resultStr, `"metadata":{"user_id":"user-1"}`)
}

func TestNormalizeClaudeOAuthRequestBody_PreservesExistingKeyCase(t *testing.T) {
	body := []byte(`{"alpha":1,"Model":"claude-3-5-sonnet-latest","temperature":0.2,"System":"You are OpenCode, the best coding agent on the planet.","Messages":[],"tool_choice":{"type":"auto"},"omega":2}`)

	result, modelID := normalizeClaudeOAuthRequestBody(body, "claude-3-5-sonnet-latest", claudeOAuthNormalizeOptions{
		injectMetadata: true,
		metadataUserID: "user-1",
	})
	resultStr := string(result)

	require.Equal(t, "claude-3-5-sonnet-latest", modelID)
	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"Model"`, `"System"`, `"Messages"`, `"omega"`, `"tools"`, `"metadata"`)
	require.Contains(t, resultStr, `"Model":"claude-3-5-sonnet-latest"`)
	require.Contains(t, resultStr, `"System":"`+claudeCodeSystemPrompt+`"`)
	require.NotContains(t, resultStr, `"model":`)
	require.NotContains(t, resultStr, `"system":`)
}

func TestInjectClaudeCodePrompt_PreservesFieldOrder(t *testing.T) {
	body := []byte(`{"alpha":1,"system":[{"id":"block-1","type":"text","text":"Custom"}],"messages":[],"omega":2}`)

	result := injectClaudeCodePrompt(body, []any{
		map[string]any{"id": "block-1", "type": "text", "text": "Custom"},
	})
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"system"`, `"messages"`, `"omega"`)
	require.Contains(t, resultStr, `{"id":"block-1","type":"text","text":"`+claudeCodeSystemPrompt+`\n\nCustom"}`)
}

func TestUpsertAnthropicBillingHeaderSystemBlock_PreservesTopLevelFieldOrder(t *testing.T) {
	body := []byte(`{"alpha":1,"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=0; cc_entrypoint=cli; cch=00000;"}],"messages":[{"role":"user","content":"hello world"}],"omega":2}`)

	result := upsertAnthropicBillingHeaderSystemBlock(body, claude.DefaultCLIUserAgent, "/v1/messages")
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"system"`, `"messages"`, `"omega"`)
	require.Contains(t, resultStr, anthropicBillingHeaderPrefix)
}

func TestEnforceCacheControlLimit_PreservesTopLevelFieldOrder(t *testing.T) {
	body := []byte(`{"alpha":1,"system":[{"type":"text","text":"s1","cache_control":{"type":"ephemeral"}},{"type":"text","text":"s2","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"m1","cache_control":{"type":"ephemeral"}},{"type":"text","text":"m2","cache_control":{"type":"ephemeral"}},{"type":"text","text":"m3","cache_control":{"type":"ephemeral"}}]}],"omega":2}`)

	result := enforceCacheControlLimit(body)
	resultStr := string(result)

	assertJSONTokenOrder(t, resultStr, `"alpha"`, `"system"`, `"messages"`, `"omega"`)
	require.Equal(t, 4, strings.Count(resultStr, `"cache_control"`))
}

func TestMoveAnthropicCacheControlAfterText_ReordersNestedBlocks(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","cache_control":{"type":"ephemeral"},"text":"s1"}],"messages":[{"role":"user","content":[{"type":"text","cache_control":{"type":"ephemeral"},"text":"m1"},{"type":"tool_use","id":"tool_1","input":{}}]}]}`)

	result := moveAnthropicCacheControlAfterText(body)
	resultStr := string(result)

	require.Contains(t, resultStr, `{"type":"text","text":"s1","cache_control":{"type":"ephemeral"}}`)
	require.Contains(t, resultStr, `{"type":"text","text":"m1","cache_control":{"type":"ephemeral"}}`)
	require.Contains(t, resultStr, `{"type":"tool_use","id":"tool_1","input":{}}`)
}
