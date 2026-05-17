package service

import (
	"math"
	"testing"
	"time"
)

// buildPotentialBenchmarkCandidates creates candidate sets with mixed states:
// 70% fresh, 20% stale, 10% unknown
// Varied saturation levels: some underused (~20%), some near danger (~70%), some exhausted (~95%)
func buildPotentialBenchmarkCandidates(size int) []AccountPotentialSnapshot {
	if size <= 0 {
		return nil
	}

	candidates := make([]AccountPotentialSnapshot, 0, size)
	now := time.Now()

	for i := 0; i < size; i++ {
		accountID := int64(10_000 + i)

		// Determine state based on position: 70% fresh, 20% stale, 10% unknown
		var state5h, state7d WindowState
		var staleAge5h, staleAge7d time.Duration

		idx := i % 10
		if idx < 7 {
			// 70% fresh
			state5h = WindowStateFresh
			state7d = WindowStateFresh
			staleAge5h = 0
			staleAge7d = 0
		} else if idx < 9 {
			// 20% stale (indices 7, 8)
			state5h = WindowStateStale
			state7d = WindowStateFresh
			staleAge5h = 10 * time.Minute // past MaxStaleAge of 5m
			staleAge7d = 0
		} else {
			// 10% unknown (index 9)
			state5h = WindowStateUnknown
			state7d = WindowStateUnknown
			staleAge5h = 0
			staleAge7d = 0
		}

		// Varied saturation levels based on i % 3:
		// 0: underused (~20% used)
		// 1: near danger (~70% used)
		// 2: exhausted (~95% used)
		var used5h, limit5h, used7d, limit7d float64

		switch i % 3 {
		case 0:
			// Underused - 20% saturation
			limit5h = 100
			used5h = 20
			limit7d = 1000
			used7d = 200
		case 1:
			// Near danger - 70% saturation
			limit5h = 100
			used5h = 70
			limit7d = 1000
			used7d = 700
		case 2:
			// Exhausted - 95% saturation
			limit5h = 100
			used5h = 95
			limit7d = 1000
			used7d = 950
		}

		snap := AccountPotentialSnapshot{
			AccountID:   accountID,
			AccountName: "bench-account",
			Platform:    "openai",
			Priority:    i % 7,
			Status:      "active",
			Schedulable: true,

			FiveHourWindow: QuotaWindowSnapshot{
				Limit:       limit5h,
				Used:        used5h,
				Remaining:   limit5h - used5h,
				WindowStart: now.Add(-5 * time.Hour),
				WindowEnd:   now,
				ObservedAt:  now.Add(-staleAge5h),
				State:       state5h,
			},
			SevenDayWindow: QuotaWindowSnapshot{
				Limit:       limit7d,
				Used:        used7d,
				Remaining:   limit7d - used7d,
				WindowStart: now.Add(-7 * 24 * time.Hour),
				WindowEnd:   now,
				ObservedAt:  now.Add(-staleAge7d),
				State:       state7d,
			},

			Has5hWindow: state5h != WindowStateUnknown,
			Has7dWindow: state7d != WindowStateUnknown,
		}

		candidates = append(candidates, snap)
	}

	return candidates
}

// buildPotentialBenchmarkAccount creates a realistic Account for snapshot building benchmarks.
func buildPotentialBenchmarkAccount() *Account {
	return &Account{
		ID:           12345,
		Name:         "benchmark-account",
		Platform:     "openai",
		Type:         "api",
		Priority:     3,
		Status:       "active",
		LastUsedAt:   func() *time.Time { t := time.Now().Add(-30 * time.Minute); return &t }(),
		RateLimitedAt: nil,
		OverloadUntil: nil,
		Credentials: map[string]interface{}{
			"plan_type": "plus",
			"api_key":   "sk-benchmark-xxxxx",
		},
		Extra: map[string]interface{}{
			"quota_limit":      150.0,
			"quota_used":       87.0,
			"quota_daily_limit": 200.0,
			"quota_daily_used":  120.0,
			"quota_30d_limit":   1000.0,
			"quota_30d_used":    450.0,
		},
	}
}

// buildPotentialBenchmarkLoadInfo creates a realistic AccountLoadInfo for snapshot building.
func buildPotentialBenchmarkLoadInfo() *AccountLoadInfo {
	return &AccountLoadInfo{
		AccountID:    12345,
		LoadRate:     58,
		WaitingCount: 2,
	}
}

func BenchmarkPotentialScore(b *testing.B) {
	params := DefaultPotentialParameters()
	demand := 1.0

	cases := []struct {
		name string
		size int
	}{
		{name: "10", size: 10},
		{name: "100", size: 100},
		{name: "1000", size: 1000},
	}

	for _, tc := range cases {
		candidates := buildPotentialBenchmarkCandidates(tc.size)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result := ScorePotential(candidates[i%tc.size], params, demand)
				if math.IsNaN(result.Score) {
					b.Fatal("unexpected NaN score")
				}
			}
		})
	}
}

func BenchmarkPotentialRank(b *testing.B) {
	params := DefaultPotentialParameters()
	demand := 1.0

	cases := []struct {
		name string
		size int
	}{
		{name: "10", size: 10},
		{name: "100", size: 100},
		{name: "1000", size: 1000},
	}

	for _, tc := range cases {
		candidates := buildPotentialBenchmarkCandidates(tc.size)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				results := RankByPotential(candidates, params, demand)
				if len(results) == 0 {
					b.Fatal("unexpected empty results")
				}
			}
		})
	}
}

func BenchmarkPotentialMaintenanceHints(b *testing.B) {
	params := DefaultPotentialParameters()

	cases := []struct {
		name string
		size int
	}{
		{name: "10", size: 10},
		{name: "100", size: 100},
		{name: "1000", size: 1000},
	}

	for _, tc := range cases {
		candidates := buildPotentialBenchmarkCandidates(tc.size)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				hints := ComputeMaintenanceHints(candidates, params)
				if len(hints) == 0 {
					b.Fatal("unexpected empty hints")
				}
			}
		})
	}
}

func BenchmarkBuildAdvisoryQuotaSnapshot(b *testing.B) {
	account := buildPotentialBenchmarkAccount()
	loadInfo := buildPotentialBenchmarkLoadInfo()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		snap := BuildAdvisoryQuotaSnapshot(account, loadInfo)
		if snap.AccountID == 0 {
			b.Fatal("unexpected empty snapshot")
		}
	}
}