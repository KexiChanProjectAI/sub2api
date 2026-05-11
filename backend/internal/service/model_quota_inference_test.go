package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

// TestModelQuotaInference_FormulaCorrectness verifies the quota inference math
func TestModelQuotaInference_FormulaCorrectness(t *testing.T) {
	tests := []struct {
		name           string
		usedUSD        float64
		percent        float64
		expectedTotal  float64
		expectedRemain float64
	}{
		{"12_used_at_30pct", 12.0, 30.0, 40.0, 28.0},
		{"50_used_at_100pct", 50.0, 100.0, 50.0, 0.0},
		{"5_used_at_5pct", 5.0, 5.0, 100.0, 95.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			accounts := []Account{
				accountForQuota(1, "openai", "m1"),
			}
			accounts[0].Extra["codex_5h_used_percent"] = tt.percent
			accounts[0].Extra["codex_7d_used_percent"] = tt.percent

			usage := &modelQuotaUsageRepoStub{
				data5h: map[int64]*usagestats.AccountStats{
					1: {Cost: tt.usedUSD},
				},
				data7d: map[int64]*usagestats.AccountStats{
					1: {Cost: tt.usedUSD},
				},
			}

			svc := newModelQuotaService(accounts, usage, time.Hour)
			resp := svc.GetModelQuotas(context.Background(), nil, "openai")

			require.NotNil(t, resp.Data)
			require.Len(t, resp.Data, 1)
			q := resp.Data[0]

			// 5h window
			require.NotNil(t, q.FiveHour.TotalUSD)
			require.NotNil(t, q.FiveHour.RemainingUSD)
			require.InDelta(t, tt.expectedTotal, *q.FiveHour.TotalUSD, 0.001)
			require.InDelta(t, tt.expectedRemain, *q.FiveHour.RemainingUSD, 0.001)
			require.InDelta(t, tt.usedUSD, q.FiveHour.UsedUSD, 0.001)
			require.Equal(t, 1, q.FiveHour.AccountsCount)
			require.Equal(t, 0, q.FiveHour.UnknownAccountsCount)

			// 7d window
			require.NotNil(t, q.SevenDay.TotalUSD)
			require.NotNil(t, q.SevenDay.RemainingUSD)
			require.InDelta(t, tt.expectedTotal, *q.SevenDay.TotalUSD, 0.001)
			require.InDelta(t, tt.expectedRemain, *q.SevenDay.RemainingUSD, 0.001)
			require.InDelta(t, tt.usedUSD, q.SevenDay.UsedUSD, 0.001)
			require.Equal(t, 1, q.SevenDay.AccountsCount)
			require.Equal(t, 0, q.SevenDay.UnknownAccountsCount)
		})
	}
}

// TestModelQuotaInference_MissingPercent tests account with no percent data
func TestModelQuotaInference_MissingPercent(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: missing percent → unknown
	require.Nil(t, q.FiveHour.TotalUSD)
	require.Nil(t, q.FiveHour.RemainingUSD)
	require.Equal(t, 10.0, q.FiveHour.UsedUSD)
	require.Equal(t, 1, q.FiveHour.AccountsCount)
	require.Equal(t, 1, q.FiveHour.UnknownAccountsCount)

	// 7d window: missing percent → unknown
	require.Nil(t, q.SevenDay.TotalUSD)
	require.Nil(t, q.SevenDay.RemainingUSD)
	require.Equal(t, 10.0, q.SevenDay.UsedUSD)
	require.Equal(t, 1, q.SevenDay.AccountsCount)
	require.Equal(t, 1, q.SevenDay.UnknownAccountsCount)
}

