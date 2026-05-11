package service

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	gocache "github.com/patrickmn/go-cache"
	"github.com/stretchr/testify/require"
)

type modelQuotaAccountRepoStub struct {
	AccountRepository

	accounts []Account
	calls    atomic.Int64
}

func (s *modelQuotaAccountRepoStub) ListSchedulable(ctx context.Context) ([]Account, error) {
	s.calls.Add(1)
	return append([]Account(nil), s.accounts...), nil
}

func (s *modelQuotaAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	s.calls.Add(1)
	return append([]Account(nil), s.accounts...), nil
}

type modelQuotaUsageRepoStub struct {
	UsageLogRepository

	batchCalls atomic.Int64
	data5h     map[int64]*usagestats.AccountStats
	data7d     map[int64]*usagestats.AccountStats
}

func (s *modelQuotaUsageRepoStub) GetAccountWindowStatsBatch(ctx context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error) {
	s.batchCalls.Add(1)
	out := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	cutoff5h := time.Now().Add(-5 * time.Hour)
	for _, id := range accountIDs {
		if startTime.After(cutoff5h) {
			if v, ok := s.data5h[id]; ok {
				out[id] = v
			}
			continue
		}
		if v, ok := s.data7d[id]; ok {
			out[id] = v
		}
	}
	return out, nil
}

func accountForQuota(id int64, platform string, models ...string) Account {
	mapping := make(map[string]any, len(models))
	for _, model := range models {
		mapping[model] = model
	}
	return Account{
		ID:       id,
		Platform: platform,
		Credentials: map[string]any{
			"model_mapping": mapping,
		},
		Extra: map[string]any{},
	}
}

func newModelQuotaService(accounts []Account, usage *modelQuotaUsageRepoStub, ttl time.Duration) *GatewayService {
	return &GatewayService{
		accountRepo:          &modelQuotaAccountRepoStub{accounts: accounts},
		usageLogRepo:         usage,
		modelQuotasCache:     gocache.New(ttl, time.Minute),
		modelQuotasCacheTTL:  ttl,
	}
}

func TestGetModelQuotas_ClusterDedup(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1", "m2"),
		accountForQuota(2, "openai", "m1", "m2"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 50.0
	accounts[0].Extra["codex_7d_used_percent"] = 50.0
	accounts[1].Extra["codex_5h_used_percent"] = 50.0
	accounts[1].Extra["codex_7d_used_percent"] = 50.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 10}, 2: &usagestats.AccountStats{Cost: 20}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 30}, 2: &usagestats.AccountStats{Cost: 40}},
	}
	svc := newModelQuotaService(accounts, usage, time.Minute)

	r := svc.GetModelQuotas(context.Background(), nil, "openai")
	require.NotNil(t, r)
	require.Len(t, r.Data, 2)
	require.Equal(t, int64(2), usage.batchCalls.Load(), "one cluster should trigger exactly 2 batch calls (5h+7d)")
	require.Equal(t, r.Data[0].FiveHour, r.Data[1].FiveHour)
	require.Equal(t, r.Data[0].SevenDay, r.Data[1].SevenDay)
}

func TestGetModelQuotas_CacheHit(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	acc.Extra["codex_5h_used_percent"] = 50.0
	acc.Extra["codex_7d_used_percent"] = 50.0
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 10}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 20}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, 15*time.Second)

	_ = svc.GetModelQuotas(context.Background(), nil, "openai")
	_ = svc.GetModelQuotas(context.Background(), nil, "openai")
	require.Equal(t, int64(2), usage.batchCalls.Load(), "second call should hit cache")
}

func TestGetModelQuotas_CacheExpiry(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	acc.Extra["codex_5h_used_percent"] = 50.0
	acc.Extra["codex_7d_used_percent"] = 50.0
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 10}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 20}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, 15*time.Millisecond)

	_ = svc.GetModelQuotas(context.Background(), nil, "openai")
	time.Sleep(30 * time.Millisecond)
	_ = svc.GetModelQuotas(context.Background(), nil, "openai")
	require.Equal(t, int64(4), usage.batchCalls.Load(), "after expiry should recompute")
}

func TestGetModelQuotas_PositiveInference(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	acc.Extra["codex_5h_used_percent"] = 30.0
	acc.Extra["codex_7d_used_percent"] = 30.0
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, time.Minute)

	r := svc.GetModelQuotas(context.Background(), nil, "openai")
	require.Len(t, r.Data, 1)
	q := r.Data[0].FiveHour
	require.NotNil(t, q.TotalUSD)
	require.NotNil(t, q.RemainingUSD)
	require.InDelta(t, 40.0, *q.TotalUSD, 0.0001)
	require.InDelta(t, 28.0, *q.RemainingUSD, 0.0001)
}

func TestGetModelQuotas_UnknownTelemetry(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, time.Minute)

	r := svc.GetModelQuotas(context.Background(), nil, "openai")
	q := r.Data[0].FiveHour
	require.Nil(t, q.TotalUSD)
	require.Nil(t, q.RemainingUSD)
	require.Greater(t, q.UnknownAccountsCount, 0)
}

func TestGetModelQuotas_ZeroPercentClamp(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	acc.Extra["codex_5h_used_percent"] = 0.0
	acc.Extra["codex_7d_used_percent"] = 0.0
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, time.Minute)

	r := svc.GetModelQuotas(context.Background(), nil, "openai")
	q := r.Data[0].FiveHour
	require.Nil(t, q.TotalUSD)
	require.Nil(t, q.RemainingUSD)
	require.Equal(t, 1, q.UnknownAccountsCount)
}

func TestGetModelQuotas_NegativeRemainingClamp(t *testing.T) {
	acc := accountForQuota(1, "openai", "m1")
	acc.Extra["codex_5h_used_percent"] = 200.0
	acc.Extra["codex_7d_used_percent"] = 200.0
	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
		data7d: map[int64]*usagestats.AccountStats{1: &usagestats.AccountStats{Cost: 12}},
	}
	svc := newModelQuotaService([]Account{acc}, usage, time.Minute)

	r := svc.GetModelQuotas(context.Background(), nil, "openai")
	q := r.Data[0].FiveHour
	require.NotNil(t, q.RemainingUSD)
	require.InDelta(t, 0.0, *q.RemainingUSD, 0.0001)
}

func TestGetModelQuotas_NoDBSchemaChanges(t *testing.T) {
	patterns := []string{
		"../ent/schema/*model*quota*",
		"../internal/repository/migrations/*model*quota*",
		"../migrations/*model*quota*",
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		require.NoError(t, err)
		require.Len(t, matches, 0, "unexpected schema/migration artifacts: %v", matches)
	}
}
