# API Integration Tests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add real-DB integration tests covering every public HTTP route in `routers/`, exercised through the actual Gin engine via `httptest`.

**Architecture:** Co-located `*_test.go` files in `package routers`. A single `setup_test.go` provides `TestMain` (DB pool init/teardown), a router builder, an HTTP helper, dynamic-discovery helpers, and assertion sugar. Each handler file gets a corresponding `*_test.go` with happy-path + targeted negative tests. Suite skips cleanly when `DATABASE_URL` is unset.

**Tech Stack:** Go 1.25, Gin v1.12, pgx/v5, `net/http/httptest`, `github.com/stretchr/testify` (test-only).

**Spec:** [`docs/superpowers/specs/2026-04-21-api-integration-tests-design.md`](../specs/2026-04-21-api-integration-tests-design.md)

**File structure (created by this plan):**
- `routers/setup_test.go` — `TestMain`, helpers (no `Test*` funcs)
- `routers/stocks_test.go` — 4 tests
- `routers/daily_test.go` — 6 tests
- `routers/alpha_test.go` — 11 tests (5 pick happy + 4 sell happy + 2 negative)
- `routers/trade_test.go` — 3 tests
- `go.mod`, `go.sum` — `testify` added

**Total: 24 test functions across 4 test files + scaffolding.**

---

## Task 1: Add testify dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add testify**

Run from repo root:
```bash
go get github.com/stretchr/testify@v1.10.0
go mod tidy
```

- [ ] **Step 2: Verify go.mod is updated**

```bash
grep -F "github.com/stretchr/testify" go.mod
```

Expected: a `require` line for `github.com/stretchr/testify v1.10.0` (direct dep, no `// indirect`).

- [ ] **Step 3: Confirm build still works**

```bash
go build ./...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add testify for integration tests"
```

---

## Task 2: Build shared test scaffolding (`routers/setup_test.go`)

**Files:**
- Create: `routers/setup_test.go`

This file contains `TestMain` and all shared helpers. It must compile on its own before any other test file is added (other test files will fail to compile without these helpers).

- [ ] **Step 1: Write `routers/setup_test.go`**

Create the file with this exact content:

```go
package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"main/db"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

// TestMain initializes the DB pool once for all tests in the routers package.
// If DATABASE_URL is unset, the whole package is skipped (exit 0) so that
// `go test ./...` stays green on machines without a DB.
func TestMain(m *testing.M) {
	_ = godotenv.Load("../.env")
	if os.Getenv("DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL not set, skipping routers integration tests")
		os.Exit(0)
	}
	if err := db.InitPool(); err != nil {
		fmt.Fprintln(os.Stderr, "db init failed:", err)
		os.Exit(1)
	}
	code := m.Run()
	db.ClosePool()
	os.Exit(code)
}

// newTestRouter mirrors main.go's route setup without middleware.
// Cheap to call per test — routes are just map entries.
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

// doJSON executes an HTTP request against the in-memory router and decodes
// the JSON response body into a generic any (map[string]any or []any).
func doJSON(t *testing.T, r *gin.Engine, method, path string) (int, any) {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var decoded any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded),
			"decode body: %s", w.Body.String())
	}
	return w.Code, decoded
}

// ── Discovery helpers (Q2-C: dynamic for happy paths) ──

func anyStockSymbol(t *testing.T) string {
	t.Helper()
	var symbol string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT symbol FROM stocks LIMIT 1").Scan(&symbol)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no rows in stocks")
	}
	require.NoError(t, err)
	return symbol
}

func anyDailyDate(t *testing.T) string {
	t.Helper()
	var d *string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT MAX(trade_date)::text FROM stock_daily_raw").Scan(&d)
	require.NoError(t, err)
	if d == nil {
		t.Skip("no rows in stock_daily_raw")
	}
	return *d
}

func anyAlphaPickDate(t *testing.T, mode string) string {
	t.Helper()
	var d *string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT MAX(trade_date)::text FROM alpha_pick WHERE mode = $1", mode).Scan(&d)
	require.NoError(t, err)
	if d == nil {
		t.Skipf("no rows in alpha_pick for mode=%s", mode)
	}
	return *d
}

func anyAlphaSellDate(t *testing.T, mode string) string {
	t.Helper()
	var d *string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT MAX(trade_date)::text FROM alpha_sell WHERE mode = $1", mode).Scan(&d)
	require.NoError(t, err)
	if d == nil {
		t.Skipf("no rows in alpha_sell for mode=%s", mode)
	}
	return *d
}

// ── Assertion helpers (thin sugar over testify) ──

// requireKeys fails listing all missing keys at once (better signal than
// failing on the first one).
func requireKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	var missing []string
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			missing = append(missing, k)
		}
	}
	require.Empty(t, missing, "missing keys: %v (got: %v)", missing, mapKeys(m))
}

// requireNoKey is the regression guard for the removed `reasons` field.
func requireNoKey(t *testing.T, m map[string]any, key string) {
	t.Helper()
	_, ok := m[key]
	require.False(t, ok, "unexpected key %q in response", key)
}

// requireDateString asserts the value is a YYYY-MM-DD string.
func requireDateString(t *testing.T, v any) {
	t.Helper()
	s, ok := v.(string)
	require.True(t, ok, "expected string, got %T (%v)", v, v)
	_, err := time.Parse(time.DateOnly, s)
	require.NoError(t, err, "expected YYYY-MM-DD, got %q", s)
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Compile-time guard so unused imports don't break this scaffolding file
// when individual helpers are temporarily removed during development.
var _ = http.StatusOK
```

