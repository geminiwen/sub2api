package service

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
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

func TestApplyAnthropicBillingCCH_MatchesFixture(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=sdk-cli; cch=00000;"}],"stream":false}`)

	updated := applyAnthropicBillingCCH(body, "/v1/messages")

	require.Contains(t, string(updated), "cch=758af;")
}

func TestApplyAnthropicBillingCCH_UsesUpdatedEntrypointFromBillingHeader(t *testing.T) {
	body := []byte(`{
		"model": "claude-sonnet-4-6",
		"messages": [
			{
				"role": "user",
				"content": [
					{"type":"text","text":"hi"}
				]
			}
		],
		"system": [
			{"type":"text","text":"x-anthropic-billing-header: cc_version=old; cc_entrypoint=old; cch=00000;"}
		],
		"stream": false
	}`)

	cliBody := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.79 (external, cli)", "/v1/messages")
	sdkCLIBody := upsertAnthropicBillingHeaderSystemBlock(body, "claude-cli/2.1.79 (external, sdk-cli)", "/v1/messages")

	cliUpdated := applyAnthropicBillingCCH(cliBody, "/v1/messages")
	sdkCLIUpdated := applyAnthropicBillingCCH(sdkCLIBody, "/v1/messages")

	require.Contains(t, string(cliUpdated), "cc_entrypoint=cli;")
	require.Contains(t, string(sdkCLIUpdated), "cc_entrypoint=sdk-cli;")
	require.NotEqual(t, string(cliUpdated), string(sdkCLIUpdated))
}

func TestApplyAnthropicBillingCCH_RecomputesAfterResettingExistingCCH(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=sdk-cli; cch=12345;"}],"stream":false}`)

	updated := applyAnthropicBillingCCH(body, "/v1/messages")

	require.Contains(t, string(updated), "cc_entrypoint=sdk-cli;")
	require.Contains(t, string(updated), "cch=758af;")
	require.NotContains(t, string(updated), "cch=12345;")
}

func TestApplyAnthropicBillingCCH_SkipsWhenBillingHeaderMissing(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"contains cch=00000 but no billing header"}]}],"system":[{"type":"text","text":"sys"}],"stream":false}`)

	updated := applyAnthropicBillingCCH(body, "/v1/messages")

	require.Equal(t, string(body), string(updated))
}

func TestNormalizeClaudeHeaderCaseForWire_RewritesSelectedHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	require.NoError(t, err)

	req.Header.Set("Anthropic-Beta", "beta")
	req.Header.Set("Anthropic-Version", "2023-06-01")
	req.Header.Set("Anthropic-Dangerous-Direct-Browser-Access", "true")
	req.Header.Set("X-App", "cli")
	req.Header.Set("X-Service-Name", "claude-code")
	req.Header.Set("X-Stainless-OS", "MacOS")

	normalizeClaudeHeaderCaseForWire(req.Header)

	var buf bytes.Buffer
	require.NoError(t, req.Write(&buf))
	wire := buf.String()

	require.Contains(t, wire, "anthropic-beta: beta\r\n")
	require.Contains(t, wire, "anthropic-version: 2023-06-01\r\n")
	require.Contains(t, wire, "anthropic-dangerous-direct-browser-access: true\r\n")
	require.Contains(t, wire, "x-app: cli\r\n")
	require.Contains(t, wire, "x-service-name: claude-code\r\n")
	require.Contains(t, wire, "X-Stainless-OS: MacOS\r\n")
	require.NotContains(t, wire, "Anthropic-Beta:")
	require.NotContains(t, wire, "Anthropic-Version:")
	require.NotContains(t, wire, "Anthropic-Dangerous-Direct-Browser-Access:")
	require.NotContains(t, wire, "X-App:")
	require.NotContains(t, wire, "X-Service-Name:")
	require.NotContains(t, wire, "X-Stainless-Os:")
}

func TestSanitizeAnthropicUpstreamUserAgentHeader_StripsCodeHubMarker(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "tail",
			in:   "claude-cli/2.1.81 (external, cli, codehub)",
			want: "claude-cli/2.1.81 (external, cli)",
		},
		{
			name: "middle",
			in:   "claude-cli/2.1.81 (external, codehub, cli)",
			want: "claude-cli/2.1.81 (external, cli)",
		},
		{
			name: "head",
			in:   "claude-cli/2.1.81 (codehub, cli, external)",
			want: "claude-cli/2.1.81 (cli, external)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
			require.NoError(t, err)

			req.Header.Set("User-Agent", tt.in)
			sanitizeAnthropicUpstreamUserAgentHeader(req.Header)

			require.Equal(t, tt.want, req.Header.Get("User-Agent"))
		})
	}
}

func TestBuildAnthropicBillingHeader_PreservesClientEntrypoint(t *testing.T) {
	body := []byte(`{
		"messages": [
			{
				"role": "user",
				"content": [
					{"type": "text", "text": "0000t00-000000000000e"}
				]
			}
		],
		"system": [
			{"type": "text", "text": "x-anthropic-billing-header: cc_version=old; cc_entrypoint=sdk-cli; cch=53f1c;"}
		]
	}`)

	header := buildAnthropicBillingHeader(body, "claude-cli/2.1.79 (external, sdk-cli)")
	require.Equal(t, "x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=sdk-cli; cch=00000;", header)
}

func TestResolveClaudeCLIEntrypoint_FromUserAgent(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want string
	}{
		{name: "plain cli", ua: "claude-cli/2.1.79 (external, cli)", want: "cli"},
		{name: "plain sdk cli", ua: "claude-cli/2.1.79 (external, sdk-cli)", want: "sdk-cli"},
		{name: "agent sdk", ua: "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12)", want: "sdk-cli"},
		{name: "agent sdk client app", ua: "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp)", want: "sdk-cli"},
		{name: "agent sdk workload", ua: "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp, workload/cron)", want: "sdk-cli"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveClaudeCLIEntrypoint(tc.ua))
		})
	}
}

func TestResolveClaudeMimicUserAgent_PrefersCachedClaudeCLIVersion(t *testing.T) {
	require.Equal(t, "claude-cli/2.1.90 (external, cli)", resolveClaudeMimicUserAgent("claude-cli/2.1.90 (external, cli)"))
}

func TestResolveClaudeMimicUserAgent_FallsBackToDefaultForNonClaudeCLI(t *testing.T) {
	require.Equal(t, claude.DefaultCLIUserAgent, resolveClaudeMimicUserAgent("curl/8.0.1"))
}

func TestApplyClaudeCodeMimicHeaders_PreservesPreferredClaudeCLIVersion(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	require.NoError(t, err)

	req.Header.Set("user-agent", "claude-cli/2.1.90 (external, cli)")
	applyClaudeCodeMimicHeaders(req, true, req.Header.Get("user-agent"))

	require.Equal(t, "claude-cli/2.1.90 (external, cli)", req.Header.Get("user-agent"))
	require.Equal(t, "cli", req.Header.Get("x-app"))
}
