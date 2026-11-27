# Market API 新增底部 / 頭部訊號 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/api/market` 與 `/api/market/:date` 回應加上 bool 欄位 `sig_bottom`（`sig_bottom OR sig_retail_exit`）與 `sig_top`，來源為 `market_daily_signal`。

**Architecture:** 兩支 API 共用 `routers/market.go` 的 `marketColumns`；FROM 改成 `market_daily m LEFT JOIN market_daily_signal s USING (trade_date)`，在 SQL 用 `COALESCE(..., false)` 算出兩個 bool。`rowsToMaps` 對 bool 原樣回傳（`convertValue` default 分支）。

**Tech Stack:** Go / Gin / pgx v5；整合測試打 staging DB（`.env` 的 `DATABASE_URL`）。

**Spec:** `docs/superpowers/specs/2026-11-27-market-signals-design.md`

## Global Constraints

- `sig_bottom` = `market_daily_signal.sig_bottom OR market_daily_signal.sig_retail_exit`；`sig_top` = `market_daily_signal.sig_top`。
- 回傳 JSON bool，永不為 `null`；`market_daily_signal` 沒有該日資料時兩欄皆 `false`。
- 以 `market_daily` 為主（LEFT JOIN），signal 缺列不可讓大盤資料消失。
- 欄位順序：接在 `day_trade_sell_amount_pct` 之後，依序 `sig_bottom, sig_top`。
- 不回傳 signal 表其他欄位（`cond_*`、`*_score`、`sig_retail_exit` 本身）。
- `/api/market/dates` 不變。

## Review Focus

- 只有 `sig_retail_exit` 為 true、`sig_bottom` 為 false 的日期 → API `sig_bottom` 須為 `true`。→ Task 1 `TestGetMarketByDate_RetailExitCountsAsBottom`
- `market_daily` 有、`market_daily_signal` 沒有的日期 → 該日仍回傳，兩欄 `false`（staging 目前 1:1，測試找不到就 skip）。→ Task 1 `TestGetMarketByDate_NoSignalRowIsFalse`
- 欄位名稱衝突：signal 表也有 `taiex_close`，未加 `m.` 前綴會 ambiguous → 500。→ 既有 `TestListMarketDaily_OK` / `TestGetMarketByDate_OK` 回 200 即涵蓋。
- 列表整體值正確（不只最新一天）→ Task 1 `TestListMarketDaily_SignalsMatchDB`
- 其他 input 類別（limit、日期格式、404）行為不變，由既有測試覆蓋。

---

### Task 1: 新增訊號欄位 + 文件

**Files:**
- Modify: `routers/market.go`（`marketColumns`、`listMarketDaily`、`getMarketByDate` 的 FROM / WHERE / ORDER BY）
- Test: `routers/market_test.go`
- Modify: `README.md`（Market Daily 段落）
- Modify: `CLAUDE.md`（`market.go` 那一行）

**Interfaces:**
- Consumes: 既有 `doJSON`、`requireKeys`、`anyMarketDate`、`db.Pool()`
- Produces: 無

- [ ] **Step 1: 寫 failing tests（`routers/market_test.go`）**
  - `marketKeys` 加入 `"sig_bottom", "sig_top"`。
  - 預期值一律在 Go 端由原始欄位算（不要在測試 SQL 寫 `OR` / `COALESCE`，避免複製實作）：讀 `SELECT trade_date::text, sig_bottom, sig_retail_exit, sig_top FROM market_daily_signal`，`wantBottom = b || rx`、`wantTop = t`，map 裡找不到的日期兩者為 `false`。
  - `TestListMarketDaily_SignalsMatchDB`：打 `/api/market?limit=1500`，每列 `m["sig_bottom"]`、`m["sig_top"]` 須為 `bool`（`require.IsType(t, true, ...)`），並等於上述預期值。
  - `TestGetMarketByDate_RetailExitCountsAsBottom`：`SELECT MAX(trade_date)::text FROM market_daily_signal WHERE sig_retail_exit AND NOT sig_bottom`，NULL 就 `t.Skip`；打 `/api/market/:date`，`assert.Equal(t, true, m["sig_bottom"])`。
  - `TestGetMarketByDate_NoSignalRowIsFalse`：`SELECT MAX(m.trade_date)::text FROM market_daily m WHERE NOT EXISTS (SELECT 1 FROM market_daily_signal s WHERE s.trade_date = m.trade_date)`，NULL 就 `t.Skip`；打該日，200，`assert.Equal(t, false, m["sig_bottom"])`、`assert.Equal(t, false, m["sig_top"])`。

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./routers -run 'Market' -v`
Expected: `TestListMarketDaily_OK`、`TestGetMarketByDate_OK`、`TestListMarketDaily_SignalsMatchDB`、`TestGetMarketByDate_RetailExitCountsAsBottom` FAIL（缺 key）；`TestGetMarketByDate_NoSignalRowIsFalse` SKIP。

- [ ] **Step 3: 改 `routers/market.go`**
  - `marketColumns`：原欄位除 `trade_date` 外加 `m.` 前綴（`trade_date` 靠 `USING` 不需前綴），最後加 `COALESCE(s.sig_bottom OR s.sig_retail_exit, false) AS sig_bottom, COALESCE(s.sig_top, false) AS sig_top`。
  - 新增常數 `marketFrom = "market_daily m LEFT JOIN market_daily_signal s USING (trade_date)"`，`listMarketDaily`、`getMarketByDate` 的 FROM 改用它；`ORDER BY trade_date`、`WHERE trade_date = $1` 不變。
  - `listMarketDates` 不動。

- [ ] **Step 4: 跑測試確認 pass**

Run: `go test ./routers -run 'Market' -v` → 全 PASS（`NoSignalRowIsFalse` 可 SKIP）；再跑 `go vet ./... && go test ./...` → PASS。

- [ ] **Step 5: 更新文件**
  - `README.md`：`GET /api/market` 與 `GET /api/market/:date` 範例 JSON 在 `day_trade_sell_amount_pct` 後加 `"sig_bottom": false, "sig_top": true`；在 `GET /api/market` 欄位說明後加一張「訊號」表：`sig_bottom`（bool，底部訊號：`market_daily_signal.sig_bottom` 或 `sig_retail_exit` 任一為 true）、`sig_top`（bool，頭部訊號：`market_daily_signal.sig_top`），並註明該日沒有訊號資料時為 `false`、不會是 `null`。
  - `CLAUDE.md`：`market.go` 那行補上「以及 `market_daily_signal` 的 `sig_bottom`（`sig_bottom OR sig_retail_exit`）/ `sig_top`（bool，LEFT JOIN，沒有資料時 `false`）」，並在部署前置加：先確認 PROD 有 `market_daily_signal` 表與 `sig_bottom`、`sig_retail_exit`、`sig_top` 欄位，缺任何一個 `/api/market` 與 `/api/market/:date` 會回 500。

- [ ] **Step 6: Commit**（依使用者全域規則，不加 `Co-Authored-By`）

```bash
git add routers/market.go routers/market_test.go README.md CLAUDE.md docs/superpowers/specs/2026-11-27-market-signals-design.md docs/superpowers/plans/2026-11-27-market-signals.md
git commit -m "feat: add bottom and top signals to market API"
```
