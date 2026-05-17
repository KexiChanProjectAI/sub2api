//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func makeTestAccount(id int64, platform string, status string, schedulable bool, priority int) Account {
	return Account{
		ID:          id,
		Platform:    platform,
		Type:        AccountTypeAPIKey,
		Status:      status,
		Schedulable: schedulable,
		Concurrency: 1,
		Priority:    priority,
		Credentials: map[string]any{"plan_type": "plus"},
		Extra:       map[string]any{},
	}
}

func makeAccountWithQuota(id int64, priority int, limit, used float64, planType string) Account {
	acc := makeTestAccount(id, PlatformOpenAI, StatusActive, true, priority)
	acc.Credentials = map[string]any{"plan_type": planType}
	acc.Extra = map[string]any{
		"quota_limit": limit,
		"quota_used":  used,
	}
	return acc
}

func makeAccountWithBothWindows(id int64, priority int, limit5h, used5h, limit7d, used7d float64) Account {
	acc := makeTestAccount(id, PlatformOpenAI, StatusActive, true, priority)
	acc.Extra = map[string]any{
		"quota_limit":        limit5h,
		"quota_used":         used5h,
		"quota_weekly_limit": limit7d,
		"quota_weekly_used":  used7d,
	}
	return acc
}

func newTestSchedulerService(accounts []Account, advancedEnabled, potentialEnabled string) *OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false

	combinedStub := &combinedSchedulerSettingRepoStub{
		values: map[string]string{},
	}
	if advancedEnabled != "" {
		combinedStub.values[openAIAdvancedSchedulerSettingKey] = advancedEnabled
	}
	if potentialEnabled != "" {
		combinedStub.values[openAIPotentialSchedulerSettingKey] = potentialEnabled
	}

	rl := &RateLimitService{
		settingService: NewSettingService(combinedStub, &config.Config{}),
	}

	return &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
		rateLimitService:   rl,
	}
}

type combinedSchedulerSettingRepoStub struct {
	values map[string]string
}

func (s *combinedSchedulerSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	value, err := s.GetValue(ctx, key)
	if err != nil {
		return nil, err
	}
	return &Setting{Key: key, Value: value}, nil
}

func (s *combinedSchedulerSettingRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if s == nil || s.values == nil {
		return "", ErrSettingNotFound
	}
	value, ok := s.values[key]
	if !ok {
		return "", ErrSettingNotFound
	}
	return value, nil
}

func (s *combinedSchedulerSettingRepoStub) Set(context.Context, string, string) error {
	panic("unexpected call to Set")
}

func (s *combinedSchedulerSettingRepoStub) GetMultiple(context.Context, []string) (map[string]string, error) {
	panic("unexpected call to GetMultiple")
}

func (s *combinedSchedulerSettingRepoStub) SetMultiple(context.Context, map[string]string) error {
	panic("unexpected call to SetMultiple")
}

func (s *combinedSchedulerSettingRepoStub) GetAll(context.Context) (map[string]string, error) {
	panic("unexpected call to GetAll")
}

func (s *combinedSchedulerSettingRepoStub) Delete(context.Context, string) error {
	panic("unexpected call to Delete")
}

func TestPotentialSchedulerIntegration_GateDisabled_PotentialPathNotUsed(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 30, 500, 100),
		makeAccountWithBothWindows(2, 1, 100, 10, 500, 50),
	}

	svc := newTestSchedulerService(accounts, "true", "false")

	require.True(t, svc.isOpenAIAdvancedSchedulerEnabled(context.Background()))
	require.False(t, svc.isOpenAIPotentialSchedulerEnabled(context.Background()))

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "legacy", decision.Strategy)
	require.Empty(t, decision.PotentialFallbackReason)
}

func TestPotentialSchedulerIntegration_GateEnabled_PotentialPathEntered(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 30, 500, 100),
		makeAccountWithBothWindows(2, 1, 100, 10, 500, 50),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	require.True(t, svc.isOpenAIAdvancedSchedulerEnabled(context.Background()))
	require.True(t, svc.isOpenAIPotentialSchedulerEnabled(context.Background()))

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Contains(t, []string{"potential", "legacy"}, decision.Strategy)
}

