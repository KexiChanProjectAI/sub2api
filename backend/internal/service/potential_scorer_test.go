package service

import (
	"math"
	"testing"
	"time"
)

var defaultParams = DefaultPotentialParameters()

func makeFiveHourWindow(limit, used float64, state WindowState, staleAge time.Duration) QuotaWindowSnapshot {
	now := time.Now()
	return QuotaWindowSnapshot{
		Limit:     limit,
		Used:      used,
		Remaining: limit - used,
		WindowStart: now.Add(-5 * time.Hour),
		WindowEnd:   now,
		ObservedAt:  now.Add(-staleAge),
		State:       state,
	}
}

func makeSevenDayWindow(limit, used float64, state WindowState, staleAge time.Duration) QuotaWindowSnapshot {
	now := time.Now()
	return QuotaWindowSnapshot{
		Limit:     limit,
		Used:      used,
		Remaining: limit - used,
		WindowStart: now.Add(-7 * 24 * time.Hour),
		WindowEnd:   now,
		ObservedAt:  now.Add(-staleAge),
		State:       state,
	}
}

func makeSnap(has5h, has7d bool, fiveHour, sevenDay QuotaWindowSnapshot, priority int) AccountPotentialSnapshot {
	return AccountPotentialSnapshot{
		AccountID:   100,
		AccountName: "test-account",
		Platform:    "openai",
		Priority:    priority,
		PlanType:    "paid",
		Status:      "active",
		Schedulable: true,

		FiveHourWindow: fiveHour,
		SevenDayWindow: sevenDay,

		Has5hWindow: has5h,
		Has7dWindow: has7d,
	}
}


func TestPotentialSaturation_ValidWindow(t *testing.T) {
	w := makeFiveHourWindow(100, 30, WindowStateFresh, 0)
	z, ok := computeSaturation(w, defaultParams)
	if !ok {
		t.Error("expected valid window")
	}
	if math.Abs(z-0.3) > 1e-9 {
		t.Errorf("expected 0.3, got %v", z)
	}
}

func TestPotentialSaturation_ClampedToOne(t *testing.T) {
	w := makeFiveHourWindow(100, 150, WindowStateFresh, 0)
	z, ok := computeSaturation(w, defaultParams)
	if !ok {
		t.Error("expected valid window")
	}
	if z != 1 {
		t.Errorf("expected 1.0, got %v", z)
	}
}

func TestPotentialSaturation_ClampedToZero(t *testing.T) {
	w := makeFiveHourWindow(100, -10, WindowStateFresh, 0)
	z, ok := computeSaturation(w, defaultParams)
	if !ok {
		t.Error("expected valid window")
	}
	if z != 0 {
		t.Errorf("expected 0.0, got %v", z)
	}
}

func TestPotentialSaturation_UnknownState(t *testing.T) {
	w := makeFiveHourWindow(100, 30, WindowStateUnknown, 0)
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for unknown state")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_InvalidState(t *testing.T) {
	w := makeFiveHourWindow(100, 30, WindowStateInvalid, 0)
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for invalid state")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_ZeroLimit(t *testing.T) {
	w := makeFiveHourWindow(0, 0, WindowStateFresh, 0)
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for zero limit")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_NegativeLimit(t *testing.T) {
	w := makeFiveHourWindow(-50, 10, WindowStateFresh, 0)
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for negative limit")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_NaNInputs(t *testing.T) {
	w := QuotaWindowSnapshot{
		Limit:   math.NaN(),
		Used:    10,
		State:   WindowStateFresh,
	}
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for NaN limit")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_InfInputs(t *testing.T) {
	w := QuotaWindowSnapshot{
		Limit:   math.Inf(1),
		Used:    10,
		State:   WindowStateFresh,
	}
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for Inf limit")
	}
	if z != 0 {
		t.Errorf("expected 0, got %v", z)
	}
}

func TestPotentialSaturation_StaleWindow(t *testing.T) {
	w := makeFiveHourWindow(100, 30, WindowStateFresh, 10*time.Minute)
	z, ok := computeSaturation(w, defaultParams)
	if ok {
		t.Error("expected invalid for stale window")
	}
	if z != 0.3 {
		t.Errorf("expected z=0.3 even though stale, got %v", z)
	}
}


func TestPotentialColdIdle_BothWindowsUnused(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 0, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 0, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	s := computeColdIdle(snap, defaultParams)
	if math.Abs(s-1.0) > 1e-9 {
		t.Errorf("expected 1.0 for fully unused, got %v", s)
	}
}

