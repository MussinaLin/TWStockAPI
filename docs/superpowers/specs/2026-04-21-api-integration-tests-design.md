# API Integration Tests — Design

**Date:** 2026-04-21
**Status:** Approved
**Scope:** Add integration tests covering every public HTTP route in TWStockAPI-Gin, exercising the real PostgreSQL dev database.

## Goal

Provide a regression-protective test suite that:
- Hits every route registered in `main.go`.
- Catches column-rename, JSON-envelope, and date-format regressions.
- Includes an explicit guard against the `reasons` field re-appearing in `/alpha/pick` and `/alpha/sell` responses.
- Runs against the developer's existing `.env` Postgres DB (no separate test DB, no fixtures).
- Skips cleanly when `DATABASE_URL` is unset, so `go test ./...` is safe in any environment.

## Decisions (settled during brainstorm)

| # | Decision | Rationale |
|---|---|---|
| Q1 | **Use the existing `.env` dev DB read-only.** Tests check shape, not exact values. | Simplest path; no schema duplication; the DB is owned by the separate TWStockAnalysis project. |
| Q2 | **Hybrid input strategy.** Discover real symbols/dates dynamically for happy-path tests; hard-code sentinels for negative tests. | Discovery → robust against changing dev data. Hard-coded `__INVALID_XX__` / `1900-01-01` → guaranteed negatives. |
| Q3 | **Status + JSON shape + light value sanity.** Verify status, expected keys, types; add cheap sanity (`count == len(picks)`, dates parse, win_rate ∈ [0,1]). | Catches the regressions that matter; avoids drifting into business-rule assertions. |
| Q4 | **testify + skip-on-no-DB + co-located `*_test.go`.** | Terser assertions, conventional Go integration pattern, tests next to handlers. |
| Health | **`/health` and `/health/db` are not tested.** | Trivial endpoints; no signal worth the boilerplate. |

## Scope

**13 routes covered. `main.go` is untouched.**

| File | Endpoints |
|---|---|
| `routers/stocks_test.go` | `GET /api/stocks`, `GET /api/stocks/:symbol` |
| `routers/daily_test.go` | `GET /api/daily/dates`, `GET /api/daily/:date`, `GET /api/daily/stock/:symbol` |
| `routers/alpha_test.go` | `GET /api/alpha/pick/{latest,dates,summary,stock/:symbol,:date}` and `GET /api/alpha/sell/{latest,summary,stock/:symbol,:date}` |
| `routers/trade_test.go` | `GET /api/trade/trade-records` |
| `routers/setup_test.go` | `TestMain`, shared helpers (no `Test*` funcs) |

## Architecture

### Shared scaffolding — `routers/setup_test.go`

**`TestMain(m *testing.M)`** — single point of pool init/teardown.

```go
func TestMain(m *testing.M) {
    _ = godotenv.Load("../.env")
    if os.Getenv("DATABASE_URL") == "" {
        os.Exit(0) // skip whole package, exit clean
    }
    if err := db.InitPool(); err != nil {
        fmt.Fprintln(os.Stderr, "db init failed:", err)
        os.Exit(1)
    }
    defer db.ClosePool()
    os.Exit(m.Run())
}
```

If `DATABASE_URL` is unset → exit 0 (package skipped, `go test ./...` stays green). If it's set but the DB is unreachable → exit 1 (real failure, surfaces loudly).

**`newTestRouter() *gin.Engine`** — mirrors `main.go`'s setup without middleware.

```go
func newTestRouter() *gin.Engine {
    gin.SetMode(gin.TestMode)
    r := gin.New()
    api := r.Group("/api")
    RegisterStocks(api)
    RegisterDaily(api)
    RegisterAlpha(api)
    RegisterTrade(api)
    return r
}
```
(Test file is `package routers`, so `RegisterX` calls are unqualified.)
Skipping `slog-gin`, recovery, and CORS keeps test output clean.