func TestPotentialSchedulerIntegration_FallbackWhenAllUnknownWindows(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeTestAccount(1, PlatformOpenAI, StatusActive, true, 1),
		makeTestAccount(2, PlatformOpenAI, StatusActive, true, 1),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "legacy", decision.Strategy)
	require.NotEmpty(t, decision.PotentialFallbackReason)
}

func TestPotentialSchedulerIntegration_PotentialWithMixedCandidates(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 10, 500, 50),
		makeTestAccount(2, PlatformOpenAI, StatusActive, true, 1),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "legacy", decision.Strategy)
}

func TestPotentialSchedulerIntegration_PotentialWithAllValidCandidates(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 10, 500, 50),
		makeAccountWithBothWindows(2, 1, 100, 80, 500, 400),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "potential", decision.Strategy)
	require.Contains(t, []int64{1, 2}, selection.Account.ID)
}

func TestPotentialSchedulerIntegration_ScoreOrdering_LowerUsageRankedHigher(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 90, 500, 450),
		makeAccountWithBothWindows(2, 1, 100, 10, 500, 50),
		makeAccountWithBothWindows(3, 1, 100, 50, 500, 250),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "potential", decision.Strategy)
	require.NotEmpty(t, selection.Account.ID)
}

func TestPotentialSchedulerIntegration_ScoreOrdering_SameScoreDeterministic(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 50, 500, 250),
		makeAccountWithBothWindows(2, 1, 100, 50, 500, 250),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)

	for i := 0; i < 5; i++ {
		selection, decision, err := svc.SelectAccountWithScheduler(
			context.Background(),
			&groupID,
			"",
			"",
			"gpt-4",
			nil,
			OpenAIUpstreamTransportAny,
			false,
		)

		require.NoError(t, err)
		require.NotNil(t, selection)
		require.Equal(t, "potential", decision.Strategy)
		require.Contains(t, []int64{1, 2}, selection.Account.ID)
	}
}

func TestPotentialSchedulerIntegration_ScoreOrdering_DifferentUsage(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 30, 500, 150),
		makeAccountWithBothWindows(2, 1, 100, 60, 500, 300),
		makeAccountWithBothWindows(3, 1, 100, 10, 500, 50),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)

	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, "potential", decision.Strategy)
	require.Contains(t, []int64{1, 2, 3}, selection.Account.ID)
}

func TestPotentialSchedulerIntegration_NoNaN_BuildAdvisoryQuotaSnapshot(t *testing.T) {
	testCases := []struct {
		name string
		acc  Account
	}{
		{name: "zero limit", acc: makeAccountWithQuota(1, 1, 0, 0, "plus")},
		{name: "negative used", acc: makeAccountWithQuota(2, 1, 100, -10, "plus")},
		{name: "used exceeds limit", acc: makeAccountWithQuota(3, 1, 100, 150, "plus")},
		{name: "no extra data", acc: makeTestAccount(4, PlatformOpenAI, StatusActive, true, 1)},
		{name: "nil extra", acc: Account{ID: 5, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Priority: 1, Extra: nil}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			snap := BuildAdvisoryQuotaSnapshot(&tc.acc, nil)

			if snap.Has5hWindow {
				require.False(t, math.IsNaN(snap.FiveHourWindow.Limit))
				require.False(t, math.IsNaN(snap.FiveHourWindow.Used))
				require.False(t, math.IsNaN(snap.FiveHourWindow.Remaining))
			}
			if snap.Has7dWindow {
				require.False(t, math.IsNaN(snap.SevenDayWindow.Limit))
				require.False(t, math.IsNaN(snap.SevenDayWindow.Used))
				require.False(t, math.IsNaN(snap.SevenDayWindow.Remaining))
			}
		})
	}
}

