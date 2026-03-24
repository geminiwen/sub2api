package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

// ── Stubs ────────────────────────────────────────────────────────────────

// stubTenguGatewayCache maps sessionHash → accountID.
type stubTenguGatewayCache struct {
	GatewayCache
	bindings map[string]int64
}

func (c *stubTenguGatewayCache) GetSessionAccountID(_ context.Context, _ int64, sessionHash string) (int64, error) {
	if id, ok := c.bindings[sessionHash]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("not found")
}
func (c *stubTenguGatewayCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}
func (c *stubTenguGatewayCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}
func (c *stubTenguGatewayCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

// stubTenguAccountRepo returns pre-configured accounts.
type stubTenguAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (r stubTenguAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account not found")
}

func (r stubTenguAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]Account, error) {
	var result []Account
	for _, acc := range r.accounts {
		if acc.Platform == platform {
			result = append(result, acc)
		}
	}
	return result, nil
}

func (r stubTenguAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByGroupIDAndPlatform(context.Background(), 0, platform)
}

func (r stubTenguAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]Account, error) {
	return r.ListSchedulableByPlatform(context.Background(), platform)
}

// stubTenguHTTPUpstream records forwarded requests and returns configurable responses.
type stubTenguHTTPUpstream struct {
	mu       sync.Mutex
	requests []*http.Request
	bodies   [][]byte
	// acceptedPerCall configures accepted_count for each successive call (cycling).
	acceptedPerCall []int
	callIndex       int
}

func (u *stubTenguHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.DoWithTLS(req, "", 0, 0, false)
}

func (u *stubTenguHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ bool) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()

	body, _ := io.ReadAll(req.Body)
	u.requests = append(u.requests, req)
	u.bodies = append(u.bodies, body)

	accepted := 1
	if len(u.acceptedPerCall) > 0 {
		accepted = u.acceptedPerCall[u.callIndex%len(u.acceptedPerCall)]
		u.callIndex++
	}

	respBody, _ := json.Marshal(map[string]int{
		"accepted_count": accepted,
		"rejected_count": 0,
	})
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// ── Helpers ──────────────────────────────────────────────────────────────

func newTenguTestGatewayService(accounts []Account, cache GatewayCache) *GatewayService {
	repo := stubTenguAccountRepo{accounts: accounts}
	concurrency := NewConcurrencyService(stubConcurrencyCache{})
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Gateway: config.GatewayConfig{
			Scheduling: config.GatewaySchedulingConfig{
				LoadBatchEnabled:         true,
				StickySessionMaxWaiting:  3,
				StickySessionWaitTimeout: time.Second,
				FallbackWaitTimeout:      time.Second,
				FallbackMaxWaiting:       10,
			},
		},
	}
	return &GatewayService{
		accountRepo:        repo,
		cache:              cache,
		cfg:                cfg,
		concurrencyService: concurrency,
		userGroupRateCache: gocache.New(time.Minute, time.Minute),
		modelsListCache:    gocache.New(time.Minute, time.Minute),
		modelsListCacheTTL: time.Minute,
	}
}

func tenguTestAccount(id int64, email, accountUUID, orgUUID, claudeUserID string) Account {
	now := time.Now().Add(-time.Minute)
	return Account{
		ID:          id,
		Platform:    PlatformAnthropic,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		Priority:    1,
		LastUsedAt:  &now,
		Credentials: map[string]any{
			"api_key":       fmt.Sprintf("sk-test-%d", id),
			"email_address": email,
		},
		Extra: map[string]any{
			"account_uuid":   accountUUID,
			"org_uuid":       orgUUID,
			"claude_user_id": claudeUserID,
		},
	}
}

func makeTenguBatchBody(events ...map[string]any) []byte {
	var wrappers []map[string]any
	for _, ed := range events {
		wrappers = append(wrappers, map[string]any{
			"event_type": "ClaudeCodeInternalEvent",
			"event_data": ed,
		})
	}
	b, _ := json.Marshal(map[string]any{"events": wrappers})
	return b
}

func tenguCtx() context.Context {
	return context.WithValue(context.Background(), ctxkey.ForcePlatform, PlatformAnthropic)
}

// ── Unit tests: enrichEventData ──────────────────────────────────────────

func TestEnrichEventData_OverwriteAll(t *testing.T) {
	account := &Account{ID: 42}
	meta := &TenguAccountMetadata{
		Email:            "server@example.com",
		AccountUUID:      "server-acc-uuid",
		OrganizationUUID: "server-org-uuid",
		DeviceID:         "server-device-id-64hex",
	}

	data := map[string]any{
		"event_name": "tengu_exit",
		"session_id": "client-session-abc",
		"device_id":  "client-device-id",
		"email":      "client@example.com",
		"auth": map[string]any{
			"account_uuid":      "client-acc-uuid",
			"organization_uuid": "client-org-uuid",
		},
	}

	enrichEventData(data, meta, account)

	// All identity fields overwritten with server values
	require.Equal(t, "server@example.com", data["email"])
	require.Equal(t, "server-device-id-64hex", data["device_id"])

	auth := data["auth"].(map[string]any)
	require.Equal(t, "server-acc-uuid", auth["account_uuid"])
	require.Equal(t, "server-org-uuid", auth["organization_uuid"])

	// session_id mapped through sticky derivation
	expectedSessionID := GenerateSessionUUID("42::client-session-abc")
	require.Equal(t, expectedSessionID, data["session_id"])

	// Non-identity field preserved
	require.Equal(t, "tengu_exit", data["event_name"])
}