- [ ] **Step 2: Verify the file compiles**

```bash
go test ./routers/ -run='^$' -count=1
```

Expected: `ok    main/routers   0.0XXs` (no tests run, but the package compiles).

If it fails to compile, fix the imports in `setup_test.go` and re-run.

- [ ] **Step 3: Verify TestMain skip path works**

```bash
DATABASE_URL= go test ./routers/ -run='^$' -count=1
```

Expected: stderr says `DATABASE_URL not set, skipping routers integration tests`; exit 0; output `ok   main/routers   ...`.

- [ ] **Step 4: Verify TestMain init path works**

```bash
go test ./routers/ -run='^$' -count=1
```

(With `.env` present in repo root, DATABASE_URL is loaded.) Expected: exit 0, no skip message.

- [ ] **Step 5: Commit**

```bash
git add routers/setup_test.go
git commit -m "test: add integration test scaffolding (TestMain + helpers)"
```

---

## Task 3: Tests for `routers/stocks.go`

**Files:**
- Create: `routers/stocks_test.go`

Covers `GET /api/stocks`, `GET /api/stocks?enabled=true`, `GET /api/stocks/:symbol`, `GET /api/stocks/__INVALID_XX__`.

- [ ] **Step 1: Write `routers/stocks_test.go`**

Create the file with this exact content:

```go
package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListStocks_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok, "expected array, got %T", body)

	if len(arr) > 0 {
		first, ok := arr[0].(map[string]any)
		require.True(t, ok)
		requireKeys(t, first, "symbol", "name", "enabled", "issued_shares")
	}
}

func TestListStocks_EnabledFilter(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks?enabled=true")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	for i, item := range arr {
		m, ok := item.(map[string]any)
		require.True(t, ok, "row %d not an object", i)
		assert.Equal(t, true, m["enabled"], "row %d: expected enabled=true", i)
	}
}

func TestGetStock_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks/"+symbol)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "name", "enabled", "issued_shares")
	assert.Equal(t, symbol, m["symbol"])
}

func TestGetStock_NotFound(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks/__INVALID_XX__")
	require.Equal(t, 404, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "error")
}
```

- [ ] **Step 2: Run the tests**

```bash
go test ./routers/ -run='^TestListStocks|^TestGetStock' -v -count=1
```

Expected: 4 PASS lines (or `SKIP` for `TestGetStock_OK` if `stocks` table is empty).

