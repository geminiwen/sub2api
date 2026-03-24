package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const tenguUpstreamURL = "https://api.anthropic.com/api/event_logging/v2/batch"

// TenguProxyService handles Tengu telemetry event proxying.
type TenguProxyService struct {
	gatewayService *GatewayService
	httpUpstream   HTTPUpstream
}

// NewTenguProxyService creates a new TenguProxyService.
func NewTenguProxyService(
	gatewayService *GatewayService,
	httpUpstream HTTPUpstream,
) *TenguProxyService {
	return &TenguProxyService{
		gatewayService: gatewayService,
		httpUpstream:   httpUpstream,
	}
}

// tenguAccountInfo caches per-account resolved data to avoid repeated lookups.
type tenguAccountInfo struct {
	Account  *Account
	Token    string
	DeviceID string
	Meta     *TenguAccountMetadata
}

// TenguAccountMetadata holds the account-level identity fields to write into events.
type TenguAccountMetadata struct {
	Email            string
	AccountUUID      string
	OrganizationUUID string
	DeviceID         string
}

// TenguForwardResult summarises the outcome of a batch forward.
type TenguForwardResult struct {
	TotalEvents   int
	DroppedEvents int
	// Aggregated upstream counts across all account batches.
	AcceptedCount int `json:"accepted_count"`
	RejectedCount int `json:"rejected_count"`
}

// ProcessAndForward is the main entry point. It:
//  1. Parses every event, extracts event_data.session_id
//  2. Looks up the sticky session → account for each session_id
//  3. Drops events whose session_id cannot be resolved
//  4. Groups remaining events by account, enriches them
//  5. Forwards one batch per account to upstream
func (s *TenguProxyService) ProcessAndForward(
	ctx context.Context,
	body []byte,
	groupID *int64,
	clientHeaders http.Header,
) (*TenguForwardResult, error) {
	// Parse batch
	var batch struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, fmt.Errorf("parse batch body: %w", err)
	}

	result := &TenguForwardResult{
		TotalEvents:    len(batch.Events),
	}

	// Resolve accounts and group events by account
	accountCache := make(map[string]*tenguAccountInfo) // sessionID → info
	type enrichedEvent struct {
		raw json.RawMessage
	}
	accountEvents := make(map[int64][]json.RawMessage)

	for _, rawEvent := range batch.Events {
		// Parse event to get session_id
		var wrapper struct {
			EventType string         `json:"event_type"`
			EventData map[string]any `json:"event_data"`
		}
		if err := json.Unmarshal(rawEvent, &wrapper); err != nil {
			result.DroppedEvents++
			continue
		}
		if wrapper.EventData == nil {
			result.DroppedEvents++
			continue
		}

		sessionID, _ := wrapper.EventData["session_id"].(string)
		if sessionID == "" {
			result.DroppedEvents++
			continue
		}

		// Resolve account for this session (with cache)
		info, ok := accountCache[sessionID]
		if !ok {
			info = s.resolveAccount(ctx, groupID, sessionID)
			accountCache[sessionID] = info // may be nil
		}
		if info == nil {
			logger.L().Warn("tengu: dropping event due to unresolved session binding",
				zap.String("session_id", sessionID),
			)
			result.DroppedEvents++
			continue
		}

		// Drop events when critical identity fields are missing
		if info.Meta.AccountUUID == "" || info.Meta.OrganizationUUID == "" {
			logger.L().Warn("tengu: dropping event due to missing account metadata",
				zap.Int64("account_id", info.Account.ID),
				zap.String("session_id", sessionID),
				zap.Bool("missing_account_uuid", info.Meta.AccountUUID == ""),
				zap.Bool("missing_org_uuid", info.Meta.OrganizationUUID == ""),
			)
			result.DroppedEvents++
			continue
		}

		// Enrich event_data in place
		enrichEventData(
			wrapper.EventData,
			info.Meta,
			info.Account,
			s.buildTelemetryEventBetas(wrapper.EventData),
		)

		// Re-encode the full event wrapper
		enriched, err := json.Marshal(wrapper)
		if err != nil {
			result.DroppedEvents++
			continue
		}
		accountEvents[info.Account.ID] = append(accountEvents[info.Account.ID], enriched)
	}

	// Forward one batch per account
	// Collect all unique account infos
	infoByID := make(map[int64]*tenguAccountInfo)
	for _, info := range accountCache {
		if info != nil {
			infoByID[info.Account.ID] = info
		}
	}

	// If all events were dropped, nothing to forward — return success immediately.
	if len(accountEvents) == 0 {
		return result, nil
	}

	for accountID, events := range accountEvents {
		info := infoByID[accountID]
		if info == nil {
			continue
		}

		batchBody, err := json.Marshal(map[string]any{"events": events})
		if err != nil {
			return nil, fmt.Errorf("marshal batch for account %d: %w", accountID, err)
		}

		accepted, rejected, err := s.forwardBatch(ctx, batchBody, info, clientHeaders)
		if err != nil {
			logger.L().Error("tengu: upstream forward failed",
				zap.Int64("account_id", accountID),
				zap.Error(err),
			)
			return nil, err
		}
		result.AcceptedCount += accepted
		result.RejectedCount += rejected
	}

	return result, nil
}