**`doJSON(t, r, method, path) (status int, decoded any)`**

Uses `httptest.NewRecorder` + `r.ServeHTTP`. Decodes the body as `any` so each test can downcast to `map[string]any` or `[]any` per its endpoint. Calls `t.Helper()`.

**Discovery helpers** — return `t.Skipf(...)` when the source table is empty:
- `anyStockSymbol(t) string` — `SELECT symbol FROM stocks LIMIT 1`
- `anyAlphaPickDate(t, mode) string` — `SELECT MAX(trade_date)::text FROM alpha_pick WHERE mode=$1`
- `anyAlphaSellDate(t, mode) string` — same against `alpha_sell`
- `anyDailyDate(t) string` — `SELECT MAX(trade_date)::text FROM stock_daily_raw`
- `anyTradeRecordSymbol(t) string` — `SELECT symbol FROM trade_records LIMIT 1` (only used if needed)

**Assertion helpers** — thin sugar over testify:
- `requireKeys(t, m map[string]any, keys ...string)` — fails listing all missing keys at once.
- `requireNoKey(t, m map[string]any, key string)` — used by the `reasons` regression guard.
- `requireDateString(t, v any)` — asserts the value is a string parseable as `YYYY-MM-DD`.

### Test inventory

#### `stocks_test.go`
- `TestListStocks_OK` — 200; body is `[]any`; if non-empty, first element has `{symbol, name, enabled, issued_shares}`.
- `TestListStocks_EnabledFilter` — `?enabled=true` → every returned `enabled` is `true`.
- `TestGetStock_OK` — `anyStockSymbol(t)` → 200; body has `{symbol, name, enabled, issued_shares}`; `symbol` matches request.
- `TestGetStock_NotFound` — `/api/stocks/__INVALID_XX__` → 404; body has `error` key.

#### `daily_test.go`
- `TestListDailyDates_OK` — 200; `[]any`; each is parseable `YYYY-MM-DD`; descending order.
- `TestListDailyDates_LimitHonored` — `?limit=3` → length ≤ 3.
- `TestGetDailyByDate_OK` — `anyDailyDate(t)` → 200; `[]any`; first element has the documented daily column keys (full set: `rsi_14`, `bb_percent_b`, `foreign_net_5d_avg`, …).
- `TestGetDailyByDate_NoData` — `/api/daily/1900-01-01` → 200; empty array `[]`.
- `TestGetStockHistory_OK` — `/api/daily/stock/{anyStockSymbol}` → 200; `[]any`; if non-empty, has expected keys; `trade_date` descending.
- `TestGetStockHistory_LimitHonored` — `?limit=5` → length ≤ 5.

#### `alpha_test.go` — pick (5 endpoints)
- `TestGetLatestPick_OK` — 200; envelope `{trade_date, count, picks}`; `count == len(picks)`; if non-empty, first pick has full key set (`close`, `rsi_14`, `cond_insti`, `cond_insti_bullish`, …, `cond_turnover_surge`); **`requireNoKey(t, pick, "reasons")`** — regression guard.
- `TestListPickDates_OK` — 200; `[]any`; each is `YYYY-MM-DD`; respects `?limit=`.
- `TestGetPickSummary_OK` — 200; `[]any`; each has `{symbol, name, pick_count, first_date, last_date}`.
- `TestGetPickBySymbol_OK` — `anyStockSymbol(t)` → 200; `{symbol, count, records}`; `count == len(records)`; each record has `{trade_date, symbol, name}` and **no `reasons`**.
- `TestGetPickByDate_OK` — `anyAlphaPickDate(t, "alpha")` → 200; same envelope as `latest`; **no `reasons`**.
- `TestGetPickByDate_NoData` — `/api/alpha/pick/1900-01-01` → 200; `count == 0`; `picks == []`.
- `TestGetPickBySymbol_Unknown` — `/api/alpha/pick/stock/__INVALID_XX__` → 200; `count == 0`; `records == []` (confirms behavior: unknown symbol returns empty, not 404).

