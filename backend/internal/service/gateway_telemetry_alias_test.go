package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type telemetryAliasCacheStub struct {
	bindings map[int64]map[string]int64
}

func (c *telemetryAliasCacheStub) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	if c.bindings == nil {
		return 0, nil
	}
	if groupBindings, ok := c.bindings[groupID]; ok {
		return groupBindings[sessionHash], nil
	}
	return 0, nil
}

func (c *telemetryAliasCacheStub) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	if c.bindings == nil {
		c.bindings = make(map[int64]map[string]int64)
	}
	if c.bindings[groupID] == nil {
		c.bindings[groupID] = make(map[string]int64)
	}
	c.bindings[groupID][sessionHash] = accountID
	return nil
}

func (c *telemetryAliasCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *telemetryAliasCacheStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func TestBindTelemetrySessionAliases_BindsOriginalAndRewrittenSessionIDs(t *testing.T) {
	cache := &telemetryAliasCacheStub{}
	svc := &GatewayService{cache: cache}
	account := &Account{
		ID:   121,
		Type: AccountTypeOAuth,
	}

	metadataUserID := FormatMetadataUserID(
		"2c64ec8dcac4cd64e35f6693f38e2096c3ec3d484ab4a7812c3d335d0964cf0d",
		"53432939-e667-4852-913c-c27b6b089405",
		"client-session-1234-5678-9012-abcdefabcdef",
		"2.1.81",
	)

	err := svc.BindTelemetrySessionAliases(context.Background(), account, "sticky-session-key", metadataUserID)
	require.NoError(t, err)

	require.Equal(t, int64(121), cache.bindings[0]["sticky-session-key"])
	require.Equal(t, int64(121), cache.bindings[0]["client-session-1234-5678-9012-abcdefabcdef"])
	require.Equal(t, int64(121), cache.bindings[0][GenerateSessionUUID("121::client-session-1234-5678-9012-abcdefabcdef")])
}

func TestBindTelemetrySessionAliases_SkipsNonOAuthAccounts(t *testing.T) {
	cache := &telemetryAliasCacheStub{}
	svc := &GatewayService{cache: cache}
	account := &Account{
		ID:   121,
		Type: AccountTypeAPIKey,
	}

	err := svc.BindTelemetrySessionAliases(context.Background(), account, "sticky-session-key", "ignored")
	require.NoError(t, err)
	require.Empty(t, cache.bindings)
}