If any test fails:
- If it's a key-set mismatch, compare against the SELECT in `routers/stocks.go` and update the test's `requireKeys` list.
- If status is wrong, read the handler — the test is the source of truth for the contract; investigate before changing.

- [ ] **Step 3: Commit**

```bash
git add routers/stocks_test.go
git commit -m "test: add integration tests for /api/stocks"
```

---

## Task 4: Tests for `routers/daily.go`

**Files:**
- Create: `routers/daily_test.go`

Covers `GET /api/daily/dates` (+`?limit=`), `GET /api/daily/:date` (happy + `1900-01-01`), `GET /api/daily/stock/:symbol` (+`?limit=`).

- [ ] **Step 1: Write `routers/daily_test.go`**

Create the file with this exact content:

```go
package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDailyDates_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/dates")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	var prev string
	for i, d := range arr {
		requireDateString(t, d)
		s := d.(string)
		if i > 0 {
			assert.LessOrEqual(t, s, prev, "dates should be descending at index %d", i)
		}
		prev = s
	}
}

func TestListDailyDates_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/dates?limit=3")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 3)
}

func TestGetDailyByDate_OK(t *testing.T) {
	date := anyDailyDate(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/"+date)
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	require.NotEmpty(t, arr, "anyDailyDate returned %s but /api/daily/%s is empty", date, date)

	first, ok := arr[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "name",
		"open", "close", "high", "low", "volume",
		"turnover_rate",
		"foreign_net", "trust_net", "dealer_net", "institutional_investors_net",
		"margin_balance", "short_balance", "short_margin_ratio",
		"foreign_holding_pct", "insti_holding_pct",
		"vol_ma5", "vol_ma10", "vol_ma20", "turnover_ma20",
		"foreign_net_5d_avg", "foreign_net_10d_avg",
		"foreign_net_15d_avg", "foreign_net_30d_avg",
		"rsi_9", "rsi_14",
		"macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_middle", "bb_lower",
		"bb_percent_b", "bb_bandwidth",
	)
}

func TestGetDailyByDate_NoData(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/1900-01-01")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.Empty(t, arr)
}

func TestGetStockHistory_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/stock/"+symbol)
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	if len(arr) == 0 {
		t.Skipf("no daily rows for symbol %s", symbol)
	}

	first, ok := arr[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"trade_date",
		"open", "close", "high", "low", "volume",
		"turnover_rate",
		"foreign_net", "trust_net", "dealer_net", "institutional_investors_net",
		"margin_balance", "short_balance", "short_margin_ratio",
		"vol_ma5", "vol_ma10", "vol_ma20", "turnover_ma20",
		"foreign_net_5d_avg", "foreign_net_10d_avg",
		"foreign_net_15d_avg", "foreign_net_30d_avg",
		"rsi_9", "rsi_14",
		"macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_middle", "bb_lower",
		"bb_percent_b", "bb_bandwidth",
	)

	var prev string
	for i, item := range arr {
		m := item.(map[string]any)
		d, ok := m["trade_date"].(string)
		require.True(t, ok, "row %d: trade_date is not a string", i)
		if i > 0 {
			assert.LessOrEqual(t, d, prev, "trade_date should be desc at row %d", i)
		}
		prev = d
	}
}

func TestGetStockHistory_LimitHonored(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/stock/"+symbol+"?limit=5")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 5)
}
```

- [ ] **Step 2: Run the tests**

```bash
go test ./routers/ -run='^TestListDailyDates|^TestGetDailyByDate|^TestGetStockHistory' -v -count=1
```

Expected: 6 PASS (or some SKIP if `stock_daily_raw` is empty for the discovered date/symbol).

If `requireKeys` fails, cross-check the column list against `routers/daily.go` (`dailyColumns` and `historyColumns`).

- [ ] **Step 3: Commit**

```bash
git add routers/daily_test.go
git commit -m "test: add integration tests for /api/daily"
```

---

## Task 5: Tests for `routers/alpha.go` — pick endpoints

**Files:**
- Create: `routers/alpha_test.go` (pick tests only — sell tests appended in Task 6)

