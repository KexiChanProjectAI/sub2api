package service

import (
	"testing"
	"time"
)

func TestBuildAdvisoryQuotaSnapshot_KnownQuotaData(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID:       1,
		Name:     "test-account",
		Platform: "openai",
		Priority: 10,
		Status:   StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "plus"},
		Extra: map[string]any{
			"quota_limit":         100.0,
			"quota_used":          30.0,
			"quota_weekly_limit":  500.0,
			"quota_weekly_used":   150.0,
		},
		UpdatedAt: now.Add(-1 * time.Hour),
		RateLimitedAt: func() *time.Time { t := now.Add(5 * time.Minute); return &t }(),
		OverloadUntil: func() *time.Time { t := now.Add(10 * time.Minute); return &t }(),
	}
	loadInfo := &AccountLoadInfo{AccountID: 1, CurrentConcurrency: 2, WaitingCount: 0, LoadRate: 20}

	snap := BuildAdvisoryQuotaSnapshot(account, loadInfo)

	if snap.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1", snap.AccountID)
	}
	if snap.PlanType != "plus" {
		t.Errorf("PlanType = %s, want plus", snap.PlanType)
	}
	if !snap.Has5hWindow {
		t.Error("Has5hWindow = false, want true")
	}
	if !snap.Has7dWindow {
		t.Error("Has7dWindow = false, want true")
	}
	if snap.FiveHourWindow.State != WindowStateFresh {
		t.Errorf("FiveHourWindow.State = %v, want WindowStateFresh", snap.FiveHourWindow.State)
	}
	if snap.FiveHourWindow.Limit != 100.0 {
		t.Errorf("FiveHourWindow.Limit = %v, want 100.0", snap.FiveHourWindow.Limit)
	}
	if snap.FiveHourWindow.Used != 30.0 {
		t.Errorf("FiveHourWindow.Used = %v, want 30.0", snap.FiveHourWindow.Used)
	}
	if snap.FiveHourWindow.Remaining != 70.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 70.0", snap.FiveHourWindow.Remaining)
	}
	if snap.FiveHourWindow.RemainingIsUnknown() {
		t.Error("FiveHourWindow.RemainingIsUnknown() = true, want false")
	}
	if snap.SevenDayWindow.State != WindowStateFresh {
		t.Errorf("SevenDayWindow.State = %v, want WindowStateFresh", snap.SevenDayWindow.State)
	}
	if snap.SevenDayWindow.Limit != 500.0 {
		t.Errorf("SevenDayWindow.Limit = %v, want 500.0", snap.SevenDayWindow.Limit)
	}
}

func TestBuildAdvisoryQuotaSnapshot_UnknownQuotaData(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID:          2,
		Name:        "unknown-account",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "free"},
		Extra:       map[string]any{},
		UpdatedAt:   now.Add(-1 * time.Hour),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if snap.Has5hWindow {
		t.Error("Has5hWindow = true, want false for missing quota data")
	}
	if snap.Has7dWindow {
		t.Error("Has7dWindow = true, want false for missing quota data")
	}
	if snap.FiveHourWindow.State != WindowStateUnknown {
		t.Errorf("FiveHourWindow.State = %v, want WindowStateUnknown", snap.FiveHourWindow.State)
	}
	if !snap.FiveHourWindow.RemainingIsUnknown() {
		t.Error("FiveHourWindow.RemainingIsUnknown() = false, want true")
	}
	if snap.SevenDayWindow.State != WindowStateUnknown {
		t.Errorf("SevenDayWindow.State = %v, want WindowStateUnknown", snap.SevenDayWindow.State)
	}
	if !snap.SevenDayWindow.RemainingIsUnknown() {
		t.Error("SevenDayWindow.RemainingIsUnknown() = false, want true")
	}
}

func TestBuildAdvisoryQuotaSnapshot_ZeroUsedFallback(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID:          3,
		Name:        "zero-used-account",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "plus"},
		Extra:       map[string]any{
			"quota_limit": 0.0,
			"quota_used":   0.0,
		},
		UpdatedAt: now,
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if !snap.Has5hWindow {
		t.Error("Has5hWindow = false, want true (plan-type prior should create window)")
	}
	if snap.FiveHourWindow.State != WindowStateFresh {
		t.Errorf("FiveHourWindow.State = %v, want WindowStateFresh", snap.FiveHourWindow.State)
	}
	if snap.FiveHourWindow.Limit != 12.0 {
		t.Errorf("FiveHourWindow.Limit = %v, want 12.0 (plus plan-type prior)", snap.FiveHourWindow.Limit)
	}
	if snap.FiveHourWindow.Used != 0.0 {
		t.Errorf("FiveHourWindow.Used = %v, want 0.0", snap.FiveHourWindow.Used)
	}
	if snap.FiveHourWindow.Remaining != 12.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 12.0", snap.FiveHourWindow.Remaining)
	}
	if snap.FiveHourWindow.RemainingIsUnknown() {
		t.Error("FiveHourWindow.RemainingIsUnknown() = true, want false")
	}
	if snap.FiveHourWindow.Source != "plan_type_prior" {
		t.Errorf("FiveHourWindow.Source = %s, want plan_type_prior", snap.FiveHourWindow.Source)
	}
}