// TestModelQuotaInference_ZeroPercent tests account with zero percent
func TestModelQuotaInference_ZeroPercent(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 0.0
	accounts[0].Extra["codex_7d_used_percent"] = 0.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: zero percent → unknown
	require.Nil(t, q.FiveHour.TotalUSD)
	require.Nil(t, q.FiveHour.RemainingUSD)
	require.Equal(t, 10.0, q.FiveHour.UsedUSD)
	require.Equal(t, 1, q.FiveHour.UnknownAccountsCount)

	// 7d window: zero percent → unknown
	require.Nil(t, q.SevenDay.TotalUSD)
	require.Nil(t, q.SevenDay.RemainingUSD)
	require.Equal(t, 10.0, q.SevenDay.UsedUSD)
	require.Equal(t, 1, q.SevenDay.UnknownAccountsCount)
}

// TestModelQuotaInference_ZeroUsageNonzeroPercent tests account with 0 used but nonzero percent
func TestModelQuotaInference_ZeroUsageNonzeroPercent(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 50.0
	accounts[0].Extra["codex_7d_used_percent"] = 50.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 0.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 0.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: used_usd <= 0 → unknown
	require.Nil(t, q.FiveHour.TotalUSD)
	require.Nil(t, q.FiveHour.RemainingUSD)
	require.Equal(t, 0.0, q.FiveHour.UsedUSD)
	require.Equal(t, 1, q.FiveHour.UnknownAccountsCount)

	// 7d window
	require.Nil(t, q.SevenDay.TotalUSD)
	require.Nil(t, q.SevenDay.RemainingUSD)
	require.Equal(t, 0.0, q.SevenDay.UsedUSD)
	require.Equal(t, 1, q.SevenDay.UnknownAccountsCount)
}

// TestModelQuotaInference_Over100Percent tests account with >100% utilization
func TestModelQuotaInference_Over100Percent(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 150.0
	accounts[0].Extra["codex_7d_used_percent"] = 150.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 15.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 15.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: over 100% → total = 10, remaining = 0 (clamped)
	require.NotNil(t, q.FiveHour.TotalUSD)
	require.NotNil(t, q.FiveHour.RemainingUSD)
	require.InDelta(t, 10.0, *q.FiveHour.TotalUSD, 0.001)
	require.Equal(t, 0.0, *q.FiveHour.RemainingUSD)
	require.Equal(t, 15.0, q.FiveHour.UsedUSD)
	require.Equal(t, 0, q.FiveHour.UnknownAccountsCount)

	// 7d window
	require.NotNil(t, q.SevenDay.TotalUSD)
	require.NotNil(t, q.SevenDay.RemainingUSD)
	require.InDelta(t, 10.0, *q.SevenDay.TotalUSD, 0.001)
	require.Equal(t, 0.0, *q.SevenDay.RemainingUSD)
	require.Equal(t, 15.0, q.SevenDay.UsedUSD)
	require.Equal(t, 0, q.SevenDay.UnknownAccountsCount)
}

// TestModelQuotaInference_NegativeRemainingClamp verifies remaining never goes negative
func TestModelQuotaInference_NegativeRemainingClamp(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 150.0
	accounts[0].Extra["codex_7d_used_percent"] = 150.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 80.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 80.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: remaining must be >= 0
	require.NotNil(t, q.FiveHour.TotalUSD)
	require.NotNil(t, q.FiveHour.RemainingUSD)
	require.GreaterOrEqual(t, *q.FiveHour.RemainingUSD, 0.0)
	require.LessOrEqual(t, *q.FiveHour.RemainingUSD, *q.FiveHour.TotalUSD)

	// 7d window
	require.NotNil(t, q.SevenDay.TotalUSD)
	require.NotNil(t, q.SevenDay.RemainingUSD)
	require.GreaterOrEqual(t, *q.SevenDay.RemainingUSD, 0.0)
	require.LessOrEqual(t, *q.SevenDay.RemainingUSD, *q.SevenDay.TotalUSD)
}



