package service

import (
	"math"
	"testing"
	"time"
)

func TestWindowStateString(t *testing.T) {
	tests := []struct {
		state    WindowState
		expected string
	}{
		{WindowStateUnknown, "unknown"},
		{WindowStateFresh, "fresh"},
		{WindowStateStale, "stale"},
		{WindowStateInvalid, "invalid"},
		{WindowState(99), "WindowState(99)"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.expected {
			t.Errorf("WindowState.String() = %v, want %v", got, tt.expected)
		}
	}
}

func TestQuotaWindowSnapshot_RemainingIsUnknown(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name              string
		window            QuotaWindowSnapshot
		expectedUnknown   bool
	}{
		{
			name: "normal window with remaining",
			window: QuotaWindowSnapshot{
				Remaining:  10.0,
				State:     WindowStateFresh,
				ObservedAt: now,
			},
			expectedUnknown: false,
		},
		{
			name: "window with remainingUnknown flag set",
			window: QuotaWindowSnapshot{
				Remaining:         10.0,
				State:             WindowStateFresh,
				ObservedAt:        now,
				remainingUnknown:  true,
			},
			expectedUnknown: true,
		},
		{
			name: "unknown state",
			window: QuotaWindowSnapshot{
				Remaining:  0.0,
				State:      WindowStateUnknown,
				ObservedAt: now,
			},
			expectedUnknown: true,
		},
		{
			name: "zero remaining is NOT unknown",
			window: QuotaWindowSnapshot{
				Remaining:  0.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedUnknown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.window.RemainingIsUnknown(); got != tt.expectedUnknown {
				t.Errorf("QuotaWindowSnapshot.RemainingIsUnknown() = %v, want %v", got, tt.expectedUnknown)
			}
		})
	}
}

func TestQuotaWindowSnapshot_Validate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		window        QuotaWindowSnapshot
		expectedState WindowState
	}{
		{
			name: "valid window",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateFresh,
		},
		{
			name: "NaN limit",
			window: QuotaWindowSnapshot{
				Limit:      math.NaN(),
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
		{
			name: "Inf limit",
			window: QuotaWindowSnapshot{
				Limit:      math.Inf(1),
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
		{
			name: "NaN used",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       math.NaN(),
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
		{
			name: "Inf remaining",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       50.0,
				Remaining:  math.Inf(1),
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
		{
			name: "negative limit",
			window: QuotaWindowSnapshot{
				Limit:      -10.0,
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
		{
			name: "already invalid state preserved",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateInvalid,
				ObservedAt: now,
			},
			expectedState: WindowStateInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.window.Validate()
			if tt.window.State != tt.expectedState {
				t.Errorf("QuotaWindowSnapshot.Validate() state = %v, want %v", tt.window.State, tt.expectedState)
			}
		})
	}
}

func TestQuotaWindowSnapshot_IsStale(t *testing.T) {
	maxStaleAge := 5 * time.Minute

	tests := []struct {
		name          string
		window        QuotaWindowSnapshot
		expectedStale bool
	}{
		{
			name: "fresh window",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-1 * time.Minute),
			},
			expectedStale: false,
		},
		{
			name: "stale window",
			window: QuotaWindowSnapshot{
				Limit:      100.0,
				Used:       50.0,
				Remaining:  50.0,
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-10 * time.Minute),
			},
			expectedStale: true,
		},
		{
			name: "unknown state is not stale",
			window: QuotaWindowSnapshot{
				State:      WindowStateUnknown,
				ObservedAt: time.Now().Add(-10 * time.Minute),
			},
			expectedStale: false,
		},
		{
			name: "invalid state is not stale",
			window: QuotaWindowSnapshot{
				State:      WindowStateInvalid,
				ObservedAt: time.Now().Add(-10 * time.Minute),
			},
			expectedStale: false,
		},
		{
			name: "window at 4 minutes is fresh",
			window: QuotaWindowSnapshot{
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-4 * time.Minute),
			},
			expectedStale: false,
		},
		{
			name: "window at 6 minutes is stale",
			window: QuotaWindowSnapshot{
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-6 * time.Minute),
			},
			expectedStale: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.window.IsStale(maxStaleAge); got != tt.expectedStale {
				t.Errorf("QuotaWindowSnapshot.IsStale() = %v, want %v", got, tt.expectedStale)
			}
		})
	}
}