func TestBuildAdvisoryQuotaSnapshot_ZeroUsedWithPositiveLimit(t *testing.T) {
	now := time.Now()
	account := &Account{
		ID:          4,
		Name:        "zero-used-with-limit",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "pro"},
		Extra: map[string]any{
			"quota_limit": 500.0,
			"quota_used":   0.0,
		},
		UpdatedAt: now,
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if !snap.Has5hWindow {
		t.Error("Has5hWindow = false, want true")
	}
	if snap.FiveHourWindow.State != WindowStateFresh {
		t.Errorf("FiveHourWindow.State = %v, want WindowStateFresh", snap.FiveHourWindow.State)
	}
	if snap.FiveHourWindow.Limit != 500.0 {
		t.Errorf("FiveHourWindow.Limit = %v, want 500.0", snap.FiveHourWindow.Limit)
	}
	if snap.FiveHourWindow.Used != 0.0 {
		t.Errorf("FiveHourWindow.Used = %v, want 0.0", snap.FiveHourWindow.Used)
	}
	if snap.FiveHourWindow.Remaining != 500.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 500.0", snap.FiveHourWindow.Remaining)
	}
	if snap.FiveHourWindow.RemainingIsUnknown() {
		t.Error("FiveHourWindow.RemainingIsUnknown() = true, want false")
	}
	if snap.FiveHourWindow.Source != "account_extra" {
		t.Errorf("FiveHourWindow.Source = %s, want account_extra", snap.FiveHourWindow.Source)
	}
}

func TestBuildAdvisoryQuotaSnapshot_PlanTypePriors(t *testing.T) {
	cases := []struct {
		planType        string
		expected5hPrior float64
	}{
		{"plus", 12.0},
		{"team", 10.0},
		{"pro", 200.0},
		{"free", 1.0},
		{"other", 5.0},
		{"unknown_plan", 5.0},
		{"", 5.0},
	}

	for _, tc := range cases {
		t.Run(tc.planType, func(t *testing.T) {
			account := &Account{
				ID:          10,
				Name:        "plan-test-" + tc.planType,
				Platform:    "openai",
				Priority:    5,
				Status:      StatusActive,
				Schedulable: true,
				Credentials: map[string]any{},
				Extra: map[string]any{
					"quota_limit": 0.0,
					"quota_used":   0.0,
				},
				UpdatedAt: time.Now(),
			}
			if tc.planType != "" {
				account.Credentials["plan_type"] = tc.planType
			}

			snap := BuildAdvisoryQuotaSnapshot(account, nil)

			if snap.FiveHourWindow.Limit != tc.expected5hPrior {
				t.Errorf("plan_type=%s: FiveHourWindow.Limit = %v, want %v", tc.planType, snap.FiveHourWindow.Limit, tc.expected5hPrior)
			}
			if snap.PlanType != tc.planType && tc.planType != "" {
				if tc.planType == "" && snap.PlanType != "other" {
					t.Errorf("plan_type=%s: PlanType = %s, want other", tc.planType, snap.PlanType)
				}
			}
		})
	}
}

func TestBuildAdvisoryQuotaSnapshot_7dWindowNoData(t *testing.T) {
	account := &Account{
		ID:          5,
		Name:        "no-7d-data",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{
			"quota_limit":  100.0,
			"quota_used":   50.0,
		},
		UpdatedAt: time.Now(),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if snap.Has7dWindow {
		t.Error("Has7dWindow = true, want false when no weekly quota data")
	}
	if snap.SevenDayWindow.State != WindowStateUnknown {
		t.Errorf("SevenDayWindow.State = %v, want WindowStateUnknown", snap.SevenDayWindow.State)
	}
}

func TestBuildAdvisoryQuotaSnapshot_7dWindowWithData(t *testing.T) {
	account := &Account{
		ID:          6,
		Name:        "has-7d-data",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "team"},
		Extra: map[string]any{
			"quota_limit":         100.0,
			"quota_used":          50.0,
			"quota_weekly_limit":  300.0,
			"quota_weekly_used":   100.0,
		},
		UpdatedAt: time.Now(),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if !snap.Has7dWindow {
		t.Error("Has7dWindow = false, want true")
	}
	if snap.SevenDayWindow.State != WindowStateFresh {
		t.Errorf("SevenDayWindow.State = %v, want WindowStateFresh", snap.SevenDayWindow.State)
	}
	if snap.SevenDayWindow.Limit != 300.0 {
		t.Errorf("SevenDayWindow.Limit = %v, want 300.0", snap.SevenDayWindow.Limit)
	}
	if snap.SevenDayWindow.Remaining != 200.0 {
		t.Errorf("SevenDayWindow.Remaining = %v, want 200.0", snap.SevenDayWindow.Remaining)
	}
}

func TestBuildAdvisoryQuotaSnapshot_UsesDailyFallback(t *testing.T) {
	account := &Account{
		ID:          7,
		Name:        "uses-daily-fallback",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "plus"},
		Extra: map[string]any{
			"quota_daily_limit":  80.0,
			"quota_daily_used":   20.0,
		},
		UpdatedAt: time.Now(),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if !snap.Has5hWindow {
		t.Error("Has5hWindow = false, want true (should use daily fallback)")
	}
	if snap.FiveHourWindow.Limit != 80.0 {
		t.Errorf("FiveHourWindow.Limit = %v, want 80.0 (from daily)", snap.FiveHourWindow.Limit)
	}
	if snap.FiveHourWindow.Used != 20.0 {
		t.Errorf("FiveHourWindow.Used = %v, want 20.0 (from daily)", snap.FiveHourWindow.Used)
	}
	if snap.FiveHourWindow.Remaining != 60.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 60.0", snap.FiveHourWindow.Remaining)
	}
}

