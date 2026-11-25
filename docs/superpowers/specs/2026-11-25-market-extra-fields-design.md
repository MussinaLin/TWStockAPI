# Market API 新增期貨未平倉 / VIX / 當沖比率欄位

## 目的

`/api/market` 回應加上 `market_daily` 已有的四個欄位：`txf_foreign_oi_net`、`txf_total_oi_net`、`tw_vix_close`、`day_trade_sell_amount_pct`。

## 範圍

- `/api/market` 與 `/api/market/:date` 共用 `marketColumns`，兩支一起加，維持列表與單日格式一致。
- `/api/market/dates` 不變。

## 欄位

| 欄位 | DB 型別 | 說明 |
|------|---------|------|
| `txf_foreign_oi_net` | bigint | 台指期外資未平倉淨口數（口） |
| `txf_total_oi_net` | bigint | 台指期三大法人合計未平倉淨口數（口） |
| `tw_vix_close` | numeric(8,2) | 台指 VIX 收盤 |
| `day_trade_sell_amount_pct` | numeric(6,4) | 當沖賣出金額佔比，比率（`0.2804` = 28.04%） |

- 原值回傳，不做換算。
- NULL 回 `null`（staging 有 2 天 `day_trade_sell_amount_pct` 為 NULL）。

## Approaches

只有一種合理做法：在 `marketColumns` 加欄位。另開 endpoint 或只加在其中一支，都會讓同一份資料有兩種格式，不採用。

## 變更

1. `routers/market.go`：`marketColumns` 加四個欄位。
2. `routers/market_test.go`：`marketKeys` 加四個 key。
3. `README.md`：兩支 API 範例 JSON 補欄位，加欄位說明。
4. `CLAUDE.md`：market 說明更新，並加部署前置。

## 部署前置

staging 已確認有欄位。PROD 未確認：部署前先用 `information_schema.columns` 確認 `market_daily` 有這四個欄位，缺任何一個，`/api/market` 與 `/api/market/:date` 會回 500。

## 已知資料狀況（不在本次處理）

`tw_vix_close` 在 staging 2026-11-23 → 11-24 從 55.36 降到 26.18，可能是 TWStockAnalysis 端資料問題。