func TestPotentialSchedulerIntegration_NoNaN_PotentialScoring(t *testing.T) {
	testCases := []struct {
		name string
		snap AccountPotentialSnapshot
	}{
		{
			name: "all unknown windows",
			snap: AccountPotentialSnapshot{AccountID: 1, Has5hWindow: false, Has7dWindow: false, Priority: 1},
		},
		{
			name: "stale window",
			snap: AccountPotentialSnapshot{
				AccountID:   2,
				Has5hWindow: true,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      100,
					Used:       50,
					Remaining:  50,
					State:      WindowStateFresh,
					ObservedAt: time.Now().Add(-24 * time.Hour),
				},
				Priority: 1,
			},
		},
		{
			name: "edge case zero limit",
			snap: AccountPotentialSnapshot{
				AccountID:   3,
				Has5hWindow: true,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      0,
					Used:       0,
					Remaining:  0,
					State:      WindowStateFresh,
					ObservedAt: time.Now(),
				},
				Priority: 1,
			},
		},
	}

	params := DefaultPotentialParameters()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := RankByPotential([]AccountPotentialSnapshot{tc.snap}, params, 1.0)

			if len(results) > 0 {
				require.False(t, math.IsNaN(results[0].Score))
				require.False(t, math.IsInf(results[0].Score, 0))
				require.False(t, math.IsNaN(results[0].Delta))
				require.False(t, math.IsInf(results[0].Delta, 0))
			}
		})
	}
}

func TestPotentialSchedulerIntegration_NoNaN_MaintenanceHints(t *testing.T) {
	testCases := []struct {
		name string
		snap AccountPotentialSnapshot
	}{
		{
			name: "no windows",
			snap: AccountPotentialSnapshot{AccountID: 1, Has5hWindow: false, Has7dWindow: false, Priority: 1},
		},
		{
			name: "unknown 5h only",
			snap: AccountPotentialSnapshot{
				AccountID:   2,
				Has5hWindow: true,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      0,
					Used:       0,
					State:      WindowStateUnknown,
					ObservedAt: time.Now(),
				},
				Priority: 1,
			},
		},
		{
			name: "valid 5h",
			snap: AccountPotentialSnapshot{
				AccountID:   3,
				Has5hWindow: true,
				FiveHourWindow: QuotaWindowSnapshot{
					Limit:      100,
					Used:       30,
					Remaining:  70,
					State:      WindowStateFresh,
					ObservedAt: time.Now(),
				},
				Priority: 1,
			},
		},
	}

	params := DefaultPotentialParameters()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{tc.snap}, params)

			for _, hint := range hints {
				require.False(t, math.IsNaN(hint.ScoreDelta))
				require.False(t, math.IsInf(hint.ScoreDelta, 0))
				require.False(t, math.IsNaN(hint.Urgency))
				require.False(t, math.IsInf(hint.Urgency, 0))
			}
		})
	}
}

func TestPotentialSchedulerIntegration_PotentialEnabled_SchedulerSelectsAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 10, 500, 50),
		makeAccountWithBothWindows(2, 1, 100, 10, 500, 50),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Contains(t, []string{"potential", "legacy"}, decision.Strategy)
}

func TestPotentialSchedulerIntegration_ExcludedIDs_PotentialEnabled(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 10, 500, 50),
		makeAccountWithBothWindows(2, 1, 100, 10, 500, 50),
		makeAccountWithBothWindows(3, 1, 100, 5, 500, 25),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	excludedIDs := map[int64]struct{}{3: {}}

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		excludedIDs,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.NotEqual(t, int64(3), selection.Account.ID)
}

func TestPotentialSchedulerIntegration_SlotAcquisition_PotentialEnabled(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 10, 500, 50),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)

	customCache := schedulerTestConcurrencyCache{
		acquireResults: map[int64]bool{1: true},
	}

	svc.concurrencyService = NewConcurrencyService(customCache)

	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
}

func TestPotentialSchedulerIntegration_SingleCandidate(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		makeAccountWithBothWindows(1, 1, 100, 50, 500, 250),
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(1), selection.Account.ID)
}

func TestPotentialSchedulerIntegration_EmptyCandidates(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, _, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.Error(t, err)
	require.Nil(t, selection)
}

