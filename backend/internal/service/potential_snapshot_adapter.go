package service

import (
	"time"
)

// Plan-type prior fallback values for 5h window when usedUSD == 0 and limit == 0.
var planTypePriors = map[string]float64{
	"plus": 12.0,
	"team": 10.0,
	"pro":  200.0,
	"free": 1.0,
	"other": 5.0,
}

// BuildAdvisoryQuotaSnapshot derives an AccountPotentialSnapshot from existing
// account/quota/model quota data available to the scheduler without hot-path writes.
// It uses plan-type prior fallback for unknown model quota windows and marks
// unavailable data as unknown or stale rather than exhausted.
func BuildAdvisoryQuotaSnapshot(account *Account, loadInfo *AccountLoadInfo) AccountPotentialSnapshot {
	snap := AccountPotentialSnapshot{
		AccountID:   account.ID,
		AccountName: account.Name,
		Platform:    account.Platform,
		Priority:    account.Priority,
		Status:      account.Status,
		Schedulable: account.IsSchedulable(),
		LastUsedAt:  account.LastUsedAt,
		PlanType:    getPlanType(account),
		Has5hWindow: false,
		Has7dWindow: false,
	}

	snap.RateLimitAt = account.RateLimitedAt
	snap.OverloadUntil = account.OverloadUntil

	snap.FiveHourWindow = build5hWindowSnapshot(account)
	if snap.FiveHourWindow.State != WindowStateUnknown {
		snap.Has5hWindow = true
	}

	snap.SevenDayWindow = build7dWindowSnapshot(account)
	if snap.SevenDayWindow.State != WindowStateUnknown {
		snap.Has7dWindow = true
	}

	_ = loadInfo
	return snap
}

// getPlanType returns the plan type from account credentials, or "other" if not found.
func getPlanType(account *Account) string {
	if account == nil || account.Credentials == nil {
		return "other"
	}
	if pt, ok := account.Credentials["plan_type"].(string); ok && pt != "" {
		return pt
	}
	return "other"
}

// getPlanTypePrior returns the prior limit for a plan type, or 5.0 (other) if unknown.
func getPlanTypePrior(planType string) float64 {
	if prior, ok := planTypePriors[planType]; ok {
		return prior
	}
	return 5.0 // "other" default
}

// build5hWindowSnapshot builds the 5h quota window snapshot.
// It prefers quota_limit/quota_used if limit > 0, otherwise falls back to
// quota_daily_limit/quota_daily_used. If both are unavailable, it applies
// plan-type prior fallback when usedUSD == 0 and limit == 0.
func hasExtraKey(account *Account, key string) bool {
	if account == nil || account.Extra == nil {
		return false
	}
	_, ok := account.Extra[key]
	return ok
}

func build5hWindowSnapshot(account *Account) QuotaWindowSnapshot {
	now := time.Now()
	windowStart := now.Add(-5 * time.Hour)

	hasPrimary := hasExtraKey(account, "quota_limit")
	hasDaily := hasExtraKey(account, "quota_daily_limit")

	if !hasPrimary && !hasDaily {
		return QuotaWindowSnapshot{
			Limit:            0,
			Used:             0,
			Remaining:        0,
			WindowStart:      windowStart,
			WindowEnd:        now,
			ResetAt:          windowStart.Add(5 * time.Hour),
			ObservedAt:       now,
			Source:           "account_extra",
			State:            WindowStateUnknown,
			remainingUnknown: true,
		}
	}

	limit := account.GetQuotaLimit()
	used := account.GetQuotaUsed()

	if limit <= 0 && hasDaily {
		limit = account.GetQuotaDailyLimit()
		used = account.GetQuotaDailyUsed()
	}

	if limit <= 0 && used <= 0 {
		planType := getPlanType(account)
		prior := getPlanTypePrior(planType)
		return QuotaWindowSnapshot{
			Limit:            prior,
			Used:             0,
			Remaining:        prior,
			WindowStart:      windowStart,
			WindowEnd:        now,
			ResetAt:          windowStart.Add(5 * time.Hour),
			ObservedAt:       now,
			Source:           "plan_type_prior",
			State:            WindowStateFresh,
			remainingUnknown: false,
		}
	}

	if used <= 0 {
		return QuotaWindowSnapshot{
			Limit:            limit,
			Used:             0,
			Remaining:        limit,
			WindowStart:      windowStart,
			WindowEnd:        now,
			ResetAt:          windowStart.Add(5 * time.Hour),
			ObservedAt:       now,
			Source:           "account_extra",
			State:            WindowStateFresh,
			remainingUnknown: false,
		}
	}

	remaining := limit - used
	remainingUnknown := remaining < 0
	if remainingUnknown {
		remaining = 0
	}

	observedAt := now
	if !account.UpdatedAt.IsZero() {
		observedAt = account.UpdatedAt
	}

	return QuotaWindowSnapshot{
		Limit:            limit,
		Used:             used,
		Remaining:        remaining,
		WindowStart:      windowStart,
		WindowEnd:        now,
		ResetAt:          windowStart.Add(5 * time.Hour),
		ObservedAt:       observedAt,
		Source:           "account_extra",
		State:            WindowStateFresh,
		remainingUnknown: remainingUnknown,
	}
}

// build7dWindowSnapshot builds the 7d quota window snapshot using
// quota_weekly_limit and quota_weekly_used from account Extra data.
func build7dWindowSnapshot(account *Account) QuotaWindowSnapshot {
	now := time.Now()
	windowStart := now.Add(-7 * 24 * time.Hour)

	hasWeekly := hasExtraKey(account, "quota_weekly_limit")

	if !hasWeekly {
		return QuotaWindowSnapshot{
			Limit:            0,
			Used:             0,
			Remaining:        0,
			WindowStart:      windowStart,
			WindowEnd:        now,
			ResetAt:          windowStart.Add(7 * 24 * time.Hour),
			ObservedAt:       now,
			Source:           "account_extra",
			State:            WindowStateUnknown,
			remainingUnknown: true,
		}
	}

	limit := account.GetQuotaWeeklyLimit()
	used := account.GetQuotaWeeklyUsed()

	if used <= 0 {
		return QuotaWindowSnapshot{
			Limit:            limit,
			Used:             0,
			Remaining:        limit,
			WindowStart:      windowStart,
			WindowEnd:        now,
			ResetAt:          windowStart.Add(7 * 24 * time.Hour),
			ObservedAt:       now,
			Source:           "account_extra",
			State:            WindowStateFresh,
			remainingUnknown: false,
		}
	}

	remaining := limit - used
	remainingUnknown := remaining < 0
	if remainingUnknown {
		remaining = 0
	}

	observedAt := now
	if !account.UpdatedAt.IsZero() {
		observedAt = account.UpdatedAt
	}

	return QuotaWindowSnapshot{
		Limit:            limit,
		Used:             used,
		Remaining:        remaining,
		WindowStart:      windowStart,
		WindowEnd:        now,
		ResetAt:          windowStart.Add(7 * 24 * time.Hour),
		ObservedAt:       observedAt,
		Source:           "account_extra",
		State:            WindowStateFresh,
		remainingUnknown: remainingUnknown,
	}
}