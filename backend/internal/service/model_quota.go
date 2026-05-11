package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

// ModelQuotaWindow represents a single quota time window for a model.
// TotalUSD and RemainingUSD are nil when unknown_accounts_count > 0.
type ModelQuotaWindow struct {
	TotalUSD             *float64 `json:"total_usd"`
	UsedUSD              float64  `json:"used_usd"`
	RemainingUSD         *float64 `json:"remaining_usd"`
	AccountsCount        int      `json:"accounts_count"`
	UnknownAccountsCount int      `json:"unknown_accounts_count"`
}

// ModelQuota represents the per-model aggregate quota estimate for one time window.
// QuotaPool is "account_shared" because provider usage percentages represent shared
// account quota pools — summing per-model quotas can double-count usage against the
// same underlying account limits. Consumers should treat these as availability
// estimates, not independently spendable pools.
type ModelQuota struct {
	ID        string           `json:"id"`
	Object    string           `json:"object"`
	QuotaPool string           `json:"quota_pool"`
	FiveHour  ModelQuotaWindow `json:"five_hour"`
	SevenDay  ModelQuotaWindow `json:"seven_day"`
}

// ModelQuotaResponse is the OpenAI-style list envelope for model quota responses.
type ModelQuotaResponse struct {
	Object string       `json:"object"`
	Data   []ModelQuota `json:"data"`
}

func modelQuotasCacheKey(groupID *int64, platform string) string {
	return fmt.Sprintf("mq:%d|%s", derefGroupID(groupID), strings.TrimSpace(platform))
}

func (s *GatewayService) GetModelQuotas(ctx context.Context, groupID *int64, platform string) *ModelQuotaResponse {
	cacheKey := modelQuotasCacheKey(groupID, platform)
	if s.modelQuotasCache != nil {
		if cached, found := s.modelQuotasCache.Get(cacheKey); found {
			if r, ok := cached.(*ModelQuotaResponse); ok {
				modelQuotasCacheHitTotal.Add(1)
				return cloneModelQuotaResponse(r)
			}
		}
	}
	modelQuotasCacheMissTotal.Add(1)

	var (
		accounts []Account
		err      error
	)
	if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
	} else {
		accounts, err = s.accountRepo.ListSchedulable(ctx)
	}
	if err != nil || len(accounts) == 0 {
		resp := &ModelQuotaResponse{Object: "list", Data: nil}
		if s.modelQuotasCache != nil {
			s.modelQuotasCache.Set(cacheKey, cloneModelQuotaResponse(resp), s.modelQuotasCacheTTL)
		}
		return cloneModelQuotaResponse(resp)
	}

	if platform != "" {
		filtered := make([]Account, 0, len(accounts))
		for _, acc := range accounts {
			if acc.Platform == platform {
				filtered = append(filtered, acc)
			}
		}
		accounts = filtered
	}
	if len(accounts) == 0 {
		resp := &ModelQuotaResponse{Object: "list", Data: nil}
		if s.modelQuotasCache != nil {
			s.modelQuotasCache.Set(cacheKey, cloneModelQuotaResponse(resp), s.modelQuotasCacheTTL)
		}
		return cloneModelQuotaResponse(resp)
	}

	modelAccountSet := make(map[string]map[int64]struct{})
	accountByID := make(map[int64]Account, len(accounts))
	for _, acc := range accounts {
		accountByID[acc.ID] = acc
		mapping := acc.GetModelMapping()
		for modelID := range mapping {
			if _, ok := modelAccountSet[modelID]; !ok {
				modelAccountSet[modelID] = make(map[int64]struct{})
			}
			modelAccountSet[modelID][acc.ID] = struct{}{}
		}
	}
	if len(modelAccountSet) == 0 {
		resp := &ModelQuotaResponse{Object: "list", Data: nil}
		if s.modelQuotasCache != nil {
			s.modelQuotasCache.Set(cacheKey, cloneModelQuotaResponse(resp), s.modelQuotasCacheTTL)
		}
		return cloneModelQuotaResponse(resp)
	}

	type cluster struct {
		accountIDs []int64
		models     []string
	}
	clusters := make(map[string]*cluster)
	for modelID, accountSet := range modelAccountSet {
		ids := make([]int64, 0, len(accountSet))
		for id := range accountSet {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		keyParts := make([]string, 0, len(ids))
		for _, id := range ids {
			keyParts = append(keyParts, strconv.FormatInt(id, 10))
		}
		clusterKey := strings.Join(keyParts, "|")
		if _, ok := clusters[clusterKey]; !ok {
			clusters[clusterKey] = &cluster{accountIDs: ids, models: make([]string, 0, 1)}
		}
		clusters[clusterKey].models = append(clusters[clusterKey].models, modelID)
	}

	now := time.Now()
	type clusterAgg struct {
		fiveHour ModelQuotaWindow
		sevenDay ModelQuotaWindow
	}
	clusterAggs := make(map[string]clusterAgg, len(clusters))

	for clusterKey, c := range clusters {
		stats5h, err5h := getAccountWindowStatsBatchCompat(ctx, s.usageLogRepo, c.accountIDs, now.Add(-5*time.Hour))
		stats7d, err7d := getAccountWindowStatsBatchCompat(ctx, s.usageLogRepo, c.accountIDs, now.Add(-7*24*time.Hour))
		if err5h != nil || err7d != nil {
			stats5h = map[int64]*usagestats.AccountStats{}
			stats7d = map[int64]*usagestats.AccountStats{}
		}

		agg5h := aggregateQuotaWindow(c.accountIDs, accountByID, stats5h, "codex_5h_used_percent")
		agg7d := aggregateQuotaWindow(c.accountIDs, accountByID, stats7d, "codex_7d_used_percent")
		clusterAggs[clusterKey] = clusterAgg{fiveHour: agg5h, sevenDay: agg7d}
	}

	resp := &ModelQuotaResponse{Object: "list", Data: make([]ModelQuota, 0, len(modelAccountSet))}
	for clusterKey, c := range clusters {
		agg := clusterAggs[clusterKey]
		for _, modelID := range c.models {
			resp.Data = append(resp.Data, ModelQuota{
				ID:        modelID,
				Object:    "model_quota",
				QuotaPool: "account_shared",
				FiveHour:  agg.fiveHour,
				SevenDay:  agg.sevenDay,
			})
		}
	}

	sort.Slice(resp.Data, func(i, j int) bool { return resp.Data[i].ID < resp.Data[j].ID })
	if s.modelQuotasCache != nil {
		s.modelQuotasCache.Set(cacheKey, cloneModelQuotaResponse(resp), s.modelQuotasCacheTTL)
	}
	return cloneModelQuotaResponse(resp)
}