func TestQuotaWindowSnapshot_IsFresh(t *testing.T) {
	maxStaleAge := 5 * time.Minute

	tests := []struct {
		name        string
		window      QuotaWindowSnapshot
		expectedFresh bool
	}{
		{
			name: "fresh window",
			window: QuotaWindowSnapshot{
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-1 * time.Minute),
			},
			expectedFresh: true,
		},
		{
			name: "stale window",
			window: QuotaWindowSnapshot{
				State:      WindowStateFresh,
				ObservedAt: time.Now().Add(-10 * time.Minute),
			},
			expectedFresh: false,
		},
		{
			name: "unknown state is not fresh",
			window: QuotaWindowSnapshot{
				State:      WindowStateUnknown,
				ObservedAt: time.Now(),
			},
			expectedFresh: false,
		},
		{
			name: "invalid state is not fresh",
			window: QuotaWindowSnapshot{
				State:      WindowStateInvalid,
				ObservedAt: time.Now(),
			},
			expectedFresh: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.window.IsFresh(maxStaleAge); got != tt.expectedFresh {
				t.Errorf("QuotaWindowSnapshot.IsFresh() = %v, want %v", got, tt.expectedFresh)
			}
		})
	}
}

func TestAccountPotentialSnapshot_HasValidWindows(t *testing.T) {
	tests := []struct {
		name          string
		snapshot      AccountPotentialSnapshot
		expectedValid bool
	}{
		{
			name: "has 5h window",
			snapshot: AccountPotentialSnapshot{
				Has5hWindow: true,
				Has7dWindow: false,
			},
			expectedValid: true,
		},
		{
			name: "has 7d window",
			snapshot: AccountPotentialSnapshot{
				Has5hWindow: false,
				Has7dWindow: true,
			},
			expectedValid: true,
		},
		{
			name: "has both windows",
			snapshot: AccountPotentialSnapshot{
				Has5hWindow: true,
				Has7dWindow: true,
			},
			expectedValid: true,
		},
		{
			name: "has no windows",
			snapshot: AccountPotentialSnapshot{
				Has5hWindow: false,
				Has7dWindow: false,
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.snapshot.HasValidWindows(); got != tt.expectedValid {
				t.Errorf("AccountPotentialSnapshot.HasValidWindows() = %v, want %v", got, tt.expectedValid)
			}
		})
	}
}

func TestDefaultPotentialParameters(t *testing.T) {
	params := DefaultPotentialParameters()

	if params.Lambda5h != 0.5 {
		t.Errorf("Lambda5h = %v, want 0.5", params.Lambda5h)
	}
	if params.Lambda7d != 0.5 {
		t.Errorf("Lambda7d = %v, want 0.5", params.Lambda7d)
	}
	if params.Theta != 0.8 {
		t.Errorf("Theta = %v, want 0.8", params.Theta)
	}
	if params.Zeta != 0.9 {
		t.Errorf("Zeta = %v, want 0.9", params.Zeta)
	}
	if params.TieEpsilon != 1e-9 {
		t.Errorf("TieEpsilon = %v, want 1e-9", params.TieEpsilon)
	}
	if params.MaxStaleAge != 5*time.Minute {
		t.Errorf("MaxStaleAge = %v, want 5m", params.MaxStaleAge)
	}
	if params.CappedSyncPenalty != 0.1 {
		t.Errorf("CappedSyncPenalty = %v, want 0.1", params.CappedSyncPenalty)
	}
}

func TestUnknownScoreResult(t *testing.T) {
	result := UnknownScoreResult()

	if result.Score != 0.5 {
		t.Errorf("Score = %v, want 0.5", result.Score)
	}
	if result.Delta != 0.0 {
		t.Errorf("Delta = %v, want 0.0", result.Delta)
	}
	if result.Valid != false {
		t.Errorf("Valid = %v, want false", result.Valid)
	}
	if result.FallbackReason != "unknown_state" {
		t.Errorf("FallbackReason = %v, want 'unknown_state'", result.FallbackReason)
	}
}

