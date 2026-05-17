package service

import (
	"math"
	"sort"
)

func computeSaturation(window QuotaWindowSnapshot, params PotentialParameters) (float64, bool) {
	if window.State == WindowStateUnknown || window.State == WindowStateInvalid {
		return 0, false
	}
	if window.RemainingIsUnknown() {
		return 0, false
	}
	if window.Limit <= 0 {
		return 0, false
	}

	z := window.Used / window.Limit

	if math.IsNaN(z) || math.IsInf(z, 0) {
		return 0, false
	}
	if z < 0 {
		z = 0
	} else if z > 1 {
		z = 1
	}

	if window.IsStale(params.MaxStaleAge) {
		return z, false
	}

	return z, true
}

func computeColdIdle(snap AccountPotentialSnapshot, params PotentialParameters) float64 {
	var z5h, z7d float64
	var valid5h, valid7d bool

	if snap.Has5hWindow {
		z5h, valid5h = computeSaturation(snap.FiveHourWindow, params)
	}
	if snap.Has7dWindow {
		z7d, valid7d = computeSaturation(snap.SevenDayWindow, params)
	}

	idle5h := 1 - z5h
	idle7d := 1 - z7d

	switch {
	case valid5h && valid7d:
		return clamp(params.Lambda5h*idle5h+params.Lambda7d*idle7d, 0, 1)
	case valid5h:
		return clamp(idle5h, 0, 1)
	case valid7d:
		return clamp(idle7d, 0, 1)
	default:
		return 0
	}
}

func computePotentialDelta(snap AccountPotentialSnapshot, params PotentialParameters, demand float64) (float64, string) {
	var z5h, z7d float64
	var valid5h, valid7d bool

	if snap.Has5hWindow {
		z5h, valid5h = computeSaturation(snap.FiveHourWindow, params)
	}
	if snap.Has7dWindow {
		z7d, valid7d = computeSaturation(snap.SevenDayWindow, params)
	}

	if !valid5h && !valid7d {
		return 0, "unknown_state"
	}

	var zCombined float64
	switch {
	case valid5h && valid7d:
		zCombined = params.Lambda5h*z5h + params.Lambda7d*z7d
	case valid5h:
		zCombined = z5h
	case valid7d:
		zCombined = z7d
	}

	if math.IsNaN(zCombined) || math.IsInf(zCombined, 0) {
		return 0, "invalid_saturation"
	}
	if zCombined < 0 {
		zCombined = 0
	} else if zCombined > 1 {
		zCombined = 1
	}

	delta := demand * (params.Theta - zCombined)

	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0, "invalid_delta"
	}
	if delta < -10 {
		delta = -10
	} else if delta > 10 {
		delta = 10
	}

	return delta, ""
}

func computeSyncExhaustionPenalty(candidates []AccountPotentialSnapshot, params PotentialParameters) float64 {
	if len(candidates) == 0 || params.Zeta >= 1 || params.Zeta <= 0 {
		return 0
	}

	count := 0
	totalExcess := 0.0

	for _, snap := range candidates {
		var z float64
		var valid bool

		if snap.Has5hWindow {
			z, valid = computeSaturation(snap.FiveHourWindow, params)
		} else if snap.Has7dWindow {
			z, valid = computeSaturation(snap.SevenDayWindow, params)
		}

		if !valid {
			continue
		}

		if z > params.Zeta {
			count++
			excess := z - params.Zeta
			if excess > 0 {
				totalExcess += excess
			}
		}
	}

	if count == 0 {
		return 0
	}

	penalty := totalExcess * params.CappedSyncPenalty / float64(len(candidates))

	if penalty > params.CappedSyncPenalty {
		penalty = params.CappedSyncPenalty
	}
	if penalty < 0 {
		penalty = 0
	}

	return penalty
}

func isTie(scoreA, scoreB, epsilon float64) bool {
	if math.IsNaN(scoreA) || math.IsNaN(scoreB) {
		return false
	}
	return math.Abs(scoreA-scoreB) < epsilon
}

func priorityFactor(priority, maxPriority int) float64 {
	if priority < 1 {
		priority = 1
	}
	if maxPriority < priority {
		maxPriority = priority
	}
	if maxPriority <= 1 {
		return 1.0
	}
	f := float64(maxPriority - priority + 1)
	f /= float64(maxPriority)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return f
}

func scoreFromComponents(priorityFactorVal, delta, coldIdle, syncPenalty float64) float64 {
	base := priorityFactorVal

	deltaNorm := delta / (1 + math.Abs(delta))

	coldBoost := 0.25 * coldIdle

	score := base + 0.4*deltaNorm + coldBoost

	score = score * (1 - syncPenalty)

	if math.IsNaN(score) || math.IsInf(score, 0) {
		return 0.5
	}
	if score < 0 {
		score = 0
	} else if score > 1 {
		score = 1
	}
	return score
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ScorePotential(snap AccountPotentialSnapshot, params PotentialParameters, demand float64) PotentialScoreResult {
	if math.IsNaN(demand) || math.IsInf(demand, 0) || demand <= 0 {
		demand = 1
	}

	if !snap.HasValidWindows() {
		return UnknownScoreResult()
	}

	pf := priorityFactor(snap.Priority, 10)

	delta, deltaFallback := computePotentialDelta(snap, params, demand)

	if deltaFallback != "" {
		return PotentialScoreResult{
			Score:          0.5,
			Delta:          0,
			Valid:          false,
			FallbackReason: deltaFallback,
		}
	}

	coldIdle := computeColdIdle(snap, params)

	syncPenalty := 0.0

	score := scoreFromComponents(pf, delta, coldIdle, syncPenalty)

	valid := !math.IsNaN(score)

	return PotentialScoreResult{
		Score:          score,
		Delta:          delta,
		Valid:          valid,
		FallbackReason: deltaFallback,
	}
}

func RankByPotential(candidates []AccountPotentialSnapshot, params PotentialParameters, demand float64) []PotentialScoreResult {
	if len(candidates) == 0 {
		return nil
	}

	if math.IsNaN(demand) || math.IsInf(demand, 0) || demand <= 0 {
		demand = 1
	}

	syncPenalty := computeSyncExhaustionPenalty(candidates, params)

	results := make([]PotentialScoreResult, len(candidates))
	for i, snap := range candidates {
		if !snap.HasValidWindows() {
			results[i] = UnknownScoreResult()
			continue
		}

		pf := priorityFactor(snap.Priority, 10)

		delta, deltaFallback := computePotentialDelta(snap, params, demand)

		coldIdle := computeColdIdle(snap, params)

		score := scoreFromComponents(pf, delta, coldIdle, syncPenalty)

		valid := deltaFallback == "" && !math.IsNaN(score)

		results[i] = PotentialScoreResult{
			Score:          score,
			Delta:          delta,
			Valid:          valid,
			FallbackReason: deltaFallback,
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if !isTie(results[i].Score, results[j].Score, params.TieEpsilon) {
			return results[i].Score > results[j].Score
		}
		if !isTie(results[i].Delta, results[j].Delta, params.TieEpsilon) {
			return results[i].Delta > results[j].Delta
		}
		return i < j
	})

	return results
}