func TestPotentialSchedulerIntegration_PlanTypePriorFallback(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()
	resetOpenAIPotentialSchedulerSettingCacheForTest()

	accounts := []Account{
		{
			ID:          1,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Concurrency: 1,
			Priority:    1,
			Credentials: map[string]any{"plan_type": "plus"},
			Extra:       map[string]any{},
		},
	}

	svc := newTestSchedulerService(accounts, "true", "true")

	groupID := int64(1)
	selection, decision, err := svc.SelectAccountWithScheduler(
		context.Background(),
		&groupID,
		"",
		"",
		"gpt-4",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Contains(t, []string{"potential", "legacy"}, decision.Strategy)
}

func TestPotentialSchedulerIntegration_MaintenanceHints_EmptyCandidates(t *testing.T) {
	params := DefaultPotentialParameters()

	hints := ComputeMaintenanceHints([]AccountPotentialSnapshot{}, params)
	require.NotNil(t, hints)
	require.Len(t, hints, 0)
}

func TestPotentialSchedulerIntegration_MaintenanceHints_MixedWindows(t *testing.T) {
	params := DefaultPotentialParameters()
	now := time.Now()

	candidates := []AccountPotentialSnapshot{
		{
			AccountID:   1,
			Has5hWindow: true,
			FiveHourWindow: QuotaWindowSnapshot{
				Limit:      100,
				Used:       20,
				Remaining:  80,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			Has7dWindow: false,
		},
		{
			AccountID:   2,
			Has5hWindow: false,
			Has7dWindow: false,
		},
		{
			AccountID:   3,
			Has5hWindow: true,
			FiveHourWindow: QuotaWindowSnapshot{
				Limit:      100,
				Used:       95,
				Remaining:  5,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
			Has7dWindow: true,
			SevenDayWindow: QuotaWindowSnapshot{
				Limit:      500,
				Used:       490,
				Remaining:  10,
				State:      WindowStateFresh,
				ObservedAt: now,
			},
		},
	}

	hints := ComputeMaintenanceHints(candidates, params)

	require.NotNil(t, hints)
}

func TestPotentialSchedulerIntegration_MaintenanceHints_NoPanicWithNilWindows(t *testing.T) {
	params := DefaultPotentialParameters()

	candidates := []AccountPotentialSnapshot{
		{
			AccountID:   1,
			Has5hWindow: true,
			FiveHourWindow: QuotaWindowSnapshot{
				Limit:      100,
				Used:       50,
				State:      WindowStateFresh,
				ObservedAt: time.Now(),
			},
		},
	}

	hints := ComputeMaintenanceHints(candidates, params)
	require.NotNil(t, hints)
}

func TestPotentialSchedulerIntegration_BuildSnapshot_AccountWithNoExtra(t *testing.T) {
	acc := &Account{
		ID:          1,
		Name:        "test",
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
		Priority:    1,
		Credentials: map[string]any{"plan_type": "free"},
		Extra:       nil,
	}

	snap := BuildAdvisoryQuotaSnapshot(acc, nil)

	require.Equal(t, int64(1), snap.AccountID)
	require.False(t, snap.Has5hWindow)
	require.False(t, snap.Has7dWindow)
}

func TestPotentialSchedulerIntegration_BuildSnapshot_AccountWithEmptyExtra(t *testing.T) {
	acc := &Account{
		ID:          2,
		Name:        "test2",
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
		Priority:    1,
		Credentials: map[string]any{"plan_type": "team"},
		Extra:       map[string]any{},
	}

	snap := BuildAdvisoryQuotaSnapshot(acc, nil)

	require.Equal(t, int64(2), snap.AccountID)
	require.False(t, snap.Has5hWindow)
	require.False(t, snap.Has7dWindow)
}

func TestPotentialSchedulerIntegration_BuildSnapshot_AccountWithZeroLimits(t *testing.T) {
	acc := &Account{
		ID:          3,
		Name:        "test3",
		Platform:    PlatformOpenAI,
		Status:      StatusActive,
		Schedulable: true,
		Priority:    1,
		Credentials: map[string]any{"plan_type": "pro"},
		Extra: map[string]any{
			"quota_limit":        0.0,
			"quota_used":         0.0,
			"quota_weekly_limit": 0.0,
			"quota_weekly_used":  0.0,
		},
	}

	snap := BuildAdvisoryQuotaSnapshot(acc, nil)

	require.True(t, snap.Has5hWindow)
	require.Equal(t, 200.0, snap.FiveHourWindow.Limit)
	require.Equal(t, 0.0, snap.FiveHourWindow.Used)
}