func TestEnrichEventData_SessionIDMapping(t *testing.T) {
	account := &Account{ID: 99}
	meta := &TenguAccountMetadata{}

	data := map[string]any{
		"session_id": "e15f3697-61e5-4e66-9575-ef8ccfe93e61",
	}

	enrichEventData(data, meta, account)

	expected := GenerateSessionUUID("99::e15f3697-61e5-4e66-9575-ef8ccfe93e61")
	require.Equal(t, expected, data["session_id"])
}

func TestEnrichEventData_AuthCreatedWhenMissing(t *testing.T) {
	account := &Account{ID: 1}
	meta := &TenguAccountMetadata{
		AccountUUID:      "acc-uuid",
		OrganizationUUID: "org-uuid",
	}

	data := map[string]any{
		"event_name": "tengu_test",
	}

	enrichEventData(data, meta, account)

	auth, ok := data["auth"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "acc-uuid", auth["account_uuid"])
	require.Equal(t, "org-uuid", auth["organization_uuid"])
}

func TestEnrichEventData_EmptyMetaNoOverwrite(t *testing.T) {
	account := &Account{ID: 1}
	meta := &TenguAccountMetadata{} // all empty

	data := map[string]any{
		"email":     "keep@example.com",
		"device_id": "keep-device",
	}

	enrichEventData(data, meta, account)

	// When server meta is empty, client values stay (nothing to overwrite with)
	require.Equal(t, "keep@example.com", data["email"])
	require.Equal(t, "keep-device", data["device_id"])
}

// ── Integration tests: ProcessAndForward ─────────────────────────────────

func TestProcessAndForward_StickySessionRouting(t *testing.T) {
	acc1 := tenguTestAccount(1, "acc1@test.com", "acc1-uuid", "org1-uuid", "device1hex")
	acc2 := tenguTestAccount(2, "acc2@test.com", "acc2-uuid", "org2-uuid", "device2hex")

	cache := &stubTenguGatewayCache{
		bindings: map[string]int64{
			"sess-A": 1,
			"sess-B": 2,
		},
	}

	gw := newTenguTestGatewayService([]Account{acc1, acc2}, cache)
	upstream := &stubTenguHTTPUpstream{acceptedPerCall: []int{2, 1}}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(
		map[string]any{"event_name": "e1", "session_id": "sess-A", "email": "old@client.com"},
		map[string]any{"event_name": "e2", "session_id": "sess-A"},
		map[string]any{"event_name": "e3", "session_id": "sess-B", "email": "old@client.com"},
	)

	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{
		"User-Agent": []string{"claude-code/2.1.80"},
	})
	require.NoError(t, err)
	require.Equal(t, 3, result.TotalEvents)
	require.Equal(t, 0, result.DroppedEvents)
	require.Equal(t, 3, result.AcceptedCount) // 2 + 1

	// Two upstream requests sent (one per account)
	upstream.mu.Lock()
	require.Equal(t, 2, len(upstream.bodies))
	upstream.mu.Unlock()
}

func TestProcessAndForward_DropNoSessionID(t *testing.T) {
	acc := tenguTestAccount(1, "a@test.com", "acc-uuid", "org-uuid", "dev-hex")
	cache := &stubTenguGatewayCache{bindings: map[string]int64{"sess-ok": 1}}
	gw := newTenguTestGatewayService([]Account{acc}, cache)
	upstream := &stubTenguHTTPUpstream{acceptedPerCall: []int{1}}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(
		map[string]any{"event_name": "good", "session_id": "sess-ok"},
		map[string]any{"event_name": "no_session"},                          // missing session_id
		map[string]any{"event_name": "empty_session", "session_id": ""},     // empty session_id
	)

	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)
	require.Equal(t, 3, result.TotalEvents)
	require.Equal(t, 2, result.DroppedEvents) // 2 dropped
	require.Equal(t, 1, result.AcceptedCount)
}

func TestProcessAndForward_UnmappedSessionFallback(t *testing.T) {
	// When sticky session has no binding, SelectAccountWithLoadAwareness
	// falls back to load-aware selection. The event is NOT dropped.
	acc := tenguTestAccount(1, "a@test.com", "acc-uuid", "org-uuid", "dev-hex")
	cache := &stubTenguGatewayCache{bindings: map[string]int64{"sess-known": 1}}
	gw := newTenguTestGatewayService([]Account{acc}, cache)
	upstream := &stubTenguHTTPUpstream{acceptedPerCall: []int{2}}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(
		map[string]any{"event_name": "good", "session_id": "sess-known"},
		map[string]any{"event_name": "fallback", "session_id": "sess-unknown"},
	)

	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)
	require.Equal(t, 2, result.TotalEvents)
	require.Equal(t, 0, result.DroppedEvents)
	require.Equal(t, 2, result.AcceptedCount)
}

