# Market API 新增底部 / 頭部訊號欄位

## 目的

`/api/market` 回應加上兩個大盤訊號 bool 欄位，資料來自 `market_daily_signal`（TWStockAnalysis 產生）：

- `sig_bottom`：`market_daily_signal.sig_bottom` 或 `sig_retail_exit` 任一為 true → `true`
- `sig_top`：`market_daily_signal.sig_top` 為 true → `true`

## 範圍

- `/api/market` 與 `/api/market/:date` 共用 `marketColumns`，兩支一起加，維持列表與單日格式一致。
- `/api/market/dates` 只回日期陣列，不變。
- 不回傳 `market_daily_signal` 其他欄位（`cond_*`、`*_score`、原始 `sig_retail_exit`）。

## 資料表

`market_daily_signal`：PK `trade_date`；`sig_bottom`、`sig_retail_exit`、`sig_top` 皆 `boolean NOT NULL DEFAULT false`。
staging（2026-11-27）：與 `market_daily` 日期 1:1 對齊（1187 筆，2022-01-03 ~ 2026-11-26）；`sig_bottom` 18 天、`sig_retail_exit` 33 天、`sig_top` 119 天，底與頂沒有同一天都為 true。

## Approaches

- **LEFT JOIN + COALESCE false（採用）**：`market_daily` 為主，signal 缺列時兩欄回 `false`；與 daily 的 `price_limit_up` NULL → `false` 一致。
- INNER JOIN：signal 批次晚跑或缺某天時，該天大盤資料整筆消失，不採用。
- 缺列回 `null`：前端可分辨「沒資料」，但使用者選擇 `false`。

## 變更

1. `routers/market.go`：
   - FROM 改為 `market_daily m LEFT JOIN market_daily_signal s USING (trade_date)`。
   - `marketColumns` 原有欄位加 `m.` 前綴（signal 表也有 `taiex_close`，不加會 ambiguous）；`trade_date` 因 `USING` 可不加。
   - 新增 `COALESCE(s.sig_bottom OR s.sig_retail_exit, false) AS sig_bottom`、`COALESCE(s.sig_top, false) AS sig_top`，接在 `day_trade_sell_amount_pct` 之後。
2. `routers/market_test.go`：`marketKeys` 加兩個 key；新增測試把回應值與 DB 的 OR 結果比對。
3. `README.md`：兩支 API 範例 JSON 補欄位，加欄位說明。
4. `CLAUDE.md`：market 說明更新，並加部署前置。

## 部署前置

PROD 未確認：部署前先確認 PROD 有 `market_daily_signal` 表，且有 `sig_bottom`、`sig_retail_exit`、`sig_top` 欄位，缺任何一個，`/api/market` 與 `/api/market/:date` 會回 500。
