# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TWStockAPI-Gin is a REST API for Taiwan stock market analysis data, built with Go/Gin. It queries a PostgreSQL database populated by the separate TWStockAnalysis batch processing project; the only writes are per-user favorites (`/api/favorites`). Designed for deployment on Railway.

## Build & Run

```bash
# Run locally (reads .env for DATABASE_URL)
go run .

# Run via Railway
railway run go run .

# Build binary
go build -o server .
```

Requires `DATABASE_URL` env var (PostgreSQL connection string). Server listens on `PORT` (default `8080`). `JWT_SECRET`（必須與 AccountService 相同）：`/api/favorites` 一律需要，其他 `/api` 在啟用 JWT 驗證時才需要；沒設時啟動只會記 warning，`/api/favorites` 會一律回 500。

## Architecture

- **`main.go`** — App entry point: loads `.env`, inits DB pool, registers middleware (`slog-gin`, `gin.Recovery`, CORS allowing `GET` / `POST` / `PUT` / `DELETE` and the `Authorization` header), mounts health checks and API route groups; the `/api` group runs `auth.RequireAuth`; `/api/favorites` additionally runs `auth.RequireLogin`.
- **`db/db.go`** — PostgreSQL connection pool via `pgxpool` (min 2, max 10 connections).
- **`db/*.sql`** — 需手動在 DB 執行的 SQL。可重複執行：`config.sql`（`JWT_TOKEN_ENABLE`）、`user_favorites.sql`（`user_config`、`user_favorite_stocks`、`user_favorite_groups` 與 `FAVORITE_GROUPS` 初始值；部署新版程式前先跑；已套用群組版 schema 的環境重跑一次即可加上 `user_favorite_groups`）。**不可重複執行**：`user_favorites_v2_migration.sql`（v1 單一清單 → v2 群組，會刪除所有我的最愛資料；只在套用過 v1 的環境跑一次，staging 已跑過，production 不需要）。
- **`auth/`** — JWT 驗證 filter，掛在 `/api` group（`/health*` 不驗證）。開關為 DB `config` 表的 `JWT_TOKEN_ENABLE`（值為 `'true'` 才啟用；沒有這一列視為 false；60 秒記憶體快取）。`db/config.sql` 建立這一列（預設 `false`）。
  - 啟用時需帶 `Authorization: Bearer <jwt>`（AccountService 簽發，HS256，`JWT_SECRET` 須相同）。`sub` = `users.id`，查 `users` 表取得 email，以 `auth.User{ID, Email}` 存入 `gin.Context`（key `auth.ContextUserKey`）。
  - 過期回 401 `{"error":"token expired"}`；其他驗證失敗回 401 `{"error":"unauthorized"}`；讀 config / users 出錯或未設 `JWT_SECRET` 回 500。
  - `RequireLogin`：不論開關一律要求有效 JWT（context 已有 user 時直接放行，不重查 DB），目前只掛在 `/api/favorites`。handler 用 `auth.CurrentUser(c)` 取得 user。
  - `jwt.go`（`ParseToken`）、`flag.go`（`FlagCache`、`LoadJWTFlag`）、`user.go`（`PgxUserStore`）、`middleware.go`（`RequireAuth`、`RequireLogin`、`CurrentUser`）。
- **`routers/`** — Route handlers organized by domain:
  - `stocks.go` — `/api/stocks` — stock master data
  - `daily.go` — `/api/daily` — daily OHLCV, technical indicators, institutional flows；`/api/daily/:date` 與 `/api/daily/stock/:symbol` 回應含 `price_limit_up` / `price_limit_down`（bool，`close` 是否等於 `stock_daily_raw.limit_up` / `limit_down`；欄位為 NULL 時回 `false`），以及 `stock_daily_statistics` 的外資 `foreign_net_{5,10,15,30}d_avg` 與投信 `trust_net_{5,10,15,30}d_avg`（買賣超 N 日平均）
  - `alpha.go` — `/api/alpha/pick/*` and `/api/alpha/sell/*` — stock picking signals and sell alerts; pick responses（latest / stock/:symbol / :date）include `pick_type`（`breakout` / `dip` / `re_entry`）
  - `trade.go` — `/api/trade/trade-records` — trade records with date range filtering (default 90 days), includes summary stats (profit_count, loss_count, avg_performance, win_rate)
  - `market.go` — `/api/market`, `/api/market/dates`, `/api/market/:date` — TAIEX 大盤每日資料（OHLC、總成交量、融資餘額、外資買賣超）
  - `period.go` — `/api/period/holding/:symbol` — 集保持股比例（stock_holder_percent, weekly）；回傳 major_ratio（大戶比例）、retail_ratio（散戶比例）；支援 `?limit`，未帶 limit 時回傳全部
  - `favorites.go` / `favorites_config.go` — `/api/favorites` — 使用者的我的最愛（`user_favorite_stocks`、`user_favorite_groups`），一律需要登入。分成固定數量的群組（`group_no` 從 1 開始；預設名稱「群組一、群組二……」由 `groupName` 產生；不建群組表，`user_favorite_groups` 只存改過名的群組），同一檔可以放在多個群組。群組數與每組上限依 `users.member_level`，設定在 `user_config` 的 `FAVORITE_GROUPS`（JSONB，例如 `{"0":{"groups":5,"stocks_per_group":5}}`；找不到 level 或格式錯誤回 500）。`GET /api/favorites` 回傳 `{max_groups, max_stocks_per_group, groups[{group_no,name,count,items[{symbol,name,created_time}]}]}`（包括空的群組；`name` 有自訂名稱時回傳自訂名稱；群組內 `created_time DESC`）；`POST /api/favorites/groups/:group_no` `{"symbol"}`：201 新增、200 已存在、400 格式錯誤、404 `group not found` / `stock not found`、409 `favorite group full`；`PUT /api/favorites/groups/:group_no` `{"name"}`：200 `{group_no,name}`、400（名稱由 `normalizeGroupName` 檢查：去掉前後空白後 1–10 個字元、不含控制字元）、404 `group not found`；不同群組可重名、沒有還原預設名稱的方式；用 `INSERT ... ON CONFLICT DO UPDATE` 寫入，不加鎖；`DELETE /api/favorites/groups/:group_no/:symbol` 回 204（`group_no` 不是 ≥ 1 的整數回 400、群組不存在回 404）。降級時超過目前群組數的資料與自訂名稱保留但不顯示。POST 在 transaction 裡用 `SELECT ... FOR UPDATE` 鎖住 user 的 row。
  - `helpers.go` — `rowsToMaps` converts pgx rows to `[]map[string]any` with type handling (dates → ISO strings, NaN/Inf → nil)

All endpoints except `/api/favorites` are read-only SELECT queries. Response format is JSON. Dates are returned as ISO 8601 strings (`created_time` in favorites is a full RFC 3339 timestamp).


## Workflow

- **所有涉及 coding、架構規劃、寫程式的任務，一律請先設計完架構並釐清所有實作細節，有疑問的地方提出討論，沒問題再開始實作。** 不可以未經討論就直接動手寫 code。
- **每次改動如果涉及 API endpoint、請求/回應格式、資料表結構的變更，必須同步更新 `CLAUDE.md` 和 `README.md`，讓文件保持最新狀態。** 包括但不限於：新增/修改 API endpoint、新增/修改查詢邏輯、新增/修改資料表、變更回應欄位或格式。