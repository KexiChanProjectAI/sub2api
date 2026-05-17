package service

import (
	"math"
	"time"
)

type MaintenanceHint struct {
	AccountID       int64
	SuggestedAction string
	Reason          string
	ScoreDelta      float64
	Urgency         float64
}

func ComputeMaintenanceHints(candidates []AccountPotentialSnapshot, params PotentialParameters) []MaintenanceHint {
	if len(candidates) == 0 {
		return []MaintenanceHint{}
	}

	hints := make([]MaintenanceHint, 0)

	for _, snap := range candidates {
		hint := computeSingleMaintenanceHint(snap, params)
		if hint != nil {
			hints = append(hints, *hint)
		}
	}

	return hints
}

func computeSingleMaintenanceHint(snap AccountPotentialSnapshot, params PotentialParameters) *MaintenanceHint {
	if !snap.HasValidWindows() {
		return computeNeverStartedHint(snap, params)
	}

	// Check for stale windows first, regardless of computeSaturation validity
	// computeSaturation returns valid=false for stale windows, so we must
	// check IsStale() directly before reaching the hasValid check
	if snap.Has5hWindow && snap.FiveHourWindow.State != WindowStateUnknown && snap.FiveHourWindow.State != WindowStateInvalid && snap.FiveHourWindow.IsStale(params.MaxStaleAge) {
		return computeStaleHint(snap, snap.FiveHourWindow.ObservedAt, "5h_window_stale", params)
	}
	if snap.Has7dWindow && snap.SevenDayWindow.State != WindowStateUnknown && snap.SevenDayWindow.State != WindowStateInvalid && snap.SevenDayWindow.IsStale(params.MaxStaleAge) {
		return computeStaleHint(snap, snap.SevenDayWindow.ObservedAt, "7d_window_stale", params)
	}

	var z5h, z7d float64
	var valid5h, valid7d bool

	if snap.Has5hWindow {
		z5h, valid5h = computeSaturation(snap.FiveHourWindow, params)
	}
	if snap.Has7dWindow {
		z7d, valid7d = computeSaturation(snap.SevenDayWindow, params)
	}

	var zCombined float64
	var hasValid bool
	switch {
	case valid5h && valid7d:
		zCombined = params.Lambda5h*z5h + params.Lambda7d*z7d
		hasValid = true
	case valid5h:
		zCombined = z5h
		hasValid = true
	case valid7d:
		zCombined = z7d
		hasValid = true
	default:
		hasValid = false
	}

	if !hasValid {
		return computeNeverStartedHint(snap, params)
	}

	if zCombined >= params.Zeta {
		return nil
	}

	if zCombined < params.Theta {
		return computeColdHint(snap, zCombined, params)
	}

	return computeReplenishHint(snap, zCombined, params)
}

func computeNeverStartedHint(snap AccountPotentialSnapshot, params PotentialParameters) *MaintenanceHint {
	urgency := 0.5

	return &MaintenanceHint{
		AccountID:       snap.AccountID,
		SuggestedAction: "activate_cold",
		Reason:          "never_started_no_windows",
		ScoreDelta:      estimateColdActivationDelta(params),
		Urgency:         urgency,
	}
}

func computeColdHint(snap AccountPotentialSnapshot, z float64, params PotentialParameters) *MaintenanceHint {
	urgency := (params.Theta - z) / params.Theta
	if urgency < 0 {
		urgency = 0
	}
	if urgency > 1 {
		urgency = 1
	}

	delta := estimateColdActivationDelta(params)

	return &MaintenanceHint{
		AccountID:       snap.AccountID,
		SuggestedAction: "activate_cold",
		Reason:          "below_target_saturation",
		ScoreDelta:      delta,
		Urgency:         urgency,
	}
}

func computeReplenishHint(snap AccountPotentialSnapshot, z float64, params PotentialParameters) *MaintenanceHint {
	var action string
	if snap.Has5hWindow {
		action = "replenish_5h"
	} else if snap.Has7dWindow {
		action = "replenish_7d"
	} else {
		return nil
	}

	urgency := (z - params.Theta) / (params.Zeta - params.Theta)
	if urgency < 0 {
		urgency = 0
	}
	if urgency > 1 {
		urgency = 1
	}

	delta := estimateReplenishDelta(params, z)

	return &MaintenanceHint{
		AccountID:       snap.AccountID,
		SuggestedAction: action,
		Reason:          "near_danger_threshold",
		ScoreDelta:      delta,
		Urgency:         urgency,
	}
}

func computeStaleHint(snap AccountPotentialSnapshot, observedAt time.Time, reason string, params PotentialParameters) *MaintenanceHint {
	age := time.Since(observedAt)
	urgency := age.Seconds() / params.MaxStaleAge.Seconds()
	if urgency > 1 {
		urgency = 1
	}
	if urgency < 0 {
		urgency = 0
	}

	return &MaintenanceHint{
		AccountID:       snap.AccountID,
		SuggestedAction: "refresh_stale",
		Reason:          reason,
		ScoreDelta:      0,
		Urgency:         urgency,
	}
}

func estimateColdActivationDelta(params PotentialParameters) float64 {
	delta := params.Theta

	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0
	}
	if delta > 10 {
		delta = 10
	}
	if delta < -10 {
		delta = -10
	}

	return delta
}

func estimateReplenishDelta(params PotentialParameters, currentZ float64) float64 {
	delta := params.Theta - currentZ

	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0
	}
	if delta > 10 {
		delta = 10
	}
	if delta < -10 {
		delta = -10
	}

	return delta
}
