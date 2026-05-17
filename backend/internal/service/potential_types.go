// Package service provides account scheduling with potential-based scoring.
//
// POTENTIAL SCORER — Implementation Notes (v1)
//
// OPT-IN MECHANISM
// Both openai_advanced_scheduler_enabled AND openai_potential_scheduler_enabled must
// be true for the potential scorer to activate. If either is false, the scheduler
// falls back to the legacy load-balanced selector.
//
// FAIL-OPEN BEHAVIOR
// When potential scoring encounters unknown/invalid data (no quota windows, stale data,
// NaN/Inf values), it returns Valid=false with a FallbackReason and the legacy scorer
// handles the selection. Unknown state → 0.5 score with unknown_state reason.
// Invalid saturation/delta → 0.0 score with invalid_* reason.
// This ensures the scheduler never fails due to potential scoring issues.
//
// V1 NON-GOALS
// - No persisted maintenance hints (hints are advisory only, recomputed each scheduling)
// - No maintenance execution (external/maintenance task is out of scope)
// - No admin UI for maintenance hints
// - No hard-bans on exhausted accounts (soft sync-exhaustion penalty only)
//
// SCORING FORMULA
// score = priorityFactor * (1 - syncPenalty) + 0.4*deltaNorm + 0.25*coldIdle
// where:
//   - priorityFactor: higher priority → higher factor (range [0,1])
//   - syncPenalty: shared penalty when many accounts are above Zeta threshold
//   - deltaNorm: normalized delta = delta / (1 + |delta|), clamped to [-10,10]
//   - coldIdle: 1 - saturation, blended across 5h and 7d windows
//   - demand: fixed at 1.0 (future: demand signal from upstream)
//
// STATE MODEL
// WindowStateUnknown  — snapshot has no quota data yet (e.g., new account)
// WindowStateFresh    — snapshot observed within MaxStaleAge (5 min)
// WindowStateStale    — snapshot observed > MaxStaleAge ago (used but de-rated)
// WindowStateInvalid  — snapshot has NaN/Inf/negative values
//
// A QuotaWindowSnapshot is "valid" for scoring when:
//   - State is Fresh (not Unknown/Invalid) AND
//   - RemainingIsUnknown() is false AND
//   - Limit > 0
//
// A candidate with no valid windows falls back via UnknownScoreResult().

package service

import (
	"fmt"
	"math"
	"time"
)

type WindowState int

const (
	WindowStateUnknown WindowState = iota
	WindowStateFresh
	WindowStateStale
	WindowStateInvalid
)

func (s WindowState) String() string {
	switch s {
	case WindowStateUnknown:
		return "unknown"
	case WindowStateFresh:
		return "fresh"
	case WindowStateStale:
		return "stale"
	case WindowStateInvalid:
		return "invalid"
	default:
		return fmt.Sprintf("WindowState(%d)", int(s))
	}
}

type QuotaWindowSnapshot struct {
	Limit       float64
	Used        float64
	Remaining   float64
	WindowStart time.Time
	WindowEnd   time.Time
	ResetAt     time.Time
	ObservedAt  time.Time
	Source      string
	State       WindowState

	remainingUnknown bool
}

func (w *QuotaWindowSnapshot) RemainingIsUnknown() bool {
	return w.remainingUnknown || w.State == WindowStateUnknown
}

func (w *QuotaWindowSnapshot) Validate() {
	if math.IsNaN(w.Limit) || math.IsInf(w.Limit, 0) ||
		math.IsNaN(w.Used) || math.IsInf(w.Used, 0) ||
		math.IsNaN(w.Remaining) || math.IsInf(w.Remaining, 0) {
		w.State = WindowStateInvalid
		return
	}
	if w.Limit < 0 || w.Used < 0 {
		w.State = WindowStateInvalid
		return
	}
	if w.State == WindowStateInvalid {
		return
	}
}

func (w *QuotaWindowSnapshot) IsStale(maxStaleAge time.Duration) bool {
	if w.State == WindowStateInvalid || w.State == WindowStateUnknown {
		return false
	}
	return time.Since(w.ObservedAt) > maxStaleAge
}

func (w *QuotaWindowSnapshot) IsFresh(maxStaleAge time.Duration) bool {
	return !w.IsStale(maxStaleAge) && w.State != WindowStateInvalid && w.State != WindowStateUnknown
}

type AccountPotentialSnapshot struct {
	AccountID     int64
	AccountName   string
	Platform      string
	Priority      int
	PlanType      string
	Status        string
	Schedulable   bool
	RateLimitAt   *time.Time
	OverloadUntil *time.Time
	LastUsedAt    *time.Time

	FiveHourWindow QuotaWindowSnapshot
	SevenDayWindow QuotaWindowSnapshot

	Has5hWindow bool
	Has7dWindow bool
}

func (a *AccountPotentialSnapshot) HasValidWindows() bool {
	return a.Has5hWindow || a.Has7dWindow
}

type PotentialParameters struct {
	Lambda5h          float64
	Lambda7d          float64
	Theta             float64
	Zeta              float64
	TieEpsilon        float64
	MaxStaleAge       time.Duration
	CappedSyncPenalty float64
}

func DefaultPotentialParameters() PotentialParameters {
	return PotentialParameters{
		Lambda5h:          0.5,
		Lambda7d:          0.5,
		Theta:             0.8,
		Zeta:              0.9,
		TieEpsilon:        1e-9,
		MaxStaleAge:       5 * time.Minute,
		CappedSyncPenalty: 0.1,
	}
}

type PotentialScoreResult struct {
	Score          float64
	Delta          float64
	Valid          bool
	FallbackReason string
}

func UnknownScoreResult() PotentialScoreResult {
	return PotentialScoreResult{
		Score:          0.5,
		Delta:          0.0,
		Valid:          false,
		FallbackReason: "unknown_state",
	}
}

func InvalidScoreResult(reason string) PotentialScoreResult {
	return PotentialScoreResult{
		Score:          0.0,
		Delta:          0.0,
		Valid:          false,
		FallbackReason: reason,
	}
}