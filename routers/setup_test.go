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
