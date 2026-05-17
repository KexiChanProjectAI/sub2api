package service

import (
	"testing"
	"time"
)

func TestMaintenanceHint_ColdUnderusedAccount(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	snap := AccountPotentialSnapshot{
		AccountID: 1,
		Has5hWindow: true,
		FiveHourWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       20.0,
			Remaining:  80.0,
			State:      WindowStateFresh,
			ObservedAt: now,
		},
	}

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}

	hint := hints[0]
	if hint.AccountID != 1 {
		t.Errorf("AccountID = %d, want 1", hint.AccountID)
	}
	if hint.SuggestedAction != "activate_cold" {
		t.Errorf("SuggestedAction = %s, want activate_cold", hint.SuggestedAction)
	}
	if hint.ScoreDelta <= 0 {
		t.Errorf("ScoreDelta = %f, want > 0", hint.ScoreDelta)
	}
	if hint.Urgency < 0 || hint.Urgency > 1 {
		t.Errorf("Urgency = %f, want between 0 and 1", hint.Urgency)
	}
}

func TestMaintenanceHint_AccountsNearOrAboveDangerThreshold(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	tests := []struct {
		name          string
		z             float64
		has5hWindow   bool
		wantReplenish bool
	}{
		{z: 0.85, has5hWindow: true, wantReplenish: true},
		{z: 0.89, has5hWindow: true, wantReplenish: true},
		{z: 0.90, has5hWindow: true, wantReplenish: false},
		{z: 0.95, has5hWindow: true, wantReplenish: false},
		{z: 1.0, has5hWindow: true, wantReplenish: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := AccountPotentialSnapshot{
				AccountID:   1,
				Has5hWindow: tt.has5hWindow,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      100.0,
					Used:       100.0 * tt.z,
					Remaining:  100.0 * (1 - tt.z),
					State:      WindowStateFresh,
					ObservedAt: now,
				},
			}

			hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

			if tt.wantReplenish {
				if len(hints) != 1 {
					t.Fatalf("expected 1 hint, got %d", len(hints))
				}
				if hints[0].SuggestedAction != "replenish_5h" {
					t.Errorf("SuggestedAction = %s, want replenish_5h", hints[0].SuggestedAction)
				}
			} else {
				if len(hints) != 0 {
					t.Fatalf("expected 0 hints, got %d", len(hints))
				}
			}
		})
	}
}

func TestMaintenanceHint_StaleDataAccount(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	snap := AccountPotentialSnapshot{
		AccountID:   1,
		Has5hWindow: true,
		FiveHourWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       50.0,
			Remaining:  50.0,
			State:      WindowStateFresh,
			ObservedAt: now.Add(-10 * time.Minute),
		},
	}

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}

	hint := hints[0]
	if hint.SuggestedAction != "refresh_stale" {
		t.Errorf("SuggestedAction = %s, want refresh_stale", hint.SuggestedAction)
	}
	if hint.Urgency <= 0 {
		t.Errorf("Urgency = %f, want > 0 for stale data", hint.Urgency)
	}
}

func TestMaintenanceHint_PureFunctionNoSideEffects(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	snap := AccountPotentialSnapshot{
		AccountID:   1,
		Has5hWindow: true,
		FiveHourWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       20.0,
			Remaining:  80.0,
			State:      WindowStateFresh,
			ObservedAt: now,
		},
	}

	candidates := []AccountPotentialSnapshot{snap}
	originalLen := len(candidates)

	ComputeMaintenanceHints(candidates, params)

	if len(candidates) != originalLen {
		t.Errorf("candidates slice was mutated: len = %d, want %d", len(candidates), originalLen)
	}

	if candidates[0].AccountID != snap.AccountID {
		t.Errorf("snapshot was mutated")
	}
}

func TestMaintenanceHint_NeverStartedAccount(t *testing.T) {
	params := DefaultPotentialParameters()

	snap := AccountPotentialSnapshot{
		AccountID:     1,
		Has5hWindow:   false,
		Has7dWindow:   false,
	}

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}

	hint := hints[0]
	if hint.SuggestedAction != "activate_cold" {
		t.Errorf("SuggestedAction = %s, want activate_cold", hint.SuggestedAction)
	}
	if hint.Reason != "never_started_no_windows" {
		t.Errorf("Reason = %s, want never_started_no_windows", hint.Reason)
	}
	if hint.Urgency != 0.5 {
		t.Errorf("Urgency = %f, want 0.5 for never-started", hint.Urgency)
	}
}

func TestMaintenanceHint_UrgencyProportionalToGap(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	tests := []struct {
		name           string
		used           float64
		minExpectedUrg float64
		maxExpectedUrg float64
	}{
		{used: 0.0, minExpectedUrg: 0.7, maxExpectedUrg: 1.0},
		{used: 20.0, minExpectedUrg: 0.5, maxExpectedUrg: 0.9},
		{used: 50.0, minExpectedUrg: 0.2, maxExpectedUrg: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := AccountPotentialSnapshot{
				AccountID:   1,
				Has5hWindow: true,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      100.0,
					Used:       tt.used,
					Remaining:  100.0 - tt.used,
					State:      WindowStateFresh,
					ObservedAt: now,
				},
			}

			hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

			if len(hints) != 1 {
				t.Fatalf("expected 1 hint, got %d", len(hints))
			}

			urgency := hints[0].Urgency
			if urgency < tt.minExpectedUrg || urgency > tt.maxExpectedUrg {
				t.Errorf("Urgency = %f, want between %f and %f",
					urgency, tt.minExpectedUrg, tt.maxExpectedUrg)
			}
		})
	}
}

func TestMaintenanceHint_7dWindow(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	snap := AccountPotentialSnapshot{
		AccountID:   1,
		Has5hWindow: false,
		Has7dWindow: true,
		SevenDayWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       85.0,
			Remaining:  15.0,
			State:      WindowStateFresh,
			ObservedAt: now,
		},
	}

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}

	hint := hints[0]
	if hint.SuggestedAction != "replenish_7d" {
		t.Errorf("SuggestedAction = %s, want replenish_7d", hint.SuggestedAction)
	}
}

func TestMaintenanceHint_EmptyCandidates(t *testing.T) {
	params := DefaultPotentialParameters()

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{}, params)

	if hints == nil {
		t.Error("hints should not be nil")
	}
	if len(hints) != 0 {
		t.Errorf("len(hints) = %d, want 0", len(hints))
	}
}

func TestMaintenanceHint_DangerThresholdExact(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	snap := AccountPotentialSnapshot{
		AccountID:   1,
		Has5hWindow: true,
		FiveHourWindow: QuotaWindowSnapshot{
			Limit:      100.0,
			Used:       90.0,
			Remaining:  10.0,
			State:      WindowStateFresh,
			ObservedAt: now,
		},
	}

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{snap}, params)

	if len(hints) != 0 {
		t.Errorf("at exactly zeta (%.2f), expected 0 hints, got %d", params.Zeta, len(hints))
	}
}
