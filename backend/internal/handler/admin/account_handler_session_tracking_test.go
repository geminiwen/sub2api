package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sessionCountCacheStub struct {
	activeBatchIDs  []int64
	trackedBatchIDs []int64

	activeBatch  map[int64]int
	trackedBatch map[int64]int
}

func (s *sessionCountCacheStub) RegisterSession(_ context.Context, _ int64, _ string, _ int, _ time.Duration) (bool, error) {
	return true, nil
}

func (s *sessionCountCacheStub) TrackSession(_ context.Context, _ int64, _ string) error {
	return nil
}

func (s *sessionCountCacheStub) RefreshSession(_ context.Context, _ int64, _ string, _ time.Duration) error {
	return nil
}

func (s *sessionCountCacheStub) GetActiveSessionCount(_ context.Context, accountID int64) (int, error) {
	return s.activeBatch[accountID], nil
}

func (s *sessionCountCacheStub) GetActiveSessionCountBatch(_ context.Context, accountIDs []int64, _ map[int64]time.Duration) (map[int64]int, error) {
	s.activeBatchIDs = append([]int64(nil), accountIDs...)
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = s.activeBatch[accountID]
	}
	return result, nil
}

func (s *sessionCountCacheStub) GetTrackedSessionCount(_ context.Context, accountID int64) (int, error) {
	return s.trackedBatch[accountID], nil
}

func (s *sessionCountCacheStub) GetTrackedSessionCountBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	s.trackedBatchIDs = append([]int64(nil), accountIDs...)
	result := make(map[int64]int, len(accountIDs))
	for _, accountID := range accountIDs {
		result[accountID] = s.trackedBatch[accountID]
	}
	return result, nil
}

func (s *sessionCountCacheStub) IsSessionActive(_ context.Context, _ int64, _ string) (bool, error) {
	return false, nil
}

func (s *sessionCountCacheStub) GetWindowCost(_ context.Context, _ int64) (float64, bool, error) {
	return 0, false, nil
}

func (s *sessionCountCacheStub) SetWindowCost(_ context.Context, _ int64, _ float64) error {
	return nil
}

func (s *sessionCountCacheStub) GetWindowCostBatch(_ context.Context, _ []int64) (map[int64]float64, error) {
	return nil, nil
}

func TestAccountHandlerList_UsesTrackedSessionsForAccountsWithoutLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now().UTC()
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{
			ID:        101,
			Name:      "limited",
			Platform:  service.PlatformAnthropic,
			Type:      service.AccountTypeOAuth,
			Status:    service.StatusActive,
			CreatedAt: now,
			UpdatedAt: now,
			Extra: map[string]any{
				"max_sessions":                 30,
				"session_idle_timeout_minutes": 5,
			},
		},
		{
			ID:        102,
			Name:      "tracked-only",
			Platform:  service.PlatformAnthropic,
			Type:      service.AccountTypeOAuth,
			Status:    service.StatusActive,
			CreatedAt: now,
			UpdatedAt: now,
			Extra:     map[string]any{},
		},
	}

	cache := &sessionCountCacheStub{
		activeBatch:  map[int64]int{101: 4},
		trackedBatch: map[int64]int{102: 9},
	}

	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, cache, nil, nil)
	router := gin.New()
	router.GET("/api/v1/admin/accounts", handler.List)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.EqualValues(t, 4, gjson.GetBytes(rec.Body.Bytes(), "data.items.0.active_sessions").Int())
	require.EqualValues(t, 9, gjson.GetBytes(rec.Body.Bytes(), "data.items.1.active_sessions").Int())
	require.EqualValues(t, 30, gjson.GetBytes(rec.Body.Bytes(), "data.items.0.max_sessions").Int())
	require.False(t, gjson.GetBytes(rec.Body.Bytes(), "data.items.1.max_sessions").Exists())
	require.Equal(t, []int64{101}, cache.activeBatchIDs)
	require.Equal(t, []int64{102}, cache.trackedBatchIDs)
}