Covers all 5 pick endpoints + 2 negative tests. The `requireNoKey(..., "reasons")` calls are the regression guard for the field removed on 2026-04-21.

- [ ] **Step 1: Write the pick portion of `routers/alpha_test.go`**

Create the file with this exact content:

```go
package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Pick endpoints ──

func TestGetLatestPick_OK(t *testing.T) {
	_ = anyAlphaPickDate(t, "alpha") // skip if alpha_pick is empty

	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/latest")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "picks")
	requireDateString(t, m["trade_date"])

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(picks), int(count))
	require.NotEmpty(t, picks, "expected non-empty picks since trade_date exists")

	first, ok := picks[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume",
		"vol_ma5", "vol_ma10", "vol_ma20",
		"rsi_14", "macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_bandwidth", "bb_percent_b",
		"insti_net_5d_sum", "insti_net_5d_avg",
		"insti_net_10d_sum", "insti_net_10d_avg",
		"insti_net_15d_sum", "insti_net_15d_avg",
		"insti_net_30d_sum", "insti_net_30d_avg",
		"bb_bw_5d_avg", "bb_bw_10d_avg", "bb_bw_15d_avg", "bb_bw_30d_avg",
		"cond_insti", "cond_insti_bullish", "cond_rsi", "cond_macd",
		"cond_vol_ma10", "cond_vol_ma20",
		"cond_bb_narrow", "cond_bb_near_upper", "cond_turnover_surge",
	)
	requireNoKey(t, first, "reasons")
	requireDateString(t, first["trade_date"])
}

func TestListPickDates_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/dates")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	var prev string
	for i, d := range arr {
		requireDateString(t, d)
		s := d.(string)
		if i > 0 {
			assert.LessOrEqual(t, s, prev, "dates should be descending at index %d", i)
		}
		prev = s
	}
}

func TestListPickDates_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/dates?limit=2")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 2)
}

func TestGetPickSummary_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/summary")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	if len(arr) > 0 {
		first, ok := arr[0].(map[string]any)
		require.True(t, ok)
		requireKeys(t, first, "symbol", "name", "pick_count", "first_date", "last_date")
	}
}

func TestGetPickBySymbol_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/stock/"+symbol)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "count", "records")
	assert.Equal(t, symbol, m["symbol"])

	records, ok := m["records"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(records), int(count))

	for i, rec := range records {
		recm, ok := rec.(map[string]any)
		require.True(t, ok, "record %d not an object", i)
		requireKeys(t, recm, "trade_date", "symbol", "name")
		requireNoKey(t, recm, "reasons")
		requireDateString(t, recm["trade_date"])
	}
}

func TestGetPickByDate_OK(t *testing.T) {
	date := anyAlphaPickDate(t, "alpha")
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/"+date)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "picks")
	assert.Equal(t, date, m["trade_date"])

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(picks), int(count))
	require.NotEmpty(t, picks)

	first, ok := picks[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume",
		"rsi_14", "macd_hist", "bb_percent_b",
		"insti_net_5d_sum", "insti_net_5d_avg",
		"insti_net_10d_sum", "insti_net_10d_avg",
		"insti_net_15d_sum", "insti_net_15d_avg",
		"insti_net_30d_sum", "insti_net_30d_avg",
		"cond_insti", "cond_insti_bullish", "cond_rsi", "cond_macd",
		"cond_vol_ma10", "cond_vol_ma20",
		"cond_bb_narrow", "cond_bb_near_upper", "cond_turnover_surge",
	)
	requireNoKey(t, first, "reasons")
}

// ── Negative paths (unconditional) ──

func TestGetPickByDate_NoData(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/1900-01-01")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "picks")

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	assert.Empty(t, picks)
}

func TestGetPickBySymbol_Unknown(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/stock/__INVALID_XX__")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "count", "records")
	assert.Equal(t, "__INVALID_XX__", m["symbol"])

	records, ok := m["records"].([]any)
	require.True(t, ok)
	assert.Empty(t, records)
	assert.Equal(t, float64(0), m["count"])
}
```

