//go:build unit

package server_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TODO (Task 2): Uncomment and fix the gatewayDeps setup once all services are properly stubbed.
func TestGatewayAPIContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		setup      func(t *testing.T, deps *gatewayContractDeps)
		method     string
		path       string
		body       string
		headers    map[string]string
		wantStatus int
		wantBody   func(t *testing.T, body string)
	}{
		{
			name:       "GET /v1/model-quotas without API key returns 401",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    nil,
			wantStatus: http.StatusUnauthorized,
			wantBody: func(t *testing.T, body string) {
				require.Contains(t, body, "api_key_required")
			},
		},
		{
			name:       "GET /v1/model-quotas with invalid API key returns 401",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-invalid-key"},
			wantStatus: http.StatusUnauthorized,
			wantBody: func(t *testing.T, body string) {
				require.Contains(t, body, "invalid_api_key")
			},
		},
		{
			name:       "GET /v1/model-quotas with valid API key returns 200 and list",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-test-key-group-a"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string) {
				require.Contains(t, body, `"object":"list"`, "response should have object:list")
				require.Contains(t, body, `"data":[`, "response should have data array")
				require.Contains(t, body, `"object":"model_quota"`, "items should have object:model_quota")
				require.Contains(t, body, `"quota_pool"`, "items should have quota_pool")
				require.Contains(t, body, `"five_hour"`, "items should have five_hour window")
				require.Contains(t, body, `"seven_day"`, "items should have seven_day window")
				assertNoAccountLeakage(t, body)
			},
		},
		{
			name:       "GET /v1/model-quotas group isolation - group A sees only group A models",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-test-key-group-a"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string) {
				require.NotContains(t, body, `"id":"model-group-b-only"`, "group A should not see group B models")
			},
		},
		{
			name:       "GET /v1/model-quotas group isolation - group B sees only group B models",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-test-key-group-b"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string) {
				require.NotContains(t, body, `"id":"model-group-a-only"`, "group B should not see group A models")
			},
		},
		{
			name:       "GET /v1/model-quotas response has correct five_hour schema",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-test-key-group-a"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string) {
				require.Contains(t, body, `"five_hour":{`, "five_hour should be an object")
				require.Contains(t, body, `"total_usd"`, "five_hour should have total_usd")
				require.Contains(t, body, `"used_usd"`, "five_hour should have used_usd")
				require.Contains(t, body, `"remaining_usd"`, "five_hour should have remaining_usd")
				require.Contains(t, body, `"accounts_count"`, "five_hour should have accounts_count")
				require.Contains(t, body, `"unknown_accounts_count"`, "five_hour should have unknown_accounts_count")
			},
		},
		{
			name:       "GET /v1/model-quotas response has correct seven_day schema",
			method:     http.MethodGet,
			path:       "/v1/model-quotas",
			headers:    map[string]string{"Authorization": "Bearer sk-test-key-group-a"},
			wantStatus: http.StatusOK,
			wantBody: func(t *testing.T, body string) {
				require.Contains(t, body, `"seven_day":{`, "seven_day should be an object")
				require.Contains(t, body, `"total_usd"`, "seven_day should have total_usd")
				require.Contains(t, body, `"used_usd"`, "seven_day should have used_usd")
				require.Contains(t, body, `"remaining_usd"`, "seven_day should have remaining_usd")
				require.Contains(t, body, `"accounts_count"`, "seven_day should have accounts_count")
				require.Contains(t, body, `"unknown_accounts_count"`, "seven_day should have unknown_accounts_count")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := newGatewayContractDeps(t)
			if tt.setup != nil {
				tt.setup(t, deps)
			}

			status, body := doGatewayRequest(t, deps.router, tt.method, tt.path, tt.body, tt.headers)
			require.Equal(t, tt.wantStatus, status)
			if tt.wantBody != nil {
				tt.wantBody(t, body)
			}
		})
	}
}

type gatewayContractDeps struct {
	router      http.Handler
	cfg         *config.Config
	apiKeyRepo  *stubApiKeyRepo
	groupRepo   *stubGroupRepo
	userSubRepo *stubUserSubscriptionRepo
	usageRepo   *stubUsageLogRepo
	settingRepo *stubSettingRepo
	redeemRepo  *stubRedeemCodeRepo
}