// resolveAccount looks up the sticky session for sessionID, selects the bound
// account, and builds its metadata. Returns nil if resolution fails.
func (s *TenguProxyService) resolveAccount(ctx context.Context, groupID *int64, sessionID string) *tenguAccountInfo {
	if direct := s.resolveAccountFromStickyAlias(ctx, sessionID); direct != nil {
		return direct
	}

	res, err := s.gatewayService.SelectAccountWithLoadAwareness(ctx, groupID, sessionID, "", nil, "")
	if err != nil || res == nil || res.Account == nil {
		return nil
	}

	return s.buildAccountInfo(ctx, res.Account)
}

func (s *TenguProxyService) resolveAccountFromStickyAlias(ctx context.Context, sessionID string) *tenguAccountInfo {
	accountID, err := s.gatewayService.GetCachedSessionAccountID(ctx, nil, sessionID)
	if err != nil || accountID <= 0 || s.gatewayService == nil || s.gatewayService.accountRepo == nil {
		return nil
	}

	account, err := s.gatewayService.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return nil
	}

	return s.buildAccountInfo(ctx, account)
}

func (s *TenguProxyService) buildAccountInfo(ctx context.Context, account *Account) *tenguAccountInfo {
	if account == nil {
		return nil
	}

	token, _, err := s.gatewayService.GetAccessToken(ctx, account)
	if err != nil {
		return nil
	}

	deviceID := account.GetClaudeUserID()
	if deviceID == "" {
		deviceID = generateClientID()
	}

	return &tenguAccountInfo{
		Account:  account,
		Token:    token,
		DeviceID: deviceID,
		Meta: &TenguAccountMetadata{
			Email:            account.GetCredential("email_address"),
			AccountUUID:      account.GetAccountUUID(),
			OrganizationUUID: account.GetExtraString("org_uuid"),
			DeviceID:         deviceID,
		},
	}
}

// forwardBatch sends an enriched batch to upstream for a single account.
// Returns (accepted_count, rejected_count, error).
func (s *TenguProxyService) forwardBatch(
	ctx context.Context,
	batchBody []byte,
	info *tenguAccountInfo,
	clientHeaders http.Header,
) (int, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tenguUpstreamURL, bytes.NewReader(batchBody))
	if err != nil {
		return 0, 0, fmt.Errorf("build upstream request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, compress, deflate, br")
	req.Header.Set("x-service-name", "claude-code")
	req.Header.Set("anthropic-beta", claude.BetaOAuth)

	if ua := clientHeaders.Get("User-Agent"); ua != "" {
		req.Header.Set("User-Agent", StripUserAgentSourceDescriptor(ua, "codehub"))
	}
	sanitizeAnthropicUpstreamUserAgentHeader(req.Header)
	normalizeClaudeHeaderCaseForWire(req.Header)

	account := info.Account
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}

	resp, err := s.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, account.IsTLSFingerprintEnabled())
	if err != nil {
		return 0, 0, fmt.Errorf("upstream request: %w", err)
	}
	defer DrainAndClose(resp.Body)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, fmt.Errorf("read upstream response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, 0, fmt.Errorf("upstream returned non-2xx status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse upstream response: {"accepted_count": N, "rejected_count": N}
	var upstreamResp struct {
		AcceptedCount int `json:"accepted_count"`
		RejectedCount int `json:"rejected_count"`
	}
	_ = json.Unmarshal(respBody, &upstreamResp)

	return upstreamResp.AcceptedCount, upstreamResp.RejectedCount, nil
}