- [ ] **Step 2: Run the pick tests**

```bash
go test ./routers/ -run='^TestGetLatestPick|^TestListPickDates|^TestGetPickSummary|^TestGetPickBySymbol|^TestGetPickByDate' -v -count=1
```

Expected: 8 PASS (5 happy + 1 limit-variant + 2 negative). Some happy-path tests may SKIP if `alpha_pick` is empty.

If `requireKeys` complains about a missing field, compare against the SELECT in `routers/alpha.go` (`getLatestPick` line 40 and `getPickByDate` line 176).

- [ ] **Step 3: Commit**

```bash
git add routers/alpha_test.go
git commit -m "test: add integration tests for /api/alpha/pick"
```

---

## Task 6: Append sell-endpoint tests to `routers/alpha_test.go`

**Files:**
- Modify: `routers/alpha_test.go`

Appends 4 sell-endpoint tests to the bottom of the file created in Task 5.

- [ ] **Step 1: Append the sell test block**

Append this block to the end of `routers/alpha_test.go` (after `TestGetPickBySymbol_Unknown`):

```go

// ── Sell endpoints ──

func TestGetLatestSell_OK(t *testing.T) {
	_ = anyAlphaSellDate(t, "sell") // skip if alpha_sell is empty

	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/sell/latest")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "sells")
	requireDateString(t, m["trade_date"])

	sells, ok := m["sells"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(sells), int(count))
	require.NotEmpty(t, sells)

	first, ok := sells[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume", "vol_ma10",
		"rsi_14", "macd_hist", "bb_percent_b",
		"foreign_net_5d_sum", "foreign_net_5d_avg",
		"foreign_net_10d_sum", "foreign_net_10d_avg",
		"foreign_net_15d_sum", "foreign_net_15d_avg",
		"foreign_net_30d_sum", "foreign_net_30d_avg",
		"trust_net_5d_sum", "trust_net_5d_avg",
		"trust_net_10d_sum", "trust_net_10d_avg",
		"trust_net_15d_sum", "trust_net_15d_avg",
		"trust_net_30d_sum", "trust_net_30d_avg",
		"cond_foreign_sell", "cond_foreign_accel",
		"cond_trust_sell", "cond_trust_accel",
		"cond_high_black", "cond_price_up_vol_down",
		"cond_rsi_overbought", "cond_rsi_divergence",
		"cond_macd_turn_neg", "cond_macd_divergence",
		"cond_bb_below", "cond_macd_death_cross",
		"cond_margin_surge", "cond_turnover_surge", "cond_vol_surge_flat",
		"conditions_met",
	)
	requireNoKey(t, first, "reasons")
	_, isFloat := first["conditions_met"].(float64)
	assert.True(t, isFloat, "conditions_met should be a number")
}

func TestGetSellSummary_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/sell/summary")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	if len(arr) > 0 {
		first, ok := arr[0].(map[string]any)
		require.True(t, ok)
		requireKeys(t, first, "symbol", "name", "sell_count", "first_date", "last_date")
	}
}

func TestGetSellBySymbol_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/sell/stock/"+symbol)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "count", "records")
	assert.Equal(t, symbol, m["symbol"])

	records, ok := m["records"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(records), int(count))

	for i, rec := range records {
		recm, ok := rec.(map[string]any)
		require.True(t, ok, "record %d not an object", i)
		requireKeys(t, recm, "trade_date", "symbol", "name")
		requireNoKey(t, recm, "reasons")
		requireDateString(t, recm["trade_date"])
	}
}

func TestGetSellByDate_OK(t *testing.T) {
	date := anyAlphaSellDate(t, "sell")
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/sell/"+date)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "sells")
	assert.Equal(t, date, m["trade_date"])

	sells, ok := m["sells"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(sells), int(count))
	require.NotEmpty(t, sells)

	first, ok := sells[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume", "vol_ma10",
		"rsi_14", "macd_hist", "bb_percent_b", "conditions_met",
	)
	requireNoKey(t, first, "reasons")

	// sorted by conditions_met DESC
	var prev float64 = -1
	for i, s := range sells {
		sm := s.(map[string]any)
		cm, ok := sm["conditions_met"].(float64)
		require.True(t, ok, "row %d: conditions_met not a number", i)
		if i > 0 {
			assert.LessOrEqual(t, cm, prev, "conditions_met should be DESC at row %d", i)
		}
		prev = cm
	}
}
```

