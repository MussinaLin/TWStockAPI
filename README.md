---
title: TWStock API (Gin)
description: 台股分析資料 REST API Server (Go/Gin)
tags:
  - gin
  - golang
  - twstock
---

# TWStock API (Gin)

台股分析資料 REST API Server，使用 Go + [Gin](https://gin-gonic.com/) 框架。

資料來源為 PostgreSQL，由 [TWStockAnalysis](../TWStockAnalysis) 批次作業負責寫入。

[![Deploy on Railway](https://railway.app/button.svg)](https://railway.app/new/template/dTvvSf)

## 需求

- Go 1.23+
- PostgreSQL（由 TWStockAnalysis 的 `docker compose up -d` 啟動）

## 設定

```bash
cp .env.example .env
```

確保 `DATABASE_URL` 指向與 TWStockAnalysis 相同的 PostgreSQL。

## 啟動

```bash
# 本地開發
go run .

# 透過 Railway
railway run go run .
```

---

## API Endpoints

### Health

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | 健康檢查 |
| GET | `/health/db` | 資料庫連線檢查 |

#### `GET /health`

**Response:**

```json
{ "status": "ok" }
```

#### `GET /health/db`

**Response (200):**

```json
{ "status": "ok" }
```

**Response (503):**

```json
{ "status": "error", "detail": "error message" }
```

---

### Stocks

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/stocks` | 列出所有股票 |
| GET | `/api/stocks/:symbol` | 取得單一股票資訊 |

#### `GET /api/stocks`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `enabled` | bool | （無） | 篩選啟用狀態，例如 `?enabled=true` |

**Response (200):**

```json
[
  {
    "symbol": "2330",
    "name": "台積電",
    "enabled": true,
    "issued_shares": 25930380458
  }
]
```

#### `GET /api/stocks/:symbol`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `symbol` | string | 股票代號，例如 `2330` |

**Response (200):**

```json
{
  "symbol": "2330",
  "name": "台積電",
  "enabled": true,
  "issued_shares": 25930380458
}
```

**Response (404):**

```json
{ "error": "not found" }
```

---

### Daily Data

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/daily/dates` | 列出可用交易日 |
| GET | `/api/daily/:date` | 取得某日所有股票資料（含技術指標） |
| GET | `/api/daily/stock/:symbol` | 取得個股歷史資料（含技術指標） |

#### `GET /api/daily/dates`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | `30` | 回傳筆數上限（最大 365） |

**Response (200):**

```json
["2026-03-20", "2026-03-19", "2026-03-18"]
```

#### `GET /api/daily/:date`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `date` | string | 交易日期（ISO 格式 `YYYY-MM-DD`） |

**Response (200):**

```json
[
  {
    "symbol": "2330",
    "name": "台積電",
    "open": 590.0,
    "close": 595.0,
    "high": 598.0,
    "low": 588.0,
    "volume": 25000000,
    "turnover_rate": 0.0012,
    "foreign_net": 5000.0,
    "trust_net": 1200.0,
    "dealer_net": -300.0,
    "institutional_investors_net": 5900.0,
    "margin_balance": 12000.0,
    "short_balance": 3000.0,
    "short_margin_ratio": 0.25,
    "foreign_holding_pct": 72.5,
    "insti_holding_pct": 80.1,
    "vol_ma5": 23000000.0,
    "vol_ma10": 22000000.0,
    "vol_ma20": 21000000.0,
    "turnover_ma20": 0.0011,
    "foreign_net_5d_avg": 4500.0,
    "foreign_net_10d_avg": 3800.0,
    "foreign_net_15d_avg": 3200.0,
    "foreign_net_30d_avg": 2800.0,
    "rsi_9": 62.5,
    "rsi_14": 58.3,
    "macd": 1.25,
    "macd_signal": 0.98,
    "macd_hist": 0.27,
    "bb_upper": 610.0,
    "bb_middle": 592.0,
    "bb_lower": 574.0,
    "bb_percent_b": 0.75,
    "bb_bandwidth": 6.08
  }
]
```

#### `GET /api/daily/stock/:symbol`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `symbol` | string | 股票代號 |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | int | `60` | 回傳筆數上限（最大 365） |

**Response (200):**

與 `/api/daily/:date` 相同欄位，但無 `symbol` 和 `name`，多了 `trade_date`：

```json
[
  {
    "trade_date": "2026-03-20",
    "open": 590.0,
    "close": 595.0,
    "high": 598.0,
    "low": 588.0,
    "volume": 25000000,
    "turnover_rate": 0.0012,
    "foreign_net": 5000.0,
    "trust_net": 1200.0,
    "dealer_net": -300.0,
    "institutional_investors_net": 5900.0,
    "margin_balance": 12000.0,
    "short_balance": 3000.0,
    "short_margin_ratio": 0.25,
    "vol_ma5": 23000000.0,
    "vol_ma10": 22000000.0,
    "vol_ma20": 21000000.0,
    "turnover_ma20": 0.0011,
    "foreign_net_5d_avg": 4500.0,
    "foreign_net_10d_avg": 3800.0,
    "foreign_net_15d_avg": 3200.0,
    "foreign_net_30d_avg": 2800.0,
    "rsi_9": 62.5,
    "rsi_14": 58.3,
    "macd": 1.25,
    "macd_signal": 0.98,
    "macd_hist": 0.27,
    "bb_upper": 610.0,
    "bb_middle": 592.0,
    "bb_lower": 574.0,
    "bb_percent_b": 0.75,
    "bb_bandwidth": 6.08
  }
]
```

---

### Alpha Pick

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/alpha/pick/latest` | 最新選股結果 |
| GET | `/api/alpha/pick/dates` | 列出有選股結果的日期 |
| GET | `/api/alpha/pick/summary` | 選股摘要（出現頻率） |
| GET | `/api/alpha/pick/stock/:symbol` | 個股被選中的歷史紀錄 |
| GET | `/api/alpha/pick/:date` | 指定日期選股結果 |

#### `GET /api/alpha/pick/latest`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"alpha"` | 模式（`alpha` / `replay`） |

**Response (200):**

```json
{
  "trade_date": "2026-03-20",
  "count": 5,
  "picks": [
    {
      "symbol": "2330",
      "trade_date": "2026-03-20",
      "name": "台積電",
      "close": 595.0,
      "volume": 25000000,
      "vol_ma5": 23000000.0,
      "vol_ma10": 22000000.0,
      "vol_ma20": 21000000.0,
      "rsi_14": 58.3,
      "macd": 1.25,
      "macd_signal": 0.98,
      "macd_hist": 0.27,
      "bb_upper": 610.0,
      "bb_bandwidth": 6.08,
      "bb_percent_b": 0.75,
      "insti_net_5d_sum": 25000.0,
      "insti_net_5d_avg": 5000.0,
      "insti_net_10d_sum": 42000.0,
      "insti_net_10d_avg": 4200.0,
      "insti_net_15d_sum": 55000.0,
      "insti_net_15d_avg": 3666.7,
      "insti_net_30d_sum": 90000.0,
      "insti_net_30d_avg": 3000.0,
      "bb_bw_5d_avg": 5.8,
      "bb_bw_10d_avg": 6.1,
      "bb_bw_15d_avg": 6.5,
      "bb_bw_30d_avg": 7.0,
      "cond_insti": true,
      "cond_insti_bullish": true,
      "cond_rsi": true,
      "cond_macd": false,
      "cond_vol_ma10": true,
      "cond_vol_ma20": true,
      "cond_bb_narrow": false,
      "cond_bb_near_upper": false,
      "cond_turnover_surge": false,
      "reasons": "法人連續買超；RSI 回升"
    }
  ]
}
```

**Response (200, 無資料):**

```json
{ "trade_date": null, "picks": [] }
```

#### `GET /api/alpha/pick/dates`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"alpha"` | 模式（`alpha` / `replay`） |
| `limit` | int | `30` | 回傳筆數上限（最大 365） |

**Response (200):**

```json
["2026-03-20", "2026-03-19", "2026-03-18"]
```

#### `GET /api/alpha/pick/summary`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"alpha"` | 模式（`alpha` / `replay`） |

**Response (200):**

```json
[
  {
    "symbol": "2330",
    "name": "台積電",
    "pick_count": 12,
    "first_date": "2026-01-15",
    "last_date": "2026-03-20"
  }
]
```

#### `GET /api/alpha/pick/stock/:symbol`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `symbol` | string | 股票代號，例如 `2330` |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"alpha"` | 模式（`alpha` / `replay`） |

**Response (200):**

```json
{
  "symbol": "2330",
  "count": 3,
  "records": [
    {
      "trade_date": "2026-03-20",
      "symbol": "2330",
      "name": "台積電",
      "reasons": "法人連續買超；RSI 回升"
    }
  ]
}
```

#### `GET /api/alpha/pick/:date`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `date` | string | 交易日期（ISO 格式 `YYYY-MM-DD`） |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"alpha"` | 模式（`alpha` / `replay`） |

**Response (200):**

```json
{
  "trade_date": "2026-03-20",
  "count": 5,
  "picks": [
    {
      "symbol": "2330",
      "trade_date": "2026-03-20",
      "name": "台積電",
      "close": 595.0,
      "volume": 25000000,
      "rsi_14": 58.3,
      "macd_hist": 0.27,
      "bb_percent_b": 0.75,
      "insti_net_5d_sum": 25000.0,
      "insti_net_5d_avg": 5000.0,
      "insti_net_10d_sum": 42000.0,
      "insti_net_10d_avg": 4200.0,
      "insti_net_15d_sum": 55000.0,
      "insti_net_15d_avg": 3666.7,
      "insti_net_30d_sum": 90000.0,
      "insti_net_30d_avg": 3000.0,
      "cond_insti": true,
      "cond_insti_bullish": true,
      "cond_rsi": true,
      "cond_macd": false,
      "cond_vol_ma10": true,
      "cond_vol_ma20": true,
      "cond_bb_narrow": false,
      "cond_bb_near_upper": false,
      "cond_turnover_surge": false,
      "reasons": "法人連續買超；RSI 回升"
    }
  ]
}
```

---

### Sell Alerts

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/alpha/sell/latest` | 最新賣出警示 |
| GET | `/api/alpha/sell/summary` | 賣出摘要（出現頻率） |
| GET | `/api/alpha/sell/stock/:symbol` | 個股被賣出警示的歷史紀錄 |
| GET | `/api/alpha/sell/:date` | 指定日期賣出警示 |

#### `GET /api/alpha/sell/latest`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"sell"` | 模式 |

**Response (200):**

```json
{
  "trade_date": "2026-03-20",
  "count": 3,
  "sells": [
    {
      "symbol": "2330",
      "trade_date": "2026-03-20",
      "name": "台積電",
      "close": 595.0,
      "volume": 25000000,
      "vol_ma10": 22000000.0,
      "rsi_14": 78.5,
      "macd_hist": -0.15,
      "bb_percent_b": 0.92,
      "foreign_net_5d_sum": -12000.0,
      "foreign_net_5d_avg": -2400.0,
      "foreign_net_10d_sum": -18000.0,
      "foreign_net_10d_avg": -1800.0,
      "foreign_net_15d_sum": -20000.0,
      "foreign_net_15d_avg": -1333.3,
      "foreign_net_30d_sum": -25000.0,
      "foreign_net_30d_avg": -833.3,
      "trust_net_5d_sum": -5000.0,
      "trust_net_5d_avg": -1000.0,
      "trust_net_10d_sum": -8000.0,
      "trust_net_10d_avg": -800.0,
      "trust_net_15d_sum": -10000.0,
      "trust_net_15d_avg": -666.7,
      "trust_net_30d_sum": -12000.0,
      "trust_net_30d_avg": -400.0,
      "cond_foreign_sell": true,
      "cond_foreign_accel": false,
      "cond_trust_sell": true,
      "cond_trust_accel": false,
      "cond_high_black": false,
      "cond_price_up_vol_down": true,
      "cond_rsi_overbought": true,
      "cond_rsi_divergence": false,
      "cond_macd_turn_neg": true,
      "cond_macd_divergence": false,
      "cond_bb_below": false,
      "cond_macd_death_cross": false,
      "cond_margin_surge": false,
      "cond_turnover_surge": false,
      "cond_vol_surge_flat": false,
      "conditions_met": 5,
      "reasons": "外資連續賣超；RSI 過熱；MACD 轉負"
    }
  ]
}
```

**Response (200, 無資料):**

```json
{ "trade_date": null, "sells": [] }
```

#### `GET /api/alpha/sell/summary`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"sell"` | 模式 |

**Response (200):**

```json
[
  {
    "symbol": "2330",
    "name": "台積電",
    "sell_count": 8,
    "first_date": "2026-01-20",
    "last_date": "2026-03-20"
  }
]
```

#### `GET /api/alpha/sell/stock/:symbol`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `symbol` | string | 股票代號，例如 `2330` |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"sell"` | 模式 |

**Response (200):**

```json
{
  "symbol": "2330",
  "count": 2,
  "records": [
    {
      "trade_date": "2026-03-20",
      "symbol": "2330",
      "name": "台積電",
      "reasons": "外資連續賣超；RSI 過熱；MACD 轉負"
    }
  ]
}
```

#### `GET /api/alpha/sell/:date`

**Path Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `date` | string | 交易日期（ISO 格式 `YYYY-MM-DD`） |

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `mode` | string | `"sell"` | 模式 |

**Response (200):**

```json
{
  "trade_date": "2026-03-20",
  "count": 3,
  "sells": [
    {
      "symbol": "2330",
      "trade_date": "2026-03-20",
      "name": "台積電",
      "close": 595.0,
      "volume": 25000000,
      "vol_ma10": 22000000.0,
      "rsi_14": 78.5,
      "macd_hist": -0.15,
      "bb_percent_b": 0.92,
      "conditions_met": 5,
      "reasons": "外資連續賣超；RSI 過熱；MACD 轉負"
    }
  ]
}
```

---

### Trade Records

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/trade/trade-records` | 查詢交易紀錄 |

#### `GET /api/trade/trade-records`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `from` | string | 90 天前 | 起始日期（`YYYY-MM-DD`） |
| `to` | string | 今天 | 結束日期（`YYYY-MM-DD`） |

**Response (200):**

```json
{
  "count": 2,
  "records": [
    {
      "symbol": "2330",
      "name": "台積電",
      "type": "buy",
      "trade_date": "2026-03-20",
      "price": 595.0,
      "performance": 0.05
    }
  ]
}
```

---

## 錯誤回應

所有 API 在伺服器錯誤時回傳：

```json
{ "detail": "Internal server error" }
```

HTTP Status Code: `500`