#### `alpha_test.go` — sell (4 endpoints)
- `TestGetLatestSell_OK` — envelope `{trade_date, count, sells}`; `conditions_met` is a number; **no `reasons`**.
- `TestGetSellSummary_OK` — `[]any`; each has `{symbol, name, sell_count, first_date, last_date}`.
- `TestGetSellBySymbol_OK` — `anyStockSymbol(t)` → `{symbol, count, records}`; **no `reasons`**.
- `TestGetSellByDate_OK` — `anyAlphaSellDate(t, "sell")` → envelope OK; **no `reasons`**; sorted by `conditions_met DESC`.

#### `trade_test.go`
- `TestGetTradeRecords_OK` — 200; envelope `{count, profit_count, loss_count, avg_performance, win_rate, records}`; `count == len(records)`; sanity: `profit_count + loss_count <= count`; if `win_rate != nil`, float in `[0, 1]`; if `avg_performance != nil`, is a float.
- `TestGetTradeRecords_DateRange` — `?from=&to=` → every `trade_date` in range; descending order.
- `TestGetTradeRecords_EmptyRange` — `?from=1900-01-01&to=1900-01-02` → `count == 0`; `records == []`; `profit_count == 0`; `loss_count == 0`; `win_rate == nil`; `avg_performance == nil`. Exercises the "no valid perf" branch of summary computation.

**Total: 25 test functions.**

## Regression guards (purpose beyond happy path)

- **`reasons` field must not reappear** in any `/alpha/pick` or `/alpha/sell` response. Every alpha test that inspects a record calls `requireNoKey(t, record, "reasons")`. Cheap fence around the change shipped on 2026-04-21.
- **`count` envelope fields match `len(picks|sells|records)`** — catches handlers that miscount or filter after JSON envelope is built.
- **Date-shaped fields are `YYYY-MM-DD` strings**, never Go `time.Time` JSON. Protects `helpers.go`'s `convertValue` date-conversion path.

## Running the suite

```bash
DATABASE_URL=postgresql://... go test ./routers/...
# or, with .env present at repo root:
go test ./routers/...
```

Expected runtime: 1–3 seconds (25 tests × 1–2 read-only queries each, single process).

## Non-goals

- **No CI config.** Approach A (shared dev DB) chosen — CI-hermeticity isn't on the table.
- **No fixture seeding or schema management.** DB is owned by TWStockAnalysis.
- **No mocks, stubs, or fake DB.** Every test hits real Postgres.
- **No benchmark tests, no load tests.**
- **No middleware tests** (CORS, slog-gin, recovery — third-party).
- **No tests against a running server.** Tests hit Gin router in-process via `httptest`.

## Risks & mitigations

1. **testify is a new direct dependency.** Pinned to current release; future bumps via normal dependency management.
2. **Tests assume schema stability.** If TWStockAnalysis renames a column we SELECT, the handler breaks first; the test fails for the same reason production does. This is the desired behavior, not a defect.
3. **Discovery helpers may race with the TWStockAnalysis batch job** writing rows mid-test. Probability is low; accepted for dev-DB usage.
4. **Shared `db.Pool()` global.** Tests use the real pool. Safe because all current handlers are read-only. Future write handlers would require revisit (transactional rollback or per-test DB).

## Acceptance criteria

- [ ] `go test ./routers/...` passes with `DATABASE_URL` set against a populated dev DB.
- [ ] `go test ./...` passes (exits 0) when `DATABASE_URL` is unset.
- [ ] All 13 routes have at least one happy-path test.
- [ ] All `/alpha/pick` and `/alpha/sell` happy-path tests assert `reasons` is absent.
- [ ] Negative-path tests (`_NotFound`, `_NoData`, `_EmptyRange`) run unconditionally (do not depend on table state).
- [ ] `main.go` is unchanged.