func TestProcessAndForward_AcceptedCountAggregation(t *testing.T) {
	acc1 := tenguTestAccount(1, "a@test.com", "acc1", "org1", "dev1")
	acc2 := tenguTestAccount(2, "b@test.com", "acc2", "org2", "dev2")

	cache := &stubTenguGatewayCache{bindings: map[string]int64{"s1": 1, "s2": 2}}
	gw := newTenguTestGatewayService([]Account{acc1, acc2}, cache)
	upstream := &stubTenguHTTPUpstream{acceptedPerCall: []int{5, 3}}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(
		map[string]any{"event_name": "e1", "session_id": "s1"},
		map[string]any{"event_name": "e2", "session_id": "s2"},
	)

	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)
	require.Equal(t, 0, result.DroppedEvents)
	require.Equal(t, 8, result.AcceptedCount) // 5 + 3
	require.Equal(t, 0, result.RejectedCount)
}

func TestProcessAndForward_DropMissingAccountUUID(t *testing.T) {
	// Account with empty account_uuid → events dropped with log
	now := time.Now().Add(-time.Minute)
	acc := Account{
		ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		LastUsedAt:  &now,
		Credentials: map[string]any{"api_key": "sk-test-1", "email_address": "a@test.com"},
		Extra:       map[string]any{"org_uuid": "org-uuid"}, // no account_uuid
	}
	cache := &stubTenguGatewayCache{bindings: map[string]int64{"sess": 1}}
	gw := newTenguTestGatewayService([]Account{acc}, cache)
	upstream := &stubTenguHTTPUpstream{}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(map[string]any{"event_name": "e1", "session_id": "sess"})
	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)
	require.Equal(t, 1, result.TotalEvents)
	require.Equal(t, 1, result.DroppedEvents)

	upstream.mu.Lock()
	require.Equal(t, 0, len(upstream.bodies)) // nothing forwarded
	upstream.mu.Unlock()
}

func TestProcessAndForward_DropMissingOrgUUID(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	acc := Account{
		ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		LastUsedAt:  &now,
		Credentials: map[string]any{"api_key": "sk-test-1", "email_address": "a@test.com", "account_uuid": "acc-uuid"},
		Extra:       map[string]any{}, // no org_uuid
	}
	cache := &stubTenguGatewayCache{bindings: map[string]int64{"sess": 1}}
	gw := newTenguTestGatewayService([]Account{acc}, cache)
	upstream := &stubTenguHTTPUpstream{}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(map[string]any{"event_name": "e1", "session_id": "sess"})
	result, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)
	require.Equal(t, 1, result.DroppedEvents)
}

func TestProcessAndForward_EnrichedBodyContent(t *testing.T) {
	acc := tenguTestAccount(42, "server@test.com", "srv-acc-uuid", "srv-org-uuid", "srv-device-hex")
	cache := &stubTenguGatewayCache{bindings: map[string]int64{"client-sess": 42}}
	gw := newTenguTestGatewayService([]Account{acc}, cache)
	upstream := &stubTenguHTTPUpstream{acceptedPerCall: []int{1}}
	svc := NewTenguProxyService(gw, upstream)

	body := makeTenguBatchBody(
		map[string]any{
			"event_name": "tengu_paste_text",
			"session_id": "client-sess",
			"device_id":  "old-device",
			"email":      "old@client.com",
			"auth": map[string]any{
				"account_uuid":      "old-acc",
				"organization_uuid": "old-org",
			},
		},
	)

	_, err := svc.ProcessAndForward(tenguCtx(), body, nil, http.Header{})
	require.NoError(t, err)

	// Parse the forwarded body
	upstream.mu.Lock()
	require.Equal(t, 1, len(upstream.bodies))
	forwardedBody := upstream.bodies[0]
	upstream.mu.Unlock()

	var batch struct {
		Events []struct {
			EventData map[string]any `json:"event_data"`
		} `json:"events"`
	}
	require.NoError(t, json.Unmarshal(forwardedBody, &batch))
	require.Equal(t, 1, len(batch.Events))

	ed := batch.Events[0].EventData
	require.Equal(t, "server@test.com", ed["email"])
	require.Equal(t, "srv-device-hex", ed["device_id"])
	require.Equal(t, GenerateSessionUUID("42::client-sess"), ed["session_id"])
	require.Equal(t, "tengu_paste_text", ed["event_name"]) // preserved

	auth, ok := ed["auth"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "srv-acc-uuid", auth["account_uuid"])
	require.Equal(t, "srv-org-uuid", auth["organization_uuid"])
}