func (s *TenguProxyService) buildTelemetryEventBetas(eventData map[string]any) string {
	modelID := telemetryEventModel(eventData)
	clientBetas := telemetryEventBetas(eventData)
	if s != nil && s.gatewayService != nil {
		return s.gatewayService.getBetaHeader(modelID, clientBetas)
	}

	isHaikuModel := strings.Contains(strings.ToLower(modelID), "haiku")
	defaultBetaHeader := claude.DefaultBetaHeader
	if isHaikuModel {
		defaultBetaHeader = claude.HaikuBetaHeader
	} else if containsBetaToken(clientBetas, claude.BetaContext1M) {
		defaultBetaHeader = claude.DefaultBetaHeaderWithContext1M
	}
	if clientBetas != "" {
		return mergeAnthropicBeta(strings.Split(defaultBetaHeader, ","), clientBetas)
	}
	return defaultBetaHeader
}

func telemetryEventModel(eventData map[string]any) string {
	if eventData == nil {
		return ""
	}
	for _, key := range []string{"model", "model_id", "modelId"} {
		if value, ok := eventData[key].(string); ok {
			value = strings.TrimSpace(value)
			if value != "" {
				return value
			}
		}
	}
	return ""
}

func telemetryEventBetas(eventData map[string]any) string {
	if eventData == nil {
		return ""
	}
	value, _ := eventData["betas"].(string)
	return strings.TrimSpace(value)
}

// enrichEventData overwrites identity fields inside a single event_data object
// with the server-side account values. All identity fields are replaced
// unconditionally — client values are not preserved.
//
// session_id is rewritten through the sticky session derivation:
//
//	seed = fmt.Sprintf("%d::%s", account.ID, clientSessionID)
//	stickySessionID = GenerateSessionUUID(seed)
func enrichEventData(data map[string]any, meta *TenguAccountMetadata, account *Account, betas string) {
	if meta.Email != "" {
		data["email"] = meta.Email
	}
	if meta.DeviceID != "" {
		data["device_id"] = meta.DeviceID
	}
	if strings.TrimSpace(betas) != "" {
		data["betas"] = betas
	}

	// session_id: derive sticky session UUID
	if clientSessionID, ok := data["session_id"].(string); ok && clientSessionID != "" {
		seed := fmt.Sprintf("%d::%s", account.ID, clientSessionID)
		data["session_id"] = GenerateSessionUUID(seed)
	}

	// auth sub-object: overwrite with server values
	auth, _ := data["auth"].(map[string]any)
	if auth == nil {
		auth = make(map[string]any)
		data["auth"] = auth
	}
	if meta.AccountUUID != "" {
		auth["account_uuid"] = meta.AccountUUID
	}
	if meta.OrganizationUUID != "" {
		auth["organization_uuid"] = meta.OrganizationUUID
	}
}

// setIfMissing sets a field only if it's not already present or is empty.
func setIfMissing(m map[string]any, key, value string) {
	if value == "" {
		return
	}
	existing, ok := m[key]
	if !ok || existing == nil || existing == "" {
		m[key] = value
	}
}

// LogTenguRequest logs a Tengu proxy request for auditing.
func LogTenguRequest(result *TenguForwardResult) {
	logger.L().Info("tengu proxy request",
		zap.String("telemetry_route", "tengu"),
		zap.Int("total_events", result.TotalEvents),
		zap.Int("dropped_events", result.DroppedEvents),
		zap.Int("accepted_count", result.AcceptedCount),
		zap.Int("rejected_count", result.RejectedCount),
	)
}

// CountEvents returns the number of events in the batch body.
func CountEvents(body []byte) int {
	var batch struct {
		Events []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return 0
	}
	return len(batch.Events)
}

// DrainAndClose reads and discards the remaining body content, then closes it.
func DrainAndClose(body io.ReadCloser) {
	if body != nil {
		_, _ = io.Copy(io.Discard, body)
		_ = body.Close()
	}
}
