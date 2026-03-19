package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildAnthropicBillingHeader_UsesDocumentedFormula(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		]
	}`)

	header := buildAnthropicBillingHeader(body, "claude-cli/2.1.79 (external, cli)")
	require.Equal(t, "x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=cli; cch=00000;", header)
}

func TestUpsertAnthropicBillingHeaderSystemBlock_ReplacesExistingInPlaceAndPreservesOthers(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"system": [
			{"type": "text", "text": "before"},
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=old; cc_entrypoint=sdk-cli; cch=53f1c;"},
			{"type": "text", "text": "keep me"}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		]
	}`)

	updated := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.79 (external, cli)", "/v1/messages")

	var req struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	require.NoError(t, json.Unmarshal(updated, &req))
	require.Len(t, req.System, 3)
	require.Equal(t, "text", req.System[0].Type)
	require.Equal(t, "before", req.System[0].Text)
	require.Equal(t, "x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=cli; cch=00000;", req.System[1].Text)
	require.Equal(t, "keep me", req.System[2].Text)
}

func TestUpsertAnthropicBillingHeaderSystemBlock_SkipsOlderCLIVersionAndRemovesExisting(t *testing.T) {
	body := []byte(`{
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=old; cc_entrypoint=sdk-cli; cch=53f1c;"},
			{"type": "text", "text": "keep me"}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		]
	}`)

	updated := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.77 (external, cli)", "/v1/messages")

	var req struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	require.NoError(t, json.Unmarshal(updated, &req))
	require.Len(t, req.System, 1)
	require.Equal(t, "keep me", req.System[0].Text)
}

func TestShouldInjectAnthropicBillingHeader(t *testing.T) {
	require.True(t, shouldInjectAnthropicBillingHeader("claude-cli/2.1.78 (external, cli)"))
	require.True(t, shouldInjectAnthropicBillingHeader("claude-cli/2.1.79 (external, cli)"))
	require.False(t, shouldInjectAnthropicBillingHeader("claude-cli/2.1.77 (external, cli)"))
	require.False(t, shouldInjectAnthropicBillingHeader("curl/8.0.1"))
}

func TestUpsertAnthropicBillingHeaderSystemBlock_DoesNothingWhenMissing(t *testing.T) {
	body := []byte(`{
		"system": [
			{"type": "text", "text": "before"},
			{"type": "text", "text": "keep me"}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		]
	}`)

	updated := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.79 (external, cli)", "/v1/messages")

	var req struct {
		System []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"system"`
	}
	require.NoError(t, json.Unmarshal(updated, &req))
	require.Len(t, req.System, 2)
	require.Equal(t, "before", req.System[0].Text)
	require.Equal(t, "keep me", req.System[1].Text)
}

func TestUpsertAnthropicBillingHeaderSystemBlock_KeepsPlaceholderOutsideMessagesPath(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-5",
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=old; cc_entrypoint=sdk-cli; cch=53f1c;"}
		],
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		]
	}`)

	updated := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.79 (external, cli)", "/v1/complete")

	var req struct {
		System []struct {
			Text string `json:"text"`
		} `json:"system"`
	}
	require.NoError(t, json.Unmarshal(updated, &req))
	require.Len(t, req.System, 1)
	require.Equal(t, "x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=cli; cch=00000;", req.System[0].Text)
}

func TestApplyAnthropicBillingCCH_FillsPlaceholderForMessagesPath(t *testing.T) {
	body := []byte(`{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=cli; cch=00000;"}],"messages":[{"role":"user","content":[{"type":"text","text":"0000t00-000000000000e"}]}]}`)

	updated := applyAnthropicBillingCCH(body, "/v1/messages")

	require.Equal(t, `{"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=cli; cch=ad29d;"}],"messages":[{"role":"user","content":[{"type":"text","text":"0000t00-000000000000e"}]}]}`, string(updated))
}

func TestApplyAnthropicBillingCCH_MatchesPythonFixture(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=sdk-cli; cch=00000;"}],"stream":false}`)

	updated := applyAnthropicBillingCCH(body, "/v1/messages")

	require.Contains(t, string(updated), "cch=758af;")
}
