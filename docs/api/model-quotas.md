# Model Quotas API

## 概述

`GET /v1/model-quotas` 端点用于查询当前 API Key 所属分组下各模型的聚合配额估算。

配额数据从分组关联的多个账户聚合计算而来，每个模型返回两个时间窗口的配额信息：5 小时窗口和 7 天窗口。配额基于各账户的用量百分比和已消耗金额推算得出。

此端点适用于需要了解分组下各模型可用配额的场景，帮助 API 使用者评估剩余调用额度。

## 认证

此端点需要 API Key 认证，支持以下三种方式：

| 头部 | 必填 | 说明 |
|------|------|------|
| Authorization | 是 | Bearer `<API_KEY>` |
| x-api-key | 是 | API Key 值本身 |
| x-goog-api-key | 是 | Gemini CLI 兼容的 API Key 方式 |

不支持在 URL query parameter 中传递 API Key（已废弃）。

**计费跳过行为**：此端点与 `/v1/usage` 类似，**跳过计费执行**。这意味着即使用户的订阅已过期、配额已耗尽或余额不足，已认证的 API Key 仍可访问此端点查询配额数据。认证、用户状态检查、IP 限制仍然正常执行。

## 请求

### HTTP 方法

`GET`

### 端点路径

`/v1/model-quotas`

### 请求头

| 头部 | 必填 | 说明 |
|------|------|------|
| Authorization | 是 | Bearer `<API_KEY>` |

### 请求示例

```bash
curl -X GET "https://api.example.com/v1/model-quotas" \
  -H "Authorization: Bearer sk-xxxxxxxx"
```

```bash
curl -X GET "https://api.example.com/v1/model-quotas" \
  -H "x-api-key: sk-xxxxxxxx"
```

## 响应

### 响应结构

响应是 OpenAI 风格的列表信封格式：

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-sonnet-4-20250514",
      "object": "model_quota",
      "quota_pool": "account_shared",
      "five_hour": {
        "total_usd": 10.0,
        "used_usd": 3.5,
        "remaining_usd": 6.5,
        "accounts_count": 2,
        "unknown_accounts_count": 0
      },
      "seven_day": {
        "total_usd": 100.0,
        "used_usd": 42.0,
        "remaining_usd": 58.0,
        "accounts_count": 2,
        "unknown_accounts_count": 0
      }
    }
  ]
}
```

### 响应示例

**正常响应**（所有账户均有完整的遥测数据）：

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-opus-4-20250514",
      "object": "model_quota",
      "quota_pool": "account_shared",
      "five_hour": {
        "total_usd": 10.0,
        "used_usd": 3.5,
        "remaining_usd": 6.5,
        "accounts_count": 2,
        "unknown_accounts_count": 0
      },
      "seven_day": {
        "total_usd": 100.0,
        "used_usd": 42.0,
        "remaining_usd": 58.0,
        "accounts_count": 2,
        "unknown_accounts_count": 0
      }
    },
    {
      "id": "claude-sonnet-4-20250514",
      "object": "model_quota",
      "quota_pool": "account_shared",
      "five_hour": {
        "total_usd": 5.0,
        "used_usd": 1.2,
        "remaining_usd": 3.8,
        "accounts_count": 1,
        "unknown_accounts_count": 0
      },
      "seven_day": {
        "total_usd": 50.0,
        "used_usd": 15.5,
        "remaining_usd": 34.5,
        "accounts_count": 1,
        "unknown_accounts_count": 0
      }
    }
  ]
}
```

