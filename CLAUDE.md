# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TWStockAPI-Gin is a read-only REST API for Taiwan stock market analysis data, built with Go/Gin. It queries a PostgreSQL database populated by the separate TWStockAnalysis batch processing project. Designed for deployment on Railway.

## Build & Run

```bash
# Run locally (reads .env for DATABASE_URL)
go run .

# Run via Railway
railway run go run .

# Build binary
go build -o server .
```

Requires `DATABASE_URL` env var (PostgreSQL connection string). Server listens on `PORT` (default `8080`).

## Architecture

- **`main.go`** — App entry point: loads `.env`, inits DB pool, registers middleware (`slog-gin`, `gin.Recovery`), mounts health checks and API route groups.
- **`db/db.go`** — PostgreSQL connection pool via `pgxpool` (min 2, max 10 connections).
- **`routers/`** — Route handlers organized by domain:
  - `stocks.go` — `/api/stocks` — stock master data
  - `daily.go` — `/api/daily` — daily OHLCV, technical indicators, institutional flows
  - `alpha.go` — `/api/alpha/pick/*` and `/api/alpha/sell/*` — stock picking signals and sell alerts
  - `helpers.go` — `rowsToMaps` converts pgx rows to `[]map[string]any` with type handling (dates → ISO strings, NaN/Inf → nil)

All endpoints are read-only SELECT queries. Response format is JSON. Dates are returned as ISO 8601 strings.


## Workflow

- **所有涉及 coding、架構規劃、寫程式的任務，一律請先設計完架構並釐清所有實作細節，有疑問的地方提出討論，沒問題再開始實作。** 不可以未經討論就直接動手寫 code。
- **每次改動如果涉及 API endpoint、請求/回應格式、資料表結構的變更，必須同步更新 `CLAUDE.md` 和 `README.md`，讓文件保持最新狀態。** 包括但不限於：新增/修改 API endpoint、新增/修改查詢邏輯、新增/修改資料表、變更回應欄位或格式。