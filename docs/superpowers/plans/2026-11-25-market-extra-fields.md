# Market API 新增欄位 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `/api/market` 與 `/api/market/:date` 回應加上 `txf_foreign_oi_net`、`txf_total_oi_net`、`tw_vix_close`、`day_trade_sell_amount_pct`。

**Architecture:** 兩支 API 共用 `routers/market.go` 的 `marketColumns` 常數；在常數加欄位即可，`rowsToMaps` 已處理 numeric → float64、NULL → nil。

**Tech Stack:** Go / Gin / pgx v5；整合測試打 staging DB（`.env` 的 `DATABASE_URL`）。

**Spec:** `docs/superpowers/specs/2026-11-25-market-extra-fields-design.md`

## Global Constraints

- 原值回傳，不換算（`day_trade_sell_amount_pct` `0.2804` 回 `0.2804`）。
- NULL 回 `null`。
- `/api/market/dates` 不變。
- 欄位順序：接在 `foreign_net` 之後，依序 `txf_foreign_oi_net, txf_total_oi_net, tw_vix_close, day_trade_sell_amount_pct`。

## Review Focus

- 欄位為 NULL 的日期（staging 2026-11-20、11-23 的 `day_trade_sell_amount_pct`）→ 回 `null`，不是 `0` 或缺 key。→ Task 1 `TestGetMarketByDate_NullDayTradePct`
- `day_trade_sell_amount_pct` 被誤換算成百分比 → 與 DB 值相等。→ Task 1 `TestGetMarketByDate_ExtraFieldsMatchDB`
- 其他 input 類別（limit、日期格式、404）行為不變，由既有測試覆蓋。

---

### Task 1: 新增欄位 + 文件

**Files:**
- Modify: `routers/market.go`（`marketColumns`）
- Test: `routers/market_test.go`
- Modify: `README.md`（Market Daily 段落）
- Modify: `CLAUDE.md`（`market.go` 那一行）

**Interfaces:**
- Consumes: 既有 `doJSON`、`requireKeys`、`anyMarketDate`、`db.Pool()`
- Produces: 無

- [ ] **Step 1: 寫 failing tests（`routers/market_test.go`）**
  - `marketKeys` 加入四個 key。
  - `TestGetMarketByDate_ExtraFieldsMatchDB`：取 `anyMarketDate`，用 SQL 讀該日四個欄位（`::float8`，可 NULL → `*float64`），打 `/api/market/:date`，逐欄比對：DB NULL → `assert.Nil`；否則 `assert.InDelta(t, dbVal, m[key].(float64), 1e-9)`。
  - `TestGetMarketByDate_NullDayTradePct`：`SELECT MAX(trade_date)::text FROM market_daily WHERE day_trade_sell_amount_pct IS NULL`，NULL 就 `t.Skip`；打該日，`requireKeys(t, m, "day_trade_sell_amount_pct")` 且 `assert.Nil(t, m["day_trade_sell_amount_pct"])`。

- [ ] **Step 2: 跑測試確認 fail**

Run: `go test ./routers -run 'Market' -v`
Expected: `TestListMarketDaily_OK`、`TestGetMarketByDate_OK`、兩個新測試 FAIL（缺 key）。

- [ ] **Step 3: `marketColumns` 加四個欄位（順序見 Global Constraints）**

- [ ] **Step 4: 跑測試確認 pass**

Run: `go test ./routers -run 'Market' -v` → 全 PASS；再跑 `go vet ./... && go test ./...` → PASS。

- [ ] **Step 5: 更新文件**
  - `README.md`：`GET /api/market` 與 `GET /api/market/:date` 範例 JSON 加四個欄位（值用 staging 2026-11-24：`-103182`、`-5401`、`26.18`、`0.2804`）；在 `GET /api/market` 範例下加欄位說明表（欄位、型別、說明：單位口 / 指數 / 比率 `0.2804` = 28.04%、可能為 `null`）。
  - `CLAUDE.md`：`market.go` 那行補上新欄位，並加 **部署前置**：先用 `information_schema.columns` 確認 PROD `market_daily` 有 `txf_foreign_oi_net`、`txf_total_oi_net`、`tw_vix_close`、`day_trade_sell_amount_pct`，缺任何一個 `/api/market` 與 `/api/market/:date` 會回 500。

- [ ] **Step 6: Commit**（依使用者全域規則，不加 `Co-Authored-By`）

```bash
git add routers/market.go routers/market_test.go README.md CLAUDE.md docs/superpowers/specs/2026-11-25-market-extra-fields-design.md docs/superpowers/plans/2026-11-25-market-extra-fields.md
git commit -m "feat: add futures OI, VIX and day-trade pct to market API"
```
