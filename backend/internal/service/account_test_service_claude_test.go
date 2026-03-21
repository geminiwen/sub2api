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

	payload, err := createTestPayload("claude-sonnet-4-6", "device-id-123", "acc-uuid-123")
	require.NoError(t, err)

	var parsed struct {
		Model    string `json:"model"`
		Tools    []any   `json:"tools"`
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
		Thinking struct {
			Type         string `json:"type"`
			BudgetTokens int    `json:"budget_tokens"`
		} `json:"thinking"`
		OutputConfig struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
		MaxTokens int  `json:"max_tokens"`
		Stream    bool `json:"stream"`
	}

	require.NoError(t, json.Unmarshal(payload, &parsed))
	require.Equal(t, "claude-sonnet-4-6", parsed.Model)
	require.True(t, parsed.Stream)
	require.Equal(t, 32000, parsed.MaxTokens)
	require.Empty(t, parsed.Tools)
	require.Equal(t, "adaptive", parsed.Thinking.Type)
	require.Equal(t, "medium", parsed.OutputConfig.Effort)
	require.Len(t, parsed.Messages, 1)
	require.Equal(t, "user", parsed.Messages[0].Role)
	require.Len(t, parsed.Messages[0].Content, 2)
	require.Equal(t, "text", parsed.Messages[0].Content[0].Type)
	require.Equal(t, buildTestSystemReminder(""), parsed.Messages[0].Content[0].Text)
	require.Equal(t, "text", parsed.Messages[0].Content[1].Type)
	require.Equal(t, "hi", parsed.Messages[0].Content[1].Text)
	require.NotEmpty(t, parsed.Metadata.UserID)
	userID := ParseMetadataUserID(parsed.Metadata.UserID)
	require.NotNil(t, userID)
	require.Equal(t, "device-id-123", userID.DeviceID)
	require.Equal(t, "acc-uuid-123", userID.AccountUUID)
	require.Len(t, parsed.System, 2)
	require.Equal(t, "text", parsed.System[0].Type)
	require.True(t, strings.HasPrefix(parsed.System[0].Text, anthropicBillingHeaderPrefix))
	require.Contains(t, parsed.System[0].Text, "cc_entrypoint=sdk-cli;")
	require.NotContains(t, parsed.System[0].Text, "cch=00000;")
	require.Equal(t, testClaudeSystemPrompt, parsed.System[1].Text)
}

func TestCreateTestPayload_HaikuThinkingShape(t *testing.T) {
	t.Parallel()

	payload, err := createTestPayload("claude-haiku-4-5-20251001", "device-id-123", "acc-uuid-123")
	require.NoError(t, err)

	var parsed struct {
		Thinking struct {
			Type         string `json:"type"`
			BudgetTokens int    `json:"budget_tokens"`
		} `json:"thinking"`
	}
	var raw map[string]any

	require.NoError(t, json.Unmarshal(payload, &parsed))
	require.NoError(t, json.Unmarshal(payload, &raw))
	require.Equal(t, "enabled", parsed.Thinking.Type)
	require.Equal(t, 31999, parsed.Thinking.BudgetTokens)
	require.NotContains(t, raw, "output_config")
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