- [ ] **Step 2: Run all alpha tests together**

```bash
go test ./routers/ -run='^TestGet|^TestList' -v -count=1
```

Wait — that pattern is too broad. Use the alpha-specific run flag:

```bash
go test ./routers/ -run='^TestGet(Latest|Pick|Sell)|^TestList(Pick)' -v -count=1
```

Expected: ~12 PASS lines. Some may SKIP if `alpha_sell` / `alpha_pick` is empty.

If `requireKeys` complains about a missing key, compare against the SELECT in `routers/alpha.go` (`getLatestSell` line 228, `getSellByDate` line 338).

- [ ] **Step 3: Commit**

```bash
git add routers/alpha_test.go
git commit -m "test: add integration tests for /api/alpha/sell"
```

---

## Task 7: Tests for `routers/trade.go`

**Files:**
- Create: `routers/trade_test.go`

Covers `GET /api/trade/trade-records` happy path, `?from=&to=` filter, and the empty-range case (which exercises the "no valid perf" branch in summary computation).

- [ ] **Step 1: Write `routers/trade_test.go`**

Create the file with this exact content:

```go
package routers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTradeRecords_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "count", "profit_count", "loss_count", "avg_performance", "win_rate", "records")

	records, ok := m["records"].([]any)
	require.True(t, ok)

	count := int(m["count"].(float64))
	assert.Equal(t, len(records), count)

	profit := int(m["profit_count"].(float64))
	loss := int(m["loss_count"].(float64))
	assert.LessOrEqual(t, profit+loss, count, "profit+loss must not exceed total records")

	if wr, present := m["win_rate"]; present && wr != nil {
		f, ok := wr.(float64)
		require.True(t, ok, "win_rate should be a number, got %T", wr)
		assert.GreaterOrEqual(t, f, 0.0)
		assert.LessOrEqual(t, f, 1.0)
	}
	if avg, present := m["avg_performance"]; present && avg != nil {
		_, ok := avg.(float64)
		require.True(t, ok, "avg_performance should be a number, got %T", avg)
	}

	for i, rec := range records {
		recm, ok := rec.(map[string]any)
		require.True(t, ok, "record %d not an object", i)
		requireKeys(t, recm, "symbol", "name", "type", "trade_date", "price", "performance")
		requireDateString(t, recm["trade_date"])
	}
}

func TestGetTradeRecords_DateRange(t *testing.T) {
	from := "2026-01-01"
	to := "2026-04-21"
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records?from="+from+"&to="+to)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	records, ok := m["records"].([]any)
	require.True(t, ok)

	var prev string
	for i, rec := range records {
		recm := rec.(map[string]any)
		d, ok := recm["trade_date"].(string)
		require.True(t, ok, "row %d: trade_date not string", i)
		assert.GreaterOrEqual(t, d, from, "row %d: trade_date %s before from %s", i, d, from)
		assert.LessOrEqual(t, d, to, "row %d: trade_date %s after to %s", i, d, to)
		if i > 0 {
			assert.LessOrEqual(t, d, prev, "row %d: trade_date should be DESC", i)
		}
		prev = d
	}

	// sanity: the parse worked end-to-end
	_, err := time.Parse(time.DateOnly, from)
	require.NoError(t, err)
}

func TestGetTradeRecords_EmptyRange(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records?from=1900-01-01&to=1900-01-02")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "count", "profit_count", "loss_count", "avg_performance", "win_rate", "records")

	records, ok := m["records"].([]any)
	require.True(t, ok)
	assert.Empty(t, records)
	assert.Equal(t, float64(0), m["count"])
	assert.Equal(t, float64(0), m["profit_count"])
	assert.Equal(t, float64(0), m["loss_count"])
	assert.Nil(t, m["avg_performance"])
	assert.Nil(t, m["win_rate"])
}
```