func aggregateQuotaWindow(accountIDs []int64, accountByID map[int64]Account, statsByAccount map[int64]*usagestats.AccountStats, percentKey string) ModelQuotaWindow {
	window := ModelQuotaWindow{AccountsCount: len(accountIDs)}
	var totalKnown float64
	var remainingKnown float64

	for _, accountID := range accountIDs {
		acc := accountByID[accountID]
		usedUSD := 0.0
		if stats, ok := statsByAccount[accountID]; ok && stats != nil {
			usedUSD = stats.Cost
		}
		window.UsedUSD += usedUSD

		usedPercentRaw, ok := acc.Extra[percentKey]
		if !ok {
			window.UnknownAccountsCount++
			continue
		}
		usedPercent := parseExtraFloat64(usedPercentRaw)
		if usedUSD <= 0 || usedPercent <= 0 {
			window.UnknownAccountsCount++
			continue
		}

		totalUSD := usedUSD / (usedPercent / 100)
		remainingUSD := totalUSD - usedUSD
		if remainingUSD < 0 {
			remainingUSD = 0
		}
		totalKnown += totalUSD
		remainingKnown += remainingUSD
	}

	if window.UnknownAccountsCount > 0 {
		window.TotalUSD = nil
		window.RemainingUSD = nil
		return window
	}
	totalKnown = math.Max(0, totalKnown)
	remainingKnown = math.Max(0, remainingKnown)
	window.TotalUSD = &totalKnown
	window.RemainingUSD = &remainingKnown
	return window
}

func cloneModelQuotaResponse(r *ModelQuotaResponse) *ModelQuotaResponse {
	if r == nil {
		return nil
	}
	cloned := *r
	cloned.Data = make([]ModelQuota, len(r.Data))
	copy(cloned.Data, r.Data)
	for i := range cloned.Data {
		cloned.Data[i].FiveHour.TotalUSD = cloneFloat64Ptr(r.Data[i].FiveHour.TotalUSD)
		cloned.Data[i].FiveHour.RemainingUSD = cloneFloat64Ptr(r.Data[i].FiveHour.RemainingUSD)
		cloned.Data[i].SevenDay.TotalUSD = cloneFloat64Ptr(r.Data[i].SevenDay.TotalUSD)
		cloned.Data[i].SevenDay.RemainingUSD = cloneFloat64Ptr(r.Data[i].SevenDay.RemainingUSD)
	}
	return &cloned
}

func cloneFloat64Ptr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

type usageLogBatchReader interface {
	GetAccountWindowStatsBatch(ctx context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error)
}

func getAccountWindowStatsBatchCompat(ctx context.Context, repo UsageLogRepository, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error) {
	if len(accountIDs) == 0 {
		return map[int64]*usagestats.AccountStats{}, nil
	}
	if batchReader, ok := repo.(usageLogBatchReader); ok {
		return batchReader.GetAccountWindowStatsBatch(ctx, accountIDs, startTime)
	}
	out := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, accountID := range accountIDs {
		stats, err := repo.GetAccountWindowStats(ctx, accountID, startTime)
		if err != nil {
			return nil, err
		}
		if stats == nil {
			stats = &usagestats.AccountStats{}
		}
		out[accountID] = stats
	}
	return out, nil
}