func TestInvalidScoreResult(t *testing.T) {
	result := InvalidScoreResult("test_reason")

	if result.Score != 0.0 {
		t.Errorf("Score = %v, want 0.0", result.Score)
	}
	if result.Delta != 0.0 {
		t.Errorf("Delta = %v, want 0.0", result.Delta)
	}
	if result.Valid != false {
		t.Errorf("Valid = %v, want false", result.Valid)
	}
	if result.FallbackReason != "test_reason" {
		t.Errorf("FallbackReason = %v, want 'test_reason'", result.FallbackReason)
	}
}

func TestQuotaWindowSnapshot_UnknownNotConvertedToZero(t *testing.T) {
	now := time.Now()

	window := QuotaWindowSnapshot{
		Limit:      100.0,
		Used:       0.0,
		Remaining:  0.0,
		State:      WindowStateUnknown,
		ObservedAt: now,
	}

	if window.RemainingIsUnknown() != true {
		t.Error("Window with Unknown state should return true for RemainingIsUnknown()")
	}

	window.remainingUnknown = true
	if window.Remaining != 0.0 {
		t.Errorf("Remaining should stay at 0.0, not converted to something else")
	}
	if window.RemainingIsUnknown() != true {
		t.Error("Window with remainingUnknown flag should return true for RemainingIsUnknown()")
	}
}

func TestQuotaWindowSnapshot_ZeroRemainingIsValid(t *testing.T) {
	now := time.Now()

	window := QuotaWindowSnapshot{
		Limit:      100.0,
		Used:       100.0,
		Remaining:  0.0,
		State:      WindowStateFresh,
		ObservedAt: now,
	}

	if window.RemainingIsUnknown() != false {
		t.Error("Window with zero remaining but Fresh state should NOT be unknown")
	}

	window.Validate()
	if window.State != WindowStateFresh {
		t.Errorf("Window with zero remaining should remain Fresh after validation, got %v", window.State)
	}
}

func TestQuotaWindowSnapshot_NegativeUsedIsInvalid(t *testing.T) {
	now := time.Now()

	window := QuotaWindowSnapshot{
		Limit:      100.0,
		Used:       -10.0,
		Remaining:  110.0,
		State:      WindowStateFresh,
		ObservedAt: now,
	}

	window.Validate()
	if window.State != WindowStateInvalid {
		t.Errorf("Window with negative Used should be Invalid, got %v", window.State)
	}
}

func TestAccountPotentialSnapshot_Construction(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)

	snapshot := AccountPotentialSnapshot{
		AccountID:    123,
		AccountName:  "test-account",
		Platform:     "openai",
		Priority:     10,
		PlanType:     "plus",
		Status:       "active",
		Schedulable:  true,
		RateLimitAt:  nil,
		OverloadUntil: nil,
		LastUsedAt:   &lastUsed,
		FiveHourWindow: QuotaWindowSnapshot{
			Limit:      10.0,
			Used:       5.0,
			Remaining:  5.0,
			State:      WindowStateFresh,
			ObservedAt: now,
			Source:     "telemetry",
		},
		SevenDayWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       50.0,
			Remaining:  50.0,
			State:      WindowStateFresh,
			ObservedAt: now,
			Source:     "telemetry",
		},
		Has5hWindow: true,
		Has7dWindow: true,
	}

	if snapshot.AccountID != 123 {
		t.Errorf("AccountID = %v, want 123", snapshot.AccountID)
	}
	if snapshot.HasValidWindows() != true {
		t.Error("HasValidWindows() should be true")
	}
	if snapshot.FiveHourWindow.Remaining != 5.0 {
		t.Errorf("FiveHourWindow.Remaining = %v, want 5.0", snapshot.FiveHourWindow.Remaining)
	}
	if snapshot.SevenDayWindow.RemainingIsUnknown() != false {
		t.Error("SevenDayWindow should not be unknown")
	}
}