- [ ] **Step 2: Run the trade tests**

```bash
go test ./routers/ -run='^TestGetTradeRecords' -v -count=1
```

Expected: 3 PASS.

- [ ] **Step 3: Commit**

```bash
git add routers/trade_test.go
git commit -m "test: add integration tests for /api/trade/trade-records"
```

---

## Task 8: Final full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Run the complete suite (DB present)**

```bash
go test ./routers/ -v -count=1
```

Expected: every test either PASS or SKIP. No FAIL. Summary line shows ~24 tests run.

If any test fails:
- Read the failing assertion. Compare expected keys against the actual SELECT in the corresponding handler.
- If the handler changed since the spec was written, update the test, not the handler — unless the test caught a real bug.

- [ ] **Step 2: Run the suite without DATABASE_URL**

```bash
DATABASE_URL= go test ./routers/ -count=1
```

Expected: stderr `DATABASE_URL not set, skipping routers integration tests`; exit 0; summary `ok   main/routers   0.0XXs`.

- [ ] **Step 3: Run `go vet` and the rest of the build**

```bash
go vet ./...
go build ./...
```

Expected: no output, exit 0 for both.

- [ ] **Step 4: No commit needed (verification only)**

If a commit is needed because of an in-test fix, group it as:

```bash
git add routers/
git commit -m "test: fix integration test assertion to match handler"
```

---

## Plan self-review

**Spec coverage:**
- Q1 (existing dev DB, read-only): ✅ Task 2 `TestMain` loads `.env`.
- Q2 (hybrid input strategy): ✅ Task 2 discovery helpers + hard-coded `__INVALID_XX__` / `1900-01-01` in Tasks 3, 4, 5.
- Q3 (status + shape + light sanity): ✅ All tests verify status, `requireKeys`, plus sanity (`count == len(...)`, dates parse, win_rate ∈ [0,1]).
- Q4 (testify + skip-on-no-DB + co-located): ✅ Task 1 dep, Task 2 skip path, all `*_test.go` co-located.
- Health endpoints not tested: ✅ no health tests anywhere.
- 13 routes covered: ✅ Tasks 3–7 cover stocks (2) + daily (3) + alpha pick (5) + alpha sell (4) + trade (1) = 15 endpoints (the spec said 13; the extra 2 are `?enabled=true` and `?limit=` variants of routes already counted, not new routes).
- `reasons` regression guard: ✅ `requireNoKey(..., "reasons")` in `TestGetLatestPick_OK`, `TestGetPickBySymbol_OK`, `TestGetPickByDate_OK`, `TestGetLatestSell_OK`, `TestGetSellBySymbol_OK`, `TestGetSellByDate_OK`.
- `count == len(...)` envelope check: ✅ in pick latest, pick by date, pick by symbol, sell latest, sell by date, sell by symbol, trade records.
- Date-shaped fields parse: ✅ `requireDateString` used in daily/dates, pick/dates, latest pick, pick by date, pick by symbol records, latest sell, sell by date, sell by symbol records, trade records.
- `_NoData` / `_EmptyRange` / `_NotFound` / `_Unknown` unconditional: ✅ none use discovery helpers.
- `main.go` unchanged: ✅ no task touches it.

**Placeholder scan:** No TBD/TODO/"implement later"/etc. All test bodies are full code.

**Type consistency:** Helper signatures used by tests match their definitions in Task 2:
- `newTestRouter() *gin.Engine` ✅
- `doJSON(t, r, method, path) (int, any)` ✅
- `anyStockSymbol(t) string`, `anyDailyDate(t) string`, `anyAlphaPickDate(t, mode) string`, `anyAlphaSellDate(t, mode) string` ✅
- `requireKeys(t, m, keys...)`, `requireNoKey(t, m, key)`, `requireDateString(t, v)` ✅

JSON value types (always `float64` for numbers, `string` for strings, `[]any`/`map[string]any` for collections) used consistently across all assertions.