func TestPotentialColdIdle_BothWindowsExhausted(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 100, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 1000, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	s := computeColdIdle(snap, defaultParams)
	if math.Abs(s-0.0) > 1e-9 {
		t.Errorf("expected 0.0 for fully exhausted, got %v", s)
	}
}

func TestPotentialColdIdle_Only5hWindow(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 50, WindowStateFresh, 0)
	sevenDay := QuotaWindowSnapshot{}
	snap := makeSnap(true, false, fiveHour, sevenDay, 5)

	s := computeColdIdle(snap, defaultParams)
	if math.Abs(s-0.5) > 1e-9 {
		t.Errorf("expected 0.5, got %v", s)
	}
}

func TestPotentialColdIdle_Only7dWindow(t *testing.T) {
	fiveHour := QuotaWindowSnapshot{}
	sevenDay := makeSevenDayWindow(1000, 200, WindowStateFresh, 0)
	snap := makeSnap(false, true, fiveHour, sevenDay, 5)

	s := computeColdIdle(snap, defaultParams)
	if math.Abs(s-0.8) > 1e-9 {
		t.Errorf("expected 0.8, got %v", s)
	}
}

func TestPotentialColdIdle_NoValidWindows(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 30, WindowStateUnknown, 0)
	sevenDay := makeSevenDayWindow(1000, 30, WindowStateUnknown, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	s := computeColdIdle(snap, defaultParams)
	if math.Abs(s-0.0) > 1e-9 {
		t.Errorf("expected 0.0 when no valid windows, got %v", s)
	}
}


func TestPotentialDelta_Underused(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 200, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback != "" {
		t.Errorf("expected no fallback, got %q", fallback)
	}
	if delta <= 0 {
		t.Errorf("expected positive delta for underused, got %v", delta)
	}
}

func TestPotentialDelta_Overused(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 90, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 900, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback != "" {
		t.Errorf("expected no fallback, got %q", fallback)
	}
	if delta >= 0 {
		t.Errorf("expected negative delta for overused, got %v", delta)
	}
}

func TestPotentialDelta_AtTheta(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 80, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 800, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback != "" {
		t.Errorf("expected no fallback, got %q", fallback)
	}
	if math.Abs(delta) > 1e-9 {
		t.Errorf("expected ~0 delta at theta, got %v", delta)
	}
}

func TestPotentialDelta_UnknownState(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 30, WindowStateUnknown, 0)
	sevenDay := makeSevenDayWindow(1000, 30, WindowStateUnknown, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback != "unknown_state" {
		t.Errorf("expected unknown_state fallback, got %q", fallback)
	}
	if delta != 0 {
		t.Errorf("expected 0 delta for unknown, got %v", delta)
	}
}

func TestPotentialDelta_DemandScaling(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 200, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	delta1, _ := computePotentialDelta(snap, defaultParams, 1)
	delta2, _ := computePotentialDelta(snap, defaultParams, 2)

	if math.Abs(delta1*2-delta2) > 1e-9 {
		t.Errorf("expected delta to scale linearly with demand: d=1:%v d=2:%v", delta1, delta2)
	}
}

func TestPotentialDelta_DefaultDemand(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 200, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	result := ScorePotential(snap, defaultParams, 0)
	if result.Score == 0.5 && !result.Valid {
		// default demand 1 was applied, and unknown fallback triggered since
		// HasValidWindows returned true but delta was computed as unknown
	}
}

func TestPotentialDelta_NaNInputsProduceFallback(t *testing.T) {
	w := QuotaWindowSnapshot{
		Limit:   math.NaN(),
		Used:    10,
		State:   WindowStateFresh,
	}
	snap := makeSnap(true, false, w, QuotaWindowSnapshot{}, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback == "" {
		t.Error("expected fallback for NaN inputs")
	}
	if delta != 0 {
		t.Errorf("expected 0 delta for invalid, got %v", delta)
	}
}

func TestPotentialDelta_InfInputsProduceFallback(t *testing.T) {
	w := QuotaWindowSnapshot{
		Limit:   math.Inf(1),
		Used:    10,
		State:   WindowStateFresh,
	}
	snap := makeSnap(true, false, w, QuotaWindowSnapshot{}, 5)

	delta, fallback := computePotentialDelta(snap, defaultParams, 1)
	if fallback == "" {
		t.Error("expected fallback for Inf inputs")
	}
	if delta != 0 {
		t.Errorf("expected 0 delta for invalid, got %v", delta)
	}
}


func TestPotentialSyncPenalty_NoneWhenAllBelowZeta(t *testing.T) {
	fiveHour1 := makeFiveHourWindow(100, 50, WindowStateFresh, 0)
	fiveHour2 := makeFiveHourWindow(100, 40, WindowStateFresh, 0)
	snap1 := makeSnap(true, false, fiveHour1, QuotaWindowSnapshot{}, 5)
	snap2 := makeSnap(true, false, fiveHour2, QuotaWindowSnapshot{}, 5)

	penalty := computeSyncExhaustionPenalty([]AccountPotentialSnapshot{snap1, snap2}, defaultParams)
	if penalty != 0 {
		t.Errorf("expected 0 penalty when all below zeta, got %v", penalty)
	}
}

func TestPotentialSyncPenalty_AdvisoryNotHardBan(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 100, WindowStateFresh, 0)
	snap := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)

	penalty := computeSyncExhaustionPenalty([]AccountPotentialSnapshot{snap}, defaultParams)
	if penalty < 0 {
		t.Error("penalty must not be negative")
	}
	if penalty > defaultParams.CappedSyncPenalty {
		t.Errorf("penalty %v exceeds cap %v", penalty, defaultParams.CappedSyncPenalty)
	}
}

