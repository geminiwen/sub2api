package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type testIdentityCache struct {
	fingerprint     *Fingerprint
	maskedSessionID string
}

func (c *testIdentityCache) GetFingerprint(ctx context.Context, accountID int64) (*Fingerprint, error) {
	if c.fingerprint == nil {
		return nil, nil
	}
	clone := *c.fingerprint
	return &clone, nil
}

func (c *testIdentityCache) SetFingerprint(ctx context.Context, accountID int64, fp *Fingerprint) error {
	if fp == nil {
		c.fingerprint = nil
		return nil
	}
	clone := *fp
	c.fingerprint = &clone
	return nil
}

func (c *testIdentityCache) GetMaskedSessionID(ctx context.Context, accountID int64) (string, error) {
	return c.maskedSessionID, nil
}

func (c *testIdentityCache) SetMaskedSessionID(ctx context.Context, accountID int64, sessionID string) error {
	c.maskedSessionID = sessionID
	return nil
}

func TestComposeLearnedClaudeCLIUserAgent_UsesCachedVersionAndClientDescriptors(t *testing.T) {
	got := composeLearnedClaudeCLIUserAgent(
		"claude-cli/2.1.79 (external, cli)",
		"claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp, workload/cron)",
	)
	require.Equal(t, "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp, workload/cron)", got)
}

func TestGetOrCreateFingerprint_UpdatesDescriptorsWithoutVersionBump(t *testing.T) {
	cache := &testIdentityCache{
		fingerprint: &Fingerprint{
			ClientID:                "client-1",
			UserAgent:               "claude-cli/2.1.79 (external, cli)",
			StainlessLang:           "js",
			StainlessPackageVersion: "0.70.0",
			StainlessOS:             "Linux",
			StainlessArch:           "arm64",
			StainlessRuntime:        "node",
			StainlessRuntimeVersion: "v24.13.0",
			UpdatedAt:               1,
		},
	}
	svc := NewIdentityService(cache)

	headers := http.Header{}
	headers.Set("User-Agent", "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp)")

	fp, err := svc.GetOrCreateFingerprint(context.Background(), 42, headers)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp)", fp.UserAgent)
	require.NotNil(t, cache.fingerprint)
	require.Equal(t, fp.UserAgent, cache.fingerprint.UserAgent)
}

func TestGetOrCreateFingerprint_UsesNewerVersionAndClientDescriptors(t *testing.T) {
	cache := &testIdentityCache{
		fingerprint: &Fingerprint{
			ClientID:                "client-1",
			UserAgent:               "claude-cli/2.1.78 (external, cli)",
			StainlessLang:           "js",
			StainlessPackageVersion: "0.70.0",
			StainlessOS:             "Linux",
			StainlessArch:           "arm64",
			StainlessRuntime:        "node",
			StainlessRuntimeVersion: "v24.13.0",
			UpdatedAt:               1,
		},
	}
	svc := NewIdentityService(cache)

	headers := http.Header{}
	headers.Set("User-Agent", "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp, workload/cron)")

	fp, err := svc.GetOrCreateFingerprint(context.Background(), 42, headers)
	require.NoError(t, err)
	require.Equal(t, "claude-cli/2.1.79 (external, sdk-cli, agent-sdk/0.1.12, client-app/myapp, workload/cron)", fp.UserAgent)
	require.NotNil(t, cache.fingerprint)
	require.Equal(t, fp.UserAgent, cache.fingerprint.UserAgent)
}
