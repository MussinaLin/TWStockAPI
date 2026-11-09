package routers

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCustomSignalRouter mounts the custom signal routes behind a fake login
// that authenticates every request as userID.
func newCustomSignalRouter(userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	fakeLogin := func(c *gin.Context) {
		c.Set(auth.ContextUserKey, auth.User{ID: userID, Email: "custom-signal-test@example.com"})
		c.Next()
	}
	RegisterCustomSignals(r.Group("/api"), fakeLogin)
	return r
}

// signalRuleFor reads the configured custom signal rule for level.
func signalRuleFor(t *testing.T, level int) signalRule {
	t.Helper()
	rule, err := loadCustomSignalLimits(context.Background(), db.Pool(), level)
	require.NoError(t, err, "apply db/user_custom_signals.sql first")
	return rule
}

// addSignalRow inserts a setting directly, bypassing the PUT checks.
func addSignalRow(t *testing.T, userID, symbol string, enabled bool, created time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO user_stock_signals (user_id, symbol, enabled, conditions, created_time)
		 VALUES ($1::uuid, $2, $3, $4::jsonb, $5)`,
		userID, symbol, enabled,
		`[{"id":"k","side":"long","kind":"kd_k","op":"gt","value":80}]`, created)
	require.NoError(t, err)
}

func signalCount(t *testing.T, userID string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM user_stock_signals WHERE user_id = $1::uuid", userID).Scan(&n)
	require.NoError(t, err)
	return n
}

func signalPath(symbol string) string { return "/api/custom-signals/" + symbol }

const oneCondition = `{"enabled":true,"conditions":[{"id":"a","side":"long","kind":"kd_k","op":"gt","value":50}]}`

// conditionsBody builds a PUT body with long/short conditions of kd_k.
func conditionsBody(enabled bool, long, short int) string {
	var cs []string
	for i := 0; i < long; i++ {
		cs = append(cs, fmt.Sprintf(`{"id":"l%d","side":"long","kind":"kd_k","op":"gt","value":50}`, i))
	}
	for i := 0; i < short; i++ {
		cs = append(cs, fmt.Sprintf(`{"id":"s%d","side":"short","kind":"kd_k","op":"lt","value":50}`, i))
	}
	return fmt.Sprintf(`{"enabled":%t,"conditions":[%s]}`, enabled, strings.Join(cs, ","))
}

// putStatus sends one PUT without testify (safe to call from goroutines).
func putStatus(r *gin.Engine, symbol, body string) int {
	req := httptest.NewRequest("PUT", signalPath(symbol), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

func TestGetCustomSignal_NotSet(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)

	status, body := doJSON(t, newCustomSignalRouter(userID), "GET", signalPath(symbol))
	require.Equal(t, 200, status)
	assert.Equal(t, map[string]any{
		"symbol": symbol, "enabled": false, "active": true,
		"conditions": []any{}, "updated_time": nil,
	}, body)
}

func TestGetCustomSignal_StockNotFound(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)
	for _, symbol := range []string{"NOPE-XYZ", disabledSymbol(t)} {
		status, body := doJSON(t, r, "GET", signalPath(symbol))
		assert.Equal(t, 404, status, symbol)
		assert.Equal(t, map[string]any{"error": "stock not found"}, body, symbol)
	}
}

func TestPutCustomSignal_CreateThenGet(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)

	status, put := doJSONBody(t, r, "PUT", signalPath(symbol),
		`{"enabled":true,"conditions":[{"id":"a","side":"long","kind":"rsi","period":9,"op":"gt","value":50,"extra":1}]}`)
	require.Equal(t, 200, status, "body: %v", put)
	m := put.(map[string]any)
	assert.Equal(t, true, m["enabled"])
	assert.Equal(t, true, m["active"])
	conds := m["conditions"].([]any)
	require.Len(t, conds, 1)
	assert.NotContains(t, conds[0], "extra")
	assert.Equal(t, "rsi", conds[0].(map[string]any)["kind"])

	status, got := doJSON(t, r, "GET", signalPath(symbol))
	require.Equal(t, 200, status)
	g := got.(map[string]any)
	assert.NotNil(t, g["updated_time"])
	assert.Equal(t, m["conditions"], g["conditions"])
	assert.Equal(t, true, g["enabled"])
	assert.Equal(t, true, g["active"])
}

func TestPutCustomSignal_Overwrites(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)

	status, _ := doJSONBody(t, r, "PUT", signalPath(symbol), oneCondition)
	require.Equal(t, 200, status)
	status, _ = doJSONBody(t, r, "PUT", signalPath(symbol),
		`{"enabled":false,"conditions":[{"id":"b","side":"short","kind":"kd_k","op":"lt","value":20}]}`)
	require.Equal(t, 200, status)

	_, got := doJSON(t, r, "GET", signalPath(symbol))
	g := got.(map[string]any)
	assert.Equal(t, false, g["enabled"])
	conds := g["conditions"].([]any)
	require.Len(t, conds, 1)
	assert.Equal(t, "b", conds[0].(map[string]any)["id"])
	assert.Equal(t, 1, signalCount(t, userID))
}

func TestPutCustomSignal_InvalidRequest(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)
	for _, body := range []string{`not json`, `{"conditions":[]}`} {
		status, resp := doJSONBody(t, r, "PUT", signalPath(symbol), body)
		assert.Equal(t, 400, status, body)
		assert.Equal(t, map[string]any{"error": "invalid request"}, resp, body)
	}
	assert.Equal(t, 0, signalCount(t, userID))
}

func TestPutCustomSignal_StringPeriod(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)

	status, resp := doJSONBody(t, newCustomSignalRouter(userID), "PUT", signalPath(symbol),
		`{"enabled":true,"conditions":[{"id":"a","side":"long","kind":"rsi","period":"9","op":"gt","value":50}]}`)
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "invalid request"}, resp)
}

func TestPutCustomSignal_InvalidCondition(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)

	status, resp := doJSONBody(t, newCustomSignalRouter(userID), "PUT", signalPath(symbol),
		`{"enabled":true,"conditions":[{"id":"a","side":"long","kind":"rsi","period":7,"op":"gt","value":50}]}`)
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "invalid conditions[0].period"}, resp)
	assert.Equal(t, 0, signalCount(t, userID))
}

func TestPutCustomSignal_EnabledWithoutConditions(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)

	status, resp := doJSONBody(t, r, "PUT", signalPath(symbol), `{"enabled":true,"conditions":[]}`)
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "enabled requires conditions"}, resp)

	status, _ = doJSONBody(t, r, "PUT", signalPath(symbol), `{"enabled":false,"conditions":[]}`)
	assert.Equal(t, 200, status)
}

func TestPutCustomSignal_TooManyConditions(t *testing.T) {
	rule := signalRuleFor(t, 0)
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)

	status, resp := doJSONBody(t, r, "PUT", signalPath(symbol), conditionsBody(true, rule.ConditionsPerSide+1, 0))
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "too many conditions"}, resp)

	status, resp = doJSONBody(t, r, "PUT", signalPath(symbol), conditionsBody(true, rule.ConditionsPerSide, rule.ConditionsPerSide))
	assert.Equal(t, 200, status, "body: %v", resp)
}

func TestPutCustomSignal_LimitReached(t *testing.T) {
	rule := signalRuleFor(t, 0)
	symbols := enabledSymbols(t, rule.Stocks+1)
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)

	for _, s := range symbols[:rule.Stocks] {
		status, resp := doJSONBody(t, r, "PUT", signalPath(s), oneCondition)
		require.Equal(t, 200, status, "body: %v", resp)
	}
	status, resp := doJSONBody(t, r, "PUT", signalPath(symbols[rule.Stocks]), oneCondition)
	assert.Equal(t, 409, status)
	assert.Equal(t, map[string]any{"error": "custom signal limit reached", "limit": float64(rule.Stocks)}, resp)
	assert.Equal(t, rule.Stocks, signalCount(t, userID))
}

func TestPutCustomSignal_UpdateAtLimit(t *testing.T) {
	rule := signalRuleFor(t, 0)
	symbols := enabledSymbols(t, rule.Stocks)
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)
	base := time.Now().Add(-time.Hour)
	for i, s := range symbols {
		addSignalRow(t, userID, s, true, base.Add(time.Duration(i)*time.Minute))
	}

	status, resp := doJSONBody(t, r, "PUT", signalPath(symbols[0]), oneCondition)
	assert.Equal(t, 200, status, "body: %v", resp)
	assert.Equal(t, rule.Stocks, signalCount(t, userID))
}

func TestPutCustomSignal_ActiveRanksDisabledToo(t *testing.T) {
	rule := signalRuleFor(t, 1)
	if rule.Stocks < 1 {
		t.Skip("level 1 allows no stocks")
	}
	symbols := enabledSymbols(t, rule.Stocks+1)
	userID := newFavoritesUser(t, 1)
	r := newCustomSignalRouter(userID)
	base := time.Now().Add(-time.Hour)
	// Stocks disabled rows are created first; the next row ranks beyond the limit.
	for i := 0; i < rule.Stocks; i++ {
		addSignalRow(t, userID, symbols[i], false, base.Add(time.Duration(i)*time.Minute))
	}
	last := symbols[rule.Stocks]
	addSignalRow(t, userID, last, true, base.Add(time.Hour/2))

	_, got := doJSON(t, r, "GET", signalPath(last))
	assert.Equal(t, false, got.(map[string]any)["active"])
	_, got = doJSON(t, r, "GET", signalPath(symbols[0]))
	assert.Equal(t, true, got.(map[string]any)["active"])
}

func TestPutCustomSignal_ConcurrentRespectsLimit(t *testing.T) {
	rule := signalRuleFor(t, 0)
	symbols := enabledSymbols(t, rule.Stocks+2)
	userID := newFavoritesUser(t, 0)
	r := newCustomSignalRouter(userID)
	base := time.Now().Add(-time.Hour)
	for i := 0; i < rule.Stocks-1; i++ {
		addSignalRow(t, userID, symbols[i], true, base.Add(time.Duration(i)*time.Minute))
	}

	codes := make([]int, 0, 3)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range symbols[rule.Stocks-1 : rule.Stocks+2] {
		wg.Add(1)
		go func(s string) {
			defer wg.Done()
			code := putStatus(r, s, oneCondition)
			mu.Lock()
			codes = append(codes, code)
			mu.Unlock()
		}(s)
	}
	wg.Wait()

	counts := map[int]int{}
	for _, c := range codes {
		counts[c]++
	}
	assert.Equal(t, map[int]int{200: 1, 409: 2}, counts)
	assert.Equal(t, rule.Stocks, signalCount(t, userID))
}

func TestPutCustomSignal_UnknownLevel(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 99)

	status, resp := doJSONBody(t, newCustomSignalRouter(userID), "PUT", signalPath(symbol), oneCondition)
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, resp)
	assert.Equal(t, 0, signalCount(t, userID))
}
