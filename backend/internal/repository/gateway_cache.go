package repository

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const stickySessionPrefix = "sticky_session:"
const accountSessionCountPrefix = "account_session_count:"
const accountSessionLimitEnabledPrefix = "account_session_limit_enabled:"

type gatewayCache struct {
	rdb *redis.Client
}

func NewGatewayCache(rdb *redis.Client) service.GatewayCache {
	return &gatewayCache{rdb: rdb}
}

func (c *gatewayCache) GetSessionAccountID(ctx context.Context, sessionHash string) (int64, error) {
	key := stickySessionPrefix + sessionHash
	return c.rdb.Get(ctx, key).Int64()
}

func (c *gatewayCache) SetSessionAccountID(ctx context.Context, sessionHash string, accountID int64, ttl time.Duration) error {
	key := stickySessionPrefix + sessionHash
	return c.rdb.Set(ctx, key, accountID, ttl).Err()
}

func (c *gatewayCache) RefreshSessionTTL(ctx context.Context, sessionHash string, ttl time.Duration) error {
	key := stickySessionPrefix + sessionHash
	return c.rdb.Expire(ctx, key, ttl).Err()
}

func (c *gatewayCache) IncrAccountSessionCount(ctx context.Context, accountID int64, ttl time.Duration) (int64, error) {
	key := fmt.Sprintf("%s%d", accountSessionCountPrefix, accountID)
	count, err := c.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// 仅在首次创建时设置 TTL
	if count == 1 {
		c.rdb.Expire(ctx, key, ttl)
	}
	return count, nil
}

func (c *gatewayCache) GetAccountSessionCount(ctx context.Context, accountID int64) (int64, error) {
	key := fmt.Sprintf("%s%d", accountSessionCountPrefix, accountID)
	count, err := c.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return count, err
}

func (c *gatewayCache) GetAccountSessionCountBatch(ctx context.Context, accountIDs []int64) (map[int64]int64, error) {
	if len(accountIDs) == 0 {
		return make(map[int64]int64), nil
	}

	// 构建所有keys
	keys := make([]string, len(accountIDs))
	for i, accountID := range accountIDs {
		keys[i] = fmt.Sprintf("%s%d", accountSessionCountPrefix, accountID)
	}

	// 批量获取
	results, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	// 构建结果map
	counts := make(map[int64]int64, len(accountIDs))
	for i, accountID := range accountIDs {
		if results[i] != nil {
			if countStr, ok := results[i].(string); ok {
				if count, err := strconv.ParseInt(countStr, 10, 64); err == nil {
					counts[accountID] = count
					continue
				}
			}
		}
		counts[accountID] = 0
	}

	return counts, nil
}

func (c *gatewayCache) DeleteAccountSessionCount(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", accountSessionCountPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}

func (c *gatewayCache) SetAccountSessionLimitEnabled(ctx context.Context, accountID int64, enabled bool) error {
	key := fmt.Sprintf("%s%d", accountSessionLimitEnabledPrefix, accountID)
	if enabled {
		return c.rdb.Set(ctx, key, "1", 0).Err() // 不过期
	}
	return c.rdb.Del(ctx, key).Err() // 关闭时删除 key
}

func (c *gatewayCache) GetAccountSessionLimitEnabled(ctx context.Context, accountID int64) (bool, error) {
	key := fmt.Sprintf("%s%d", accountSessionLimitEnabledPrefix, accountID)
	val, err := c.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil // key 不存在，默认关闭
	}
	if err != nil {
		return false, err
	}
	return val == "1", nil
}

func (c *gatewayCache) DeleteAccountSessionLimitEnabled(ctx context.Context, accountID int64) error {
	key := fmt.Sprintf("%s%d", accountSessionLimitEnabledPrefix, accountID)
	return c.rdb.Del(ctx, key).Err()
}
