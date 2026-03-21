//go:build unit

package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/stretchr/testify/require"
)

func TestCreateTestPayload_ClaudeCodeShape(t *testing.T) {
	t.Parallel()

	payload, err := createTestPayload("claude-sonnet-4-6")
	require.NoError(t, err)

	var parsed struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
		Metadata struct {
			UserID string `json:"user_id"`
		} `json:"metadata"`
		Stream bool `json:"stream"`
	}

	require.NoError(t, json.Unmarshal(payload, &parsed))
	require.Equal(t, "claude-sonnet-4-6", parsed.Model)
	require.True(t, parsed.Stream)
	require.Len(t, parsed.Messages, 1)
	require.Equal(t, "user", parsed.Messages[0].Role)
	require.Len(t, parsed.Messages[0].Content, 1)
	require.Equal(t, "text", parsed.Messages[0].Content[0].Type)
	require.Equal(t, "hi", parsed.Messages[0].Content[0].Text)
	require.NotEmpty(t, parsed.Metadata.UserID)
	require.Len(t, parsed.System, 2)
	require.Equal(t, claudeCodeSystemPrompt, parsed.System[0].Text)
	require.Equal(t, "text", parsed.System[1].Type)
	require.True(t, strings.HasPrefix(parsed.System[1].Text, anthropicBillingHeaderPrefix))
	require.Contains(t, parsed.System[1].Text, "cc_entrypoint=sdk-cli;")
	require.NotContains(t, parsed.System[1].Text, "cch=00000;")
}

func TestTestClaudeBetaHeader_ByModel(t *testing.T) {
	t.Parallel()

	require.Equal(t, claude.HaikuBetaHeader, testClaudeBetaHeader("claude-haiku-4-5-20251001", true))
	require.Equal(t, claude.DefaultBetaHeader, testClaudeBetaHeader("claude-sonnet-4-6", true))
	require.Equal(t, claude.DefaultBetaHeaderWithContext1M, testClaudeBetaHeader("claude-opus-4-6", true))

	require.Equal(t, claude.APIKeyHaikuBetaHeader, testClaudeBetaHeader("claude-haiku-4-5-20251001", false))
	require.Equal(t, claude.APIKeyBetaHeader, testClaudeBetaHeader("claude-sonnet-4-6", false))
	require.Equal(
		t,
		mergeAnthropicBeta(strings.Split(claude.APIKeyBetaHeader, ","), claude.BetaContext1M),
		testClaudeBetaHeader("claude-opus-4-6", false),
	)
}