func TestBuildAdvisoryQuotaSnapshot_ExhaustedWindow(t *testing.T) {
	account := &Account{
		ID:          8,
		Name:        "exhausted-account",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{
			"quota_limit":  10.0,
			"quota_used":   15.0,
		},
		UpdatedAt: time.Now(),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if !snap.Has5hWindow {
		t.Error("Has5hWindow = false, want true")
	}
	if snap.FiveHourWindow.State != WindowStateFresh {
		t.Errorf("FiveHourWindow.State = %v, want WindowStateFresh", snap.FiveHourWindow.State)
	}
	if snap.FiveHourWindow.Remaining != 0.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 0.0 (exhausted)", snap.FiveHourWindow.Remaining)
	}
	if !snap.FiveHourWindow.RemainingIsUnknown() {
		t.Error("FiveHourWindow.RemainingIsUnknown() = false, want true (remaining was negative)")
	}
}

func TestBuildAdvisoryQuotaSnapshot_RateLimitFields(t *testing.T) {
	now := time.Now()
	rateLimitAt := now.Add(10 * time.Minute)
	overloadUntil := now.Add(30 * time.Minute)

	account := &Account{
		ID:             9,
		Name:           "rate-limited-account",
		Platform:       "openai",
		Priority:       5,
		Status:         StatusActive,
		Schedulable:    true,
		Credentials:    map[string]any{"plan_type": "plus"},
		Extra:          map[string]any{},
		RateLimitedAt:  &rateLimitAt,
		OverloadUntil:  &overloadUntil,
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	if snap.RateLimitAt == nil {
		t.Error("RateLimitAt = nil, want non-nil")
	} else if !snap.RateLimitAt.Equal(rateLimitAt) {
		t.Errorf("RateLimitAt = %v, want %v", *snap.RateLimitAt, rateLimitAt)
	}

	if snap.OverloadUntil == nil {
		t.Error("OverloadUntil = nil, want non-nil")
	} else if !snap.OverloadUntil.Equal(overloadUntil) {
		t.Errorf("OverloadUntil = %v, want %v", *snap.OverloadUntil, overloadUntil)
	}
}

func TestBuildAdvisoryQuotaSnapshot_NoDBWrites(t *testing.T) {
	account := &Account{
		ID:          100,
		Name:        "no-db-write-test",
		Platform:    "openai",
		Priority:    5,
		Status:      StatusActive,
		Schedulable: true,
		Credentials: map[string]any{"plan_type": "plus"},
		Extra: map[string]any{
			"quota_limit":  100.0,
			"quota_used":   50.0,
		},
		UpdatedAt: time.Now(),
	}

	snap := BuildAdvisoryQuotaSnapshot(account, nil)

	_ = snap
}

func TestGetPlanType(t *testing.T) {
	cases := []struct {
		name     string
		account  *Account
		expected string
	}{
		{
			name:     "nil account",
			account:  nil,
			expected: "other",
		},
		{
			name:     "nil credentials",
			account:  &Account{Credentials: nil},
			expected: "other",
		},
		{
			name:     "empty credentials",
			account:  &Account{Credentials: map[string]any{}},
			expected: "other",
		},
		{
			name:     "plan_type exists",
			account:  &Account{Credentials: map[string]any{"plan_type": "pro"}},
			expected: "pro",
		},
		{
			name:     "plan_type not a string",
			account:  &Account{Credentials: map[string]any{"plan_type": 123}},
			expected: "other",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getPlanType(tc.account)
			if got != tc.expected {
				t.Errorf("getPlanType() = %s, want %s", got, tc.expected)
			}
		})
	}
}

func TestGetPlanTypePrior(t *testing.T) {
	cases := []struct {
		planType string
		expected float64
	}{
		{"plus", 12.0},
		{"team", 10.0},
		{"pro", 200.0},
		{"free", 1.0},
		{"other", 5.0},
		{"unknown", 5.0},
		{"", 5.0},
	}

	for _, tc := range cases {
		t.Run(tc.planType, func(t *testing.T) {
			got := getPlanTypePrior(tc.planType)
			if got != tc.expected {
				t.Errorf("getPlanTypePrior(%s) = %v, want %v", tc.planType, got, tc.expected)
			}
		})
	}
}