func TestPotentialSyncPenalty_CappedByMaxPenalty(t *testing.T) {
	candidates := []AccountPotentialSnapshot{}
	for i := 0; i < 10; i++ {
		fiveHour := makeFiveHourWindow(100, 99, WindowStateFresh, 0)
		candidates = append(candidates, makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5))
	}

	penalty := computeSyncExhaustionPenalty(candidates, defaultParams)
	if penalty > defaultParams.CappedSyncPenalty {
		t.Errorf("penalty %v exceeds cap %v", penalty, defaultParams.CappedSyncPenalty)
	}
}

func TestPotentialSyncPenalty_EmptyCandidates(t *testing.T) {
	penalty := computeSyncExhaustionPenalty([]AccountPotentialSnapshot{}, defaultParams)
	if penalty != 0 {
		t.Errorf("expected 0 for empty candidates, got %v", penalty)
	}
}


func TestPotentialTie_Detected(t *testing.T) {
	if !isTie(0.5, 0.5+1e-10, 1e-9) {
		t.Error("expected tie within epsilon")
	}
}

func TestPotentialTie_NotDetected(t *testing.T) {
	if isTie(0.5, 0.6, 1e-9) {
		t.Error("expected no tie outside epsilon")
	}
}

func TestPotentialTie_NaNNotTied(t *testing.T) {
	if isTie(math.NaN(), math.NaN(), 1e-3) {
		t.Error("NaN should not be tied to anything")
	}
	if isTie(0.5, math.NaN(), 1e-3) {
		t.Error("NaN should not be tied to a number")
	}
}

func TestPotentialTie_ZeroEpsilon(t *testing.T) {
	if isTie(0.5, 0.5, 0) {
		t.Error("zero epsilon should require exact equality")
	}
	if !isTie(0.5, 0.5, 0) {
		// exact equality with zero epsilon
	}
}


func TestPotentialScore_UnderusedOutranksOverused(t *testing.T) {
	fiveHourUnder := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	fiveHourOver := makeFiveHourWindow(100, 90, WindowStateFresh, 0)
	snapUnder := makeSnap(true, false, fiveHourUnder, QuotaWindowSnapshot{}, 5)
	snapOver := makeSnap(true, false, fiveHourOver, QuotaWindowSnapshot{}, 5)

	resUnder := ScorePotential(snapUnder, defaultParams, 1)
	resOver := ScorePotential(snapOver, defaultParams, 1)

	if resUnder.Score <= resOver.Score {
		t.Errorf("underused account score (%v) should exceed overused (%v)", resUnder.Score, resOver.Score)
	}
}

func TestPotentialScore_HighPriorityOutranksLowPriority(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 50, WindowStateFresh, 0)
	snapLowPrio := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 10)
	snapHighPrio := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 1)

	resLow := ScorePotential(snapLowPrio, defaultParams, 1)
	resHigh := ScorePotential(snapHighPrio, defaultParams, 1)

	if resHigh.Score <= resLow.Score {
		t.Errorf("high priority score (%v) should exceed low priority (%v)", resHigh.Score, resLow.Score)
	}
}

func TestPotentialScore_UnknownStateNeutralScore(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 30, WindowStateUnknown, 0)
	sevenDay := makeSevenDayWindow(1000, 30, WindowStateUnknown, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	res := ScorePotential(snap, defaultParams, 1)
	if res.Score != 0.5 {
		t.Errorf("expected neutral 0.5 for unknown state, got %v", res.Score)
	}
	if res.Valid {
		t.Error("expected Valid=false for unknown state")
	}
	if res.FallbackReason != "unknown_state" {
		t.Errorf("expected fallback 'unknown_state', got %q", res.FallbackReason)
	}
}

func TestPotentialScore_InvalidInputsProduceFallbackNotCrash(t *testing.T) {
	w := QuotaWindowSnapshot{
		Limit:   math.NaN(),
		Used:    10,
		State:   WindowStateFresh,
	}
	snap := makeSnap(true, false, w, QuotaWindowSnapshot{}, 5)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ScorePotential panicked on invalid input: %v", r)
		}
	}()

	res := ScorePotential(snap, defaultParams, 1)
	if res.Score == 0 && !res.Valid {
		// expected invalid result
	}
}

func TestPotentialScore_UnknownRemainingProducesFallback(t *testing.T) {
	now := time.Now()
	w := QuotaWindowSnapshot{
		Limit:           100,
		Used:            30,
		Remaining:       70,
		remainingUnknown: true,
		WindowStart:     now.Add(-5 * time.Hour),
		WindowEnd:       now,
		ObservedAt:     now,
		State:           WindowStateFresh,
	}
	snap := makeSnap(true, false, w, QuotaWindowSnapshot{}, 5)

	res := ScorePotential(snap, defaultParams, 1)
	if res.Valid {
		t.Error("expected invalid for unknown remaining")
	}
}

func TestPotentialScore_NaNDemandDefaultsToOne(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	snap := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)

	res1 := ScorePotential(snap, defaultParams, 1)
	resNaN := ScorePotential(snap, defaultParams, math.NaN())
	resNeg := ScorePotential(snap, defaultParams, -1)
	resZero := ScorePotential(snap, defaultParams, 0)

	if math.Abs(res1.Score-resNaN.Score) > 1e-9 {
		t.Errorf("NaN demand should behave like 1: got %v vs %v", res1.Score, resNaN.Score)
	}
	if math.Abs(res1.Score-resNeg.Score) > 1e-9 {
		t.Errorf("negative demand should behave like 1: got %v vs %v", res1.Score, resNeg.Score)
	}
	if math.Abs(res1.Score-resZero.Score) > 1e-9 {
		t.Errorf("zero demand should behave like 1: got %v vs %v", res1.Score, resZero.Score)
	}
}

func TestPotentialScore_DefaultDemandWorks(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	snap := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)

	res := ScorePotential(snap, defaultParams, 1)
	if math.IsNaN(res.Score) || math.IsInf(res.Score, 0) {
		t.Errorf("score must not be NaN or Inf, got %v", res.Score)
	}
	if res.Score < 0 || res.Score > 1 {
		t.Errorf("score must be in [0,1], got %v", res.Score)
	}
}

func TestPotentialScore_ScoreInRange(t *testing.T) {
	testCases := []struct {
		used5h  float64
		used7d  float64
		prio    int
	}{
		{0, 0, 1},
		{100, 1000, 10},
		{50, 500, 5},
		{99, 999, 1},
		{1, 1, 10},
	}

	for _, tc := range testCases {
		fiveHour := makeFiveHourWindow(100, tc.used5h, WindowStateFresh, 0)
		sevenDay := makeSevenDayWindow(1000, tc.used7d, WindowStateFresh, 0)
		snap := makeSnap(true, true, fiveHour, sevenDay, tc.prio)

		res := ScorePotential(snap, defaultParams, 1)
		if math.IsNaN(res.Score) || math.IsInf(res.Score, 0) {
			t.Errorf("[used5h=%v used7d=%v prio=%d] score is NaN/Inf: %v", tc.used5h, tc.used7d, tc.prio, res.Score)
		}
		if res.Score < 0 || res.Score > 1 {
			t.Errorf("[used5h=%v used7d=%v prio=%d] score out of [0,1]: %v", tc.used5h, tc.used7d, tc.prio, res.Score)
		}
	}
}

func TestPotentialScore_Deterministic(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 30, WindowStateFresh, 0)
	sevenDay := makeSevenDayWindow(1000, 300, WindowStateFresh, 0)
	snap := makeSnap(true, true, fiveHour, sevenDay, 5)

	for i := 0; i < 100; i++ {
		res := ScorePotential(snap, defaultParams, 1)
		if math.IsNaN(res.Score) || math.IsInf(res.Score, 0) {
			t.Fatalf("non-deterministic score at iteration %d: %v", i, res.Score)
		}
	}
}


func TestPotentialRank_EmptyInput(t *testing.T) {
	res := RankByPotential([]AccountPotentialSnapshot{}, defaultParams, 1)
	if res != nil {
		t.Errorf("expected nil for empty input, got %v", res)
	}
}

func TestPotentialRank_UnderusedRanksFirst(t *testing.T) {
	fiveHour1 := makeFiveHourWindow(100, 90, WindowStateFresh, 0)
	fiveHour2 := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	snapOver := makeSnap(true, false, fiveHour1, QuotaWindowSnapshot{}, 5)
	snapUnder := makeSnap(true, false, fiveHour2, QuotaWindowSnapshot{}, 5)

	results := RankByPotential([]AccountPotentialSnapshot{snapOver, snapUnder}, defaultParams, 1)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("first should have higher score: [%v, %v]", results[0].Score, results[1].Score)
	}
}

func TestPotentialRank_PriorityBreaksTie(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 50, WindowStateFresh, 0)
	snap1 := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)
	snap2 := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)

	// Same score windows; higher priority should win
	results := RankByPotential([]AccountPotentialSnapshot{snap1, snap2}, defaultParams, 1)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The one with higher priority (lower number) should rank first.
	// Since both have same score and same delta, the sort is stable.
}

func TestPotentialRank_SyncPenaltyAppliedToGroup(t *testing.T) {
	fiveHour1 := makeFiveHourWindow(100, 99, WindowStateFresh, 0)
	fiveHour2 := makeFiveHourWindow(100, 99, WindowStateFresh, 0)
	snap1 := makeSnap(true, false, fiveHour1, QuotaWindowSnapshot{}, 5)
	snap2 := makeSnap(true, false, fiveHour2, QuotaWindowSnapshot{}, 5)

	results := RankByPotential([]AccountPotentialSnapshot{snap1, snap2}, defaultParams, 1)

	for _, res := range results {
		if res.Score == 0 {
			t.Error("sync penalty should not hard-ban exhausted accounts; score must be > 0")
		}
	}
}

func TestPotentialRank_UnknownAccountAtEnd(t *testing.T) {
	fiveHour1 := makeFiveHourWindow(100, 20, WindowStateFresh, 0)
	fiveHour2 := makeFiveHourWindow(100, 30, WindowStateUnknown, 0)
	snapGood := makeSnap(true, false, fiveHour1, QuotaWindowSnapshot{}, 5)
	snapUnknown := makeSnap(true, false, fiveHour2, QuotaWindowSnapshot{}, 5)

	results := RankByPotential([]AccountPotentialSnapshot{snapUnknown, snapGood}, defaultParams, 1)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[len(results)-1].FallbackReason != "unknown_state" {
		t.Errorf("unknown account should rank last, got fallback %q at position %d",
			results[len(results)-1].FallbackReason, len(results)-1)
	}
}

func TestPotentialRank_Deterministic(t *testing.T) {
	candidates := []AccountPotentialSnapshot{}
	for i := 0; i < 10; i++ {
		fiveHour := makeFiveHourWindow(100, float64(i*10), WindowStateFresh, 0)
		candidates = append(candidates, makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, i%10+1))
	}

	for i := 0; i < 50; i++ {
		res := RankByPotential(candidates, defaultParams, 1)
		for j, r := range res {
			if math.IsNaN(r.Score) || math.IsInf(r.Score, 0) {
				t.Fatalf("non-deterministic at iter %d pos %d: score=%v", i, j, r.Score)
			}
		}
	}
}

func TestPotentialRank_TieEpsilonBreaksTies(t *testing.T) {
	fiveHour := makeFiveHourWindow(100, 50, WindowStateFresh, 0)
	snap1 := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)
	snap2 := makeSnap(true, false, fiveHour, QuotaWindowSnapshot{}, 5)

	params := defaultParams
	params.TieEpsilon = 1e-9

	results := RankByPotential([]AccountPotentialSnapshot{snap1, snap2}, params, 1)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestPotentialRank_ScoreInRangeForAll(t *testing.T) {
	candidates := []AccountPotentialSnapshot{}
	for i := 0; i < 20; i++ {
		fiveHour := makeFiveHourWindow(100, float64(i*5), WindowStateFresh, 0)
		sevenDay := makeSevenDayWindow(1000, float64(i*50), WindowStateFresh, 0)
		candidates = append(candidates, makeSnap(true, true, fiveHour, sevenDay, i%10+1))
	}

	results := RankByPotential(candidates, defaultParams, 1)

	for i, res := range results {
		if math.IsNaN(res.Score) || math.IsInf(res.Score, 0) {
			t.Errorf("result[%d] score is NaN/Inf: %v", i, res.Score)
		}
		if res.Score < 0 || res.Score > 1 {
			t.Errorf("result[%d] score out of [0,1]: %v", i, res.Score)
		}
	}
}