**部分未知遥测的响应**（存在遥测数据缺失的账户）：

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-opus-4-20250514",
      "object": "model_quota",
      "quota_pool": "account_shared",
      "five_hour": {
        "total_usd": null,
        "used_usd": 5.5,
        "remaining_usd": null,
        "accounts_count": 3,
        "unknown_accounts_count": 1
      },
      "seven_day": {
        "total_usd": null,
        "used_usd": 48.0,
        "remaining_usd": null,
        "accounts_count": 3,
        "unknown_accounts_count": 1
      }
    }
  ]
}
```

**空响应**（分组下没有账户或账户没有配置模型映射）：

```json
{
  "object": "list",
  "data": null
}
```

### 字段说明

| 字段路径 | 类型 | 说明 |
|----------|------|------|
| object | string | OpenAI 风格列表信封，值为 `"list"` |
| data | array | 模型配额数组，每个元素代表一个模型的配额估算 |
| data[].id | string | 模型名称/标识符 |
| data[].object | string | 条目类型标识，值为 `"model_quota"` |
| data[].quota_pool | string | 配额池类型，值为 `"account_shared"`。表示该模型的配额来自共享账户池，多个模型可能共用同一组底层账户 |
| data[].five_hour | object | 5 小时时间窗口的配额信息 |
| data[].seven_day | object | 7 天时间窗口的配额信息 |
| data[].five_hour.total_usd | number, null | 推算的总配额（USD）。当存在未知遥测数据时为 `null` |
| data[].five_hour.used_usd | number | 已使用的金额（USD），累加所有账户的用量 |
| data[].five_hour.remaining_usd | number, null | 剩余配额（USD）。当存在未知遥测数据时为 `null` |
| data[].five_hour.accounts_count | int | 贡献此模型配额的账户数量 |
| data[].five_hour.unknown_accounts_count | int | 遥测数据缺失的账户数量 |
| data[].seven_day | object | 7 天时间窗口的配额结构，同 five_hour |

## 配额计算逻辑

### 基础公式

当账户拥有有效的遥测数据时（即 `used_usd > 0` 且 `used_percent > 0`），按以下公式推算：

```
total_usd = used_usd / (used_percent / 100)
remaining_usd = max(0, total_usd - used_usd)
```

**示例**：

- 某账户 5 小时内消耗了 $3.50，配额使用了 35%
- 推算总配额：`3.50 / 0.35 = $10.00`
- 剩余配额：`10.00 - 3.50 = $6.50`

### 遥测数据缺失的处理

以下情况会导致 `total_usd` 和 `remaining_usd` 为 `null`：

1. **缺少百分比数据**：账户的遥测数据中不存在用量百分比字段
2. **零百分比**：账户的用量百分比为 0 或无效值
3. **零用量但有百分比**：账户的 `used_usd` 为 0 但百分比大于 0（无法推算总量）

当存在任何未知遥测的账户时，该时间窗口的 `total_usd` 和 `remaining_usd` 均设为 `null`，但 `used_usd` 仍会累加所有账户的已消耗金额。

### 多账户聚合

多个提供同一模型服务的账户会被聚合计算：

- `accounts_count` 表示贡献此模型配额的账户总数
- `used_usd` 是所有账户已消耗金额的总和
- `total_usd` 和 `remaining_usd` 是所有账户推算值的总和
- 当任意一个账户的遥测数据缺失时，整个模型的这两个字段均为 `null`

## 重要注意事项

### 1. 共享账户池语义

`quota_pool: "account_shared"` 表示提供者使用百分比代表共享账户配额池。这意味着：

- 多个模型可能由同一组底层账户支撑
- 各模型的配额是可用性估算，而非独立可消费的池
- **简单将所有模型的 `remaining_usd` 相加会高估实际可用配额**，因为同一账户的消耗可能被多个模型重复计算

### 2. 未知遥测数据

当某些账户的配额遥测数据不可用时：

- 对应时间窗口的 `total_usd` 和 `remaining_usd` 为 `null`
- `used_usd` 仍会正常累加所有账户的用量
- `unknown_accounts_count` 会指明有多少账户缺少遥测数据

### 3. 缓存机制

响应在内存中缓存 **15 秒**。短时间内重复请求会返回相同的缓存结果。如果配额数据刚更新，可能需要等待缓存过期才能看到最新数据。

### 4. 仅限 5H 和 7D 窗口

此端点仅提供 5 小时和 7 天两个时间窗口的配额估算，不提供其他时间范围（如 24 小时、30 天等）的配额数据。

### 5. 不预测重置时间

响应中不包含配额重置时间。如果配额按固定周期重置，需要自行推断或咨询服务商了解重置策略。

### 6. 不触发上游刷新

此端点不会调用上游提供商或刷新配额遥测数据。它仅基于现有的遥测记录进行计算。

### 7. 无账户详情

响应中不包含任何账户 ID、账户名称、账户凭证或账户代理信息。`accounts_count` 仅表示贡献配额的账户数量。

### 8. 分组隔离

返回的配额数据仅限于当前 API Key 所属分组。不同分组的配额数据相互隔离，无法跨分组查询。

## 错误响应

### 401 Unauthorized — 缺少或无效的 API Key

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "API_KEY_REQUIRED",
    "message": "API key is required in Authorization header (Bearer scheme), x-api-key header, or x-goog-api-key header"
  }
}
```

### 401 Unauthorized — 无效的 API Key

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "INVALID_API_KEY",
    "message": "Invalid API key"
  }
}
```

### 400 Bad Request — Query Parameter 中的 API Key（已废弃）

```json
{
  "error": {
    "type": "invalid_request_error",
    "code": "api_key_in_query_deprecated",
    "message": "API key in query parameter is deprecated. Please use Authorization header instead."
  }
}
```

## 与 /v1/models 的关系

| 方面 | /v1/models | /v1/model-quotas |
|------|------------|------------------|
| 用途 | 列出可用的模型 | 查询模型的配额估算 |
| 返回内容 | 模型列表和元信息 | 各模型的已用/剩余配额 |
| 时间维度 | 无时间概念 | 5小时窗口、7天窗口 |
| 适用场景 | 了解支持哪些模型 | 评估可用配额、规划用量 |

`/v1/models` 告知你**有哪些模型可用**，而 `/v1/model-quotas` 告诉你**各模型的剩余额度**。两者互为补充，在发起请求前可以先用 `/v1/model-quotas` 检查配额，再用 `/v1/models` 确认模型可用性。

## 代码参考

- Handler: `backend/internal/handler/gateway_handler.go` — `ModelQuotas` 方法
- Service: `backend/internal/service/model_quota.go` — `GetModelQuotas` 方法
- 认证中间件: `backend/internal/server/middleware/api_key_auth.go` — `skipBilling` 逻辑
- 路由注册: `backend/internal/server/routes/gateway.go` — `/v1/model-quotas` 路由