func newGatewayContractDeps(t *testing.T) *gatewayContractDeps {
	t.Helper()

	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	userRepo := &stubUserRepo{
		users: map[int64]*service.User{
			1: {
				ID:            1,
				Email:         "alice@example.com",
				Username:      "alice",
				Role:          service.RoleUser,
				Balance:       100.0,
				Concurrency:   5,
				Status:        service.StatusActive,
				AllowedGroups: nil,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			2: {
				ID:            2,
				Email:         "bob@example.com",
				Username:      "bob",
				Role:          service.RoleUser,
				Balance:       100.0,
				Concurrency:   5,
				Status:        service.StatusActive,
				AllowedGroups: nil,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		},
	}

	apiKeyRepo := newStubApiKeyRepo(now)
	_ = stubApiKeyCache{}

	apiKeyRepo.MustSeed(&service.APIKey{
		ID:        101,
		Key:       "sk-test-key-group-a",
		UserID:    1,
		GroupID:   ptrInt64(1),
		Name:      "Test Key Group A",
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})

	apiKeyRepo.MustSeed(&service.APIKey{
		ID:        102,
		Key:       "sk-test-key-group-b",
		UserID:    2,
		GroupID:   ptrInt64(2),
		Name:      "Test Key Group B",
		Status:    service.StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})

	groupRepo := &stubGroupRepo{}
	groupRepo.SetActive([]service.Group{
		{
			ID:       1,
			Name:     "group-a",
			Platform: service.PlatformAnthropic,
			Status:   service.StatusActive,
		},
		{
			ID:       2,
			Name:     "group-b",
			Platform: service.PlatformAnthropic,
			Status:   service.StatusActive,
		},
	})

	userSubRepo := &stubUserSubscriptionRepo{}
	redeemRepo := &stubRedeemCodeRepo{}
	settingRepo := newStubSettingRepo()
	settingRepo.SetAll(map[string]string{
		"ops_monitoring_enabled": "true",
	})

	cfg := &config.Config{
		Default: config.DefaultConfig{
			APIKeyPrefix: "sk-",
		},
		RunMode: config.RunModeStandard,
		Ops: config.OpsConfig{
			Enabled: false,
		},
	}

	_ = service.NewSubscriptionService(groupRepo, userSubRepo, nil, nil, cfg)
	usageRepo := newStubUsageLogRepo()
	_ = service.NewUsageService(usageRepo, userRepo, nil, nil)

	r := gin.New()

	apiKeyAuth := func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "invalid_request_error",
					"code":    "api_key_required",
					"message": "Missing API key",
				},
			})
			return
		}

		if len(authHeader) < 7 || authHeader[:7] != "Bearer " {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
					"message": "Invalid API key format",
				},
			})
			return
		}

		key := authHeader[7:]
		apiKey, err := apiKeyRepo.GetByKeyForAuth(context.Background(), key)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"type": "error",
				"error": gin.H{
					"type":    "invalid_request_error",
					"code":    "invalid_api_key",
					"message": "Invalid API key",
				},
			})
			return
		}

		c.Set("user", apiKeyAuthSubject{
			UserID:      apiKey.UserID,
			APIKeyID:    apiKey.ID,
			GroupID:     apiKey.GroupID,
			Concurrency: 5,
		})
		c.Set("api_key", apiKey)
		c.Next()
	}

	v1 := r.Group("/v1")
	v1.Use(apiKeyAuth)
	{
		v1.GET("/model-quotas", func(c *gin.Context) {
			apiKey, _ := middleware.GetAPIKeyFromContext(c)
			var groupID *int64
			var platform string
			if apiKey != nil && apiKey.Group != nil {
				groupID = &apiKey.Group.ID
				platform = apiKey.Group.Platform
			}
			_ = groupID
			_ = platform
			c.JSON(http.StatusOK, service.ModelQuotaResponse{
				Object: "list",
				Data: []service.ModelQuota{
					{
						ID:        "claude-sonnet-4-20250514",
						Object:    "model_quota",
						QuotaPool: "account_shared",
						FiveHour: service.ModelQuotaWindow{
							TotalUSD:             ptrFloat64(40.0),
							UsedUSD:              12.0,
							RemainingUSD:         ptrFloat64(28.0),
							AccountsCount:        1,
							UnknownAccountsCount: 0,
						},
						SevenDay: service.ModelQuotaWindow{
							TotalUSD:             ptrFloat64(120.0),
							UsedUSD:              3.0,
							RemainingUSD:         ptrFloat64(117.0),
							AccountsCount:        1,
							UnknownAccountsCount: 1,
						},
					},
				},
			})
		})
	}

	return &gatewayContractDeps{
		router:      r,
		cfg:         cfg,
		apiKeyRepo:  apiKeyRepo,
		groupRepo:   groupRepo,
		userSubRepo: userSubRepo,
		usageRepo:   usageRepo,
		settingRepo: settingRepo,
		redeemRepo:  redeemRepo,
	}
}

type apiKeyAuthSubject struct {
	UserID      int64
	APIKeyID    int64
	GroupID     *int64
	Concurrency int
}

func doGatewayRequest(t *testing.T, router http.Handler, method, path, body string, headers map[string]string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	respBody, err := io.ReadAll(w.Result().Body)
	require.NoError(t, err)

	return w.Result().StatusCode, string(respBody)
}

func assertNoAccountLeakage(t *testing.T, body string) {
	t.Helper()
	forbidden := []string{"account_id", "account_name", "credentials", "proxy", "user_id", "user_name"}
	for _, field := range forbidden {
		require.NotContains(t, body, fmt.Sprintf(`"%s"`, field), "response must not expose account details via field: %s", field)
	}
}

func ptrInt64(v int64) *int64 {
	return &v
}

func ptrFloat64(v float64) *float64 {
	return &v
}