// TestModelQuotaInference_JSONNullForUnknown verifies JSON serialization of null values
func TestModelQuotaInference_JSONNullForUnknown(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
		accountForQuota(2, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 50.0
	accounts[0].Extra["codex_7d_used_percent"] = 50.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
			2: {Cost: 5.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
			2: {Cost: 5.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)

	body, err := json.Marshal(resp)
	require.NoError(t, err)
	jsonStr := string(body)

	require.Contains(t, jsonStr, `"total_usd":null`)
	require.Contains(t, jsonStr, `"remaining_usd":null`)
	require.NotContains(t, jsonStr, `"used_usd":null`)
	require.NotContains(t, jsonStr, `"used_usd":"null"`)
	require.NotContains(t, jsonStr, `"account_id"`)
	require.NotContains(t, jsonStr, `"account_name"`)
	require.NotContains(t, jsonStr, `"credentials"`)
	require.NotContains(t, jsonStr, `"proxy"`)
	require.NotContains(t, jsonStr, `"user"`)
	require.Contains(t, jsonStr, `"object":"list"`)
	require.Contains(t, jsonStr, `"object":"model_quota"`)
	require.Contains(t, jsonStr, `"quota_pool":"account_shared"`)
}

// TestModelQuotaInference_MixedKnownUnknown tests 2 known + 1 unknown accounts
func TestModelQuotaInference_MixedKnownUnknown(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
		accountForQuota(2, "openai", "m1"),
		accountForQuota(3, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 50.0
	accounts[0].Extra["codex_7d_used_percent"] = 50.0
	accounts[1].Extra["codex_5h_used_percent"] = 50.0
	accounts[1].Extra["codex_7d_used_percent"] = 50.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
			2: {Cost: 20.0},
			3: {Cost: 5.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
			2: {Cost: 20.0},
			3: {Cost: 5.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// Any unknown → null total/remaining
	require.Nil(t, q.FiveHour.TotalUSD)
	require.Nil(t, q.FiveHour.RemainingUSD)
	require.Equal(t, 35.0, q.FiveHour.UsedUSD)
	require.Equal(t, 3, q.FiveHour.AccountsCount)
	require.Equal(t, 1, q.FiveHour.UnknownAccountsCount)

	// 7d same
	require.Nil(t, q.SevenDay.TotalUSD)
	require.Nil(t, q.SevenDay.RemainingUSD)
	require.Equal(t, 35.0, q.SevenDay.UsedUSD)
	require.Equal(t, 3, q.SevenDay.AccountsCount)
	require.Equal(t, 1, q.SevenDay.UnknownAccountsCount)
}

// TestModelQuotaInference_IndependentWindows tests 5h known but 7d unknown for same account
func TestModelQuotaInference_IndependentWindows(t *testing.T) {
	accounts := []Account{
		accountForQuota(1, "openai", "m1"),
	}
	accounts[0].Extra["codex_5h_used_percent"] = 50.0

	usage := &modelQuotaUsageRepoStub{
		data5h: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
		data7d: map[int64]*usagestats.AccountStats{
			1: {Cost: 10.0},
		},
	}

	svc := newModelQuotaService(accounts, usage, time.Hour)
	resp := svc.GetModelQuotas(context.Background(), nil, "openai")

	require.NotNil(t, resp.Data)
	require.Len(t, resp.Data, 1)
	q := resp.Data[0]

	// 5h window: known (has percent)
	require.NotNil(t, q.FiveHour.TotalUSD)
	require.NotNil(t, q.FiveHour.RemainingUSD)
	require.InDelta(t, 20.0, *q.FiveHour.TotalUSD, 0.001)
	require.InDelta(t, 10.0, *q.FiveHour.RemainingUSD, 0.001)
	require.Equal(t, 10.0, q.FiveHour.UsedUSD)
	require.Equal(t, 1, q.FiveHour.AccountsCount)
	require.Equal(t, 0, q.FiveHour.UnknownAccountsCount)

	// 7d window: unknown (missing percent)
	require.Nil(t, q.SevenDay.TotalUSD)
	require.Nil(t, q.SevenDay.RemainingUSD)
	require.Equal(t, 10.0, q.SevenDay.UsedUSD)
	require.Equal(t, 1, q.SevenDay.AccountsCount)
	require.Equal(t, 1, q.SevenDay.UnknownAccountsCount)
}
