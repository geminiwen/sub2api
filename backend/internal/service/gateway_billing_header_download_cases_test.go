package service

import "testing"

import "github.com/stretchr/testify/require"

func TestBillingHeaderDownloadCases_CCVersionShortText(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
	header := buildAnthropicBillingHeader(body, "claude-cli/2.1.79 (external, cli)")
	require.Contains(t, header, "cc_version=2.1.79.7b3;")
}

func TestBillingHeaderDownloadCases_CCVersionOriginShape(t *testing.T) {
	body := []byte(`{"messages":[{"type":"user","message":{"content":[{"type":"text","text":"xxxxtxx-xxxxxxxxxxxxe"}]}}]}`)
	header := buildAnthropicBillingHeader(body, "claude-cli/2.1.79 (external, cli)")
	require.Contains(t, header, "cc_version=2.1.79.04b;")
}

func TestBillingHeaderDownloadCases_CCHFixture(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"system":[{"type":"text","text":"x-anthropic-billing-header: cc_version=2.1.79.04b; cc_entrypoint=sdk-cli; cch=00000;"}],"stream":false}`)
	updated := applyAnthropicBillingCCH(body, "/v1/messages")
	require.Contains(t, string(updated), "cch=758af;")
	require.Equal(t, body, applyAnthropicBillingCCH(body, "/v1/other"))
}
