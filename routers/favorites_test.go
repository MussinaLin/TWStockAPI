package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Favorites fixtures ──

// newFavoritesUser inserts a throwaway users row and returns its id. Cleanup
// deletes the row, which cascades to its user_favorite_stocks rows.
func newFavoritesUser(t *testing.T, level int) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO users (google_sub, email, name, member_level)
		 VALUES ($1, 'favorites-test@example.com', 'favorites test', $2)
		 RETURNING id::text`,
		"test-favorites-"+uuid.NewString(), level).Scan(&id)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.Pool().Exec(context.Background(), "DELETE FROM users WHERE id = $1::uuid", id)
		assert.NoError(t, err)
	})
	return id
}

// newFavoritesRouter mounts the favorites routes behind a fake login that
// authenticates every request as userID.
func newFavoritesRouter(userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	fakeLogin := func(c *gin.Context) {
		c.Set(auth.ContextUserKey, auth.User{ID: userID, Email: "favorites-test@example.com"})
		c.Next()
	}
	RegisterFavorites(r.Group("/api"), fakeLogin)
	return r
}

// groupRuleFor reads the configured group rule for level from user_config.
func groupRuleFor(t *testing.T, level int) groupRule {
	t.Helper()
	rule, err := loadFavoriteGroups(context.Background(), db.Pool(), level)
	require.NoError(t, err, "apply db/user_favorites.sql (or the v2 migration) first")
	return rule
}

// enabledSymbols returns n enabled stock symbols, skipping if fewer exist.
func enabledSymbols(t *testing.T, n int) []string {
	t.Helper()
	rows, err := db.Pool().Query(context.Background(),
		"SELECT symbol FROM stocks WHERE enabled ORDER BY symbol LIMIT $1", n)
	require.NoError(t, err)
	symbols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	if len(symbols) < n {
		t.Skipf("need %d enabled stocks, found %d", n, len(symbols))
	}
	return symbols
}

// disabledSymbol returns one stock with enabled = false, skipping if none exist.
func disabledSymbol(t *testing.T) string {
	t.Helper()
	var symbol string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT symbol FROM stocks WHERE NOT enabled ORDER BY symbol LIMIT 1").Scan(&symbol)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no disabled stocks")
	}
	require.NoError(t, err)
	return symbol
}

// addFavoriteRow inserts a favorite directly, bypassing the POST checks.
func addFavoriteRow(t *testing.T, userID string, groupNo int, symbol string, created time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO user_favorite_stocks (user_id, group_no, symbol, created_time)
		 VALUES ($1::uuid, $2, $3, $4)`,
		userID, groupNo, symbol, created)
	require.NoError(t, err)
}

func groupCount(t *testing.T, userID string, groupNo int) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1::uuid AND group_no = $2",
		userID, groupNo).Scan(&n)
	require.NoError(t, err)
	return n
}

// groupsOf returns the "groups" array of a GET /api/favorites body.
func groupsOf(t *testing.T, body any) []map[string]any {
	t.Helper()
	m, ok := body.(map[string]any)
	require.True(t, ok, "expected object, got %T", body)
	raw, ok := m["groups"].([]any)
	require.True(t, ok, "expected groups array, got %T", m["groups"])
	groups := make([]map[string]any, len(raw))
	for i, g := range raw {
		groups[i], ok = g.(map[string]any)
		require.True(t, ok, "group %d not an object", i)
	}
	return groups
}

func groupPath(groupNo int) string {
	return fmt.Sprintf("/api/favorites/groups/%d", groupNo)
}

// ── GET /api/favorites ──

func TestListFavorites_AllGroupsEmpty(t *testing.T) {
	rule := groupRuleFor(t, 0)
	userID := newFavoritesUser(t, 0)

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)

	m := body.(map[string]any)
	assert.Equal(t, float64(rule.Groups), m["max_groups"])
	assert.Equal(t, float64(rule.StocksPerGroup), m["max_stocks_per_group"])
	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	for i, g := range groups {
		assert.Equal(t, float64(i+1), g["group_no"])
		assert.Equal(t, groupName(i+1), g["name"])
		assert.Equal(t, float64(0), g["count"])
		assert.Equal(t, []any{}, g["items"])
	}
}

func TestListFavorites_Level1Groups(t *testing.T) {
	rule := groupRuleFor(t, 1)
	userID := newFavoritesUser(t, 1)

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)

	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	assert.Equal(t, groupName(rule.Groups), groups[rule.Groups-1]["name"])
	assert.Equal(t, float64(rule.StocksPerGroup), body.(map[string]any)["max_stocks_per_group"])
}

func TestListFavorites_GroupsAndOrder(t *testing.T) {
	symbols := enabledSymbols(t, 2)
	userID := newFavoritesUser(t, 0)
	now := time.Now()
	addFavoriteRow(t, userID, 1, symbols[0], now.Add(-time.Hour))
	addFavoriteRow(t, userID, 1, symbols[1], now)
	addFavoriteRow(t, userID, 2, symbols[0], now)

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)

	assert.Equal(t, float64(2), groups[0]["count"])
	g1 := groups[0]["items"].([]any)
	require.Len(t, g1, 2)
	first := g1[0].(map[string]any)
	requireKeys(t, first, "symbol", "name", "created_time")
	assert.Equal(t, symbols[1], first["symbol"], "newest first")
	assert.Equal(t, symbols[0], g1[1].(map[string]any)["symbol"])
	_, err := time.Parse(time.RFC3339Nano, first["created_time"].(string))
	assert.NoError(t, err, "created_time should be RFC 3339, got %v", first["created_time"])

	assert.Equal(t, float64(1), groups[1]["count"])
	g2 := groups[1]["items"].([]any)
	require.Len(t, g2, 1)
	assert.Equal(t, symbols[0], g2[0].(map[string]any)["symbol"])
}

func TestListFavorites_IncludesDisabledStock(t *testing.T) {
	symbol := disabledSymbol(t)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, 1, symbol, time.Now())

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	items := groupsOf(t, body)[0]["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, symbol, items[0].(map[string]any)["symbol"])
}

func TestListFavorites_OnlyOwn(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	addFavoriteRow(t, owner, 1, symbols[0], time.Now())

	status, body := doJSON(t, newFavoritesRouter(other), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	for _, g := range groupsOf(t, body) {
		assert.Equal(t, float64(0), g["count"])
	}
}

func TestListFavorites_Downgraded(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups >= 8 {
		t.Skipf("level 0 has %d groups; this test needs fewer than 8", rule.Groups)
	}
	n := rule.StocksPerGroup + 4
	symbols := enabledSymbols(t, n)
	userID := newFavoritesUser(t, 0)
	for _, s := range symbols {
		addFavoriteRow(t, userID, 1, s, time.Now())
	}
	addFavoriteRow(t, userID, 8, symbols[0], time.Now())

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	assert.Equal(t, float64(n), groups[0]["count"])
	assert.Len(t, groups[0]["items"], n)
	for _, g := range groups {
		assert.NotEqual(t, float64(8), g["group_no"])
	}
}

func TestListFavorites_OnlyHiddenGroups(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups >= 8 {
		t.Skipf("level 0 has %d groups; this test needs fewer than 8", rule.Groups)
	}
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, 8, symbols[0], time.Now())

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	for _, g := range groups {
		assert.Equal(t, float64(0), g["count"])
	}
}

func TestListFavorites_UnknownLevel(t *testing.T) {
	userID := newFavoritesUser(t, 99)
	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
}

func TestListFavorites_UserGone(t *testing.T) {
	status, body := doJSON(t, newFavoritesRouter(uuid.NewString()), "GET", "/api/favorites")
	require.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}

// ── DELETE /api/favorites/groups/:group_no/:symbol ──

func TestDeleteFavorite_OnlyThatGroup(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, 1, symbols[0], time.Now())
	addFavoriteRow(t, userID, 2, symbols[0], time.Now())
	r := newFavoritesRouter(userID)

	status, body := doJSON(t, r, "DELETE", groupPath(1)+"/"+symbols[0])
	assert.Equal(t, 204, status)
	assert.Nil(t, body)
	assert.Equal(t, 0, groupCount(t, userID, 1))
	assert.Equal(t, 1, groupCount(t, userID, 2))

	status, _ = doJSON(t, r, "DELETE", groupPath(1)+"/"+symbols[0])
	assert.Equal(t, 204, status)
}

func TestDeleteFavorite_InvalidGroupNo(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	for _, g := range []string{"abc", "0", "-1", "1.5"} {
		t.Run(g, func(t *testing.T) {
			status, body := doJSON(t, r, "DELETE", "/api/favorites/groups/"+g+"/2330")
			assert.Equal(t, 400, status)
			assert.Equal(t, map[string]any{"error": "invalid request"}, body)
		})
	}
}

func TestDeleteFavorite_GroupNotFound(t *testing.T) {
	rule := groupRuleFor(t, 0)
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	hidden := rule.Groups + 1
	addFavoriteRow(t, userID, hidden, symbols[0], time.Now())
	r := newFavoritesRouter(userID)

	for _, g := range []int{hidden, 99999} {
		status, body := doJSON(t, r, "DELETE", groupPath(g)+"/"+symbols[0])
		assert.Equal(t, 404, status, "group_no %d", g)
		assert.Equal(t, map[string]any{"error": "group not found"}, body)
	}
	assert.Equal(t, 1, groupCount(t, userID, hidden), "hidden group's data must be kept")
}

func TestDeleteFavorite_InvalidSymbol(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	for name, s := range map[string]string{"nul": "%00", "invalid utf-8": "%FF"} {
		t.Run(name, func(t *testing.T) {
			status, body := doJSON(t, r, "DELETE", groupPath(1)+"/"+s)
			assert.Equal(t, 204, status)
			assert.Nil(t, body)
		})
	}
}

func TestDeleteFavorite_OnlyOwn(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	addFavoriteRow(t, owner, 1, symbols[0], time.Now())

	status, _ := doJSON(t, newFavoritesRouter(other), "DELETE", groupPath(1)+"/"+symbols[0])
	assert.Equal(t, 204, status)
	assert.Equal(t, 1, groupCount(t, owner, 1))
}

func TestDeleteFavorite_UnknownLevel(t *testing.T) {
	userID := newFavoritesUser(t, 99)
	status, body := doJSON(t, newFavoritesRouter(userID), "DELETE", groupPath(1)+"/2330")
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
}

func TestDeleteFavorite_UserGone(t *testing.T) {
	status, body := doJSON(t, newFavoritesRouter(uuid.NewString()), "DELETE", groupPath(1)+"/2330")
	assert.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}

// doJSONBody is doJSON with a JSON request body.
func doJSONBody(t *testing.T, r *gin.Engine, method, path, body string) (int, any) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var decoded any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &decoded), "decode body: %s", w.Body.String())
	}
	return w.Code, decoded
}

// postStatus sends one POST without testify (safe to call from goroutines).
func postStatus(r *gin.Engine, groupNo int, symbol string) int {
	req := httptest.NewRequest("POST", groupPath(groupNo), strings.NewReader(`{"symbol":"`+symbol+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code
}

// ── POST /api/favorites/groups/:group_no ──

func TestAddFavorite_CreatedThenIdempotent(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, body := doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 201, status, "body: %v", body)
	created := body.(map[string]any)
	requireKeys(t, created, "group_no", "symbol", "name", "created_time")
	assert.Equal(t, float64(1), created["group_no"])
	assert.Equal(t, symbol, created["symbol"])
	_, err := time.Parse(time.RFC3339Nano, created["created_time"].(string))
	assert.NoError(t, err)

	status, body = doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, created, body)
	assert.Equal(t, 1, groupCount(t, userID, 1))
}

func TestAddFavorite_SameStockInTwoGroups(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, _ := doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 201, status)
	status, body := doJSONBody(t, r, "POST", groupPath(2), `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 201, status)
	assert.Equal(t, float64(2), body.(map[string]any)["group_no"])

	status, body = doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	assert.Equal(t, float64(1), groups[0]["count"])
	assert.Equal(t, float64(1), groups[1]["count"])
}

func TestAddFavorite_TrimsSymbol(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", groupPath(1), `{"symbol":"  `+symbol+` "}`)
	require.Equal(t, 201, status, "body: %v", body)
	assert.Equal(t, symbol, body.(map[string]any)["symbol"])
}

func TestAddFavorite_GroupNoVariants(t *testing.T) {
	symbols := enabledSymbols(t, 2)
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, body := doJSONBody(t, r, "POST", "/api/favorites/groups/+1", `{"symbol":"`+symbols[0]+`"}`)
	require.Equal(t, 201, status, "body: %v", body)
	assert.Equal(t, float64(1), body.(map[string]any)["group_no"])

	status, body = doJSONBody(t, r, "POST", "/api/favorites/groups/01", `{"symbol":"`+symbols[1]+`"}`)
	require.Equal(t, 201, status, "body: %v", body)
	assert.Equal(t, float64(1), body.(map[string]any)["group_no"])
	assert.Equal(t, 2, groupCount(t, userID, 1))
}

func TestAddFavorite_InvalidRequest(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	invalid := map[string]struct{ group, payload string }{
		"group abc":        {"abc", `{"symbol":"` + symbol + `"}`},
		"group zero":       {"0", `{"symbol":"` + symbol + `"}`},
		"group negative":   {"-1", `{"symbol":"` + symbol + `"}`},
		"group fractional": {"1.5", `{"symbol":"` + symbol + `"}`},
		"invalid json":     {"1", `{`},
		"empty body":       {"1", ``},
		"missing symbol":   {"1", `{}`},
		"blank symbol":     {"1", `{"symbol":"   "}`},
		"non-string value": {"1", `{"symbol":2330}`},
		"nul symbol":       {"1", `{"symbol":"\u0000"}`},
		"nul in middle":    {"1", `{"symbol":"23\u000030"}`},
	}
	for name, tc := range invalid {
		t.Run(name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "POST", "/api/favorites/groups/"+tc.group, tc.payload)
			assert.Equal(t, 400, status)
			assert.Equal(t, map[string]any{"error": "invalid request"}, body)
		})
	}
	assert.Equal(t, 0, groupCount(t, userID, 1))
}

func TestAddFavorite_GroupNotFound(t *testing.T) {
	rule := groupRuleFor(t, 0)
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, g := range []int{rule.Groups + 1, 99999} {
		status, body := doJSONBody(t, r, "POST", groupPath(g), `{"symbol":"`+symbol+`"}`)
		assert.Equal(t, 404, status, "group_no %d", g)
		assert.Equal(t, map[string]any{"error": "group not found"}, body)
	}
}

func TestAddFavorite_StockNotFound(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	for name, symbol := range map[string]string{"unknown": "__NOPE__", "overlong": "12345678901234567890"} {
		t.Run(name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
			assert.Equal(t, 404, status)
			assert.Equal(t, map[string]any{"error": "stock not found"}, body)
		})
	}
	assert.Equal(t, 0, groupCount(t, userID, 1))
}

func TestAddFavorite_DisabledStock(t *testing.T) {
	symbol := disabledSymbol(t)
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 404, status)
	assert.Equal(t, map[string]any{"error": "stock not found"}, body)
}

func TestAddFavorite_GroupFull(t *testing.T) {
	rule := groupRuleFor(t, 0)
	n := rule.StocksPerGroup
	symbols := enabledSymbols(t, n+1)
	userID := newFavoritesUser(t, 0)
	created := time.Now().Add(-time.Minute)
	for _, s := range symbols[:n] {
		addFavoriteRow(t, userID, 1, s, created)
	}
	r := newFavoritesRouter(userID)

	// 群組已滿時，加入已在群組裡的股票回 200 並附上既有資料
	status, body := doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbols[0]+`"}`)
	require.Equal(t, 200, status)
	existing := body.(map[string]any)
	assert.Equal(t, float64(1), existing["group_no"])
	assert.Equal(t, symbols[0], existing["symbol"])
	assert.NotEmpty(t, existing["name"])
	got, err := time.Parse(time.RFC3339Nano, existing["created_time"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, created, got, time.Millisecond)

	status, body = doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbols[n]+`"}`)
	assert.Equal(t, 409, status)
	assert.Equal(t, map[string]any{"error": "favorite group full", "limit": float64(n)}, body)
	assert.Equal(t, n, groupCount(t, userID, 1))

	// 其他群組照樣能加入
	status, _ = doJSONBody(t, r, "POST", groupPath(2), `{"symbol":"`+symbols[n]+`"}`)
	assert.Equal(t, 201, status)
}

func TestAddFavorite_Downgraded(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups >= 8 {
		t.Skipf("level 0 has %d groups; this test needs fewer than 8", rule.Groups)
	}
	over := rule.StocksPerGroup + 4
	symbols := enabledSymbols(t, over+1)
	userID := newFavoritesUser(t, 0)
	for _, s := range symbols[:over] {
		addFavoriteRow(t, userID, 1, s, time.Now())
	}
	r := newFavoritesRouter(userID)

	status, body := doJSONBody(t, r, "POST", groupPath(1), `{"symbol":"`+symbols[over]+`"}`)
	assert.Equal(t, 409, status)
	assert.Equal(t, map[string]any{"error": "favorite group full", "limit": float64(rule.StocksPerGroup)}, body)

	status, body = doJSONBody(t, r, "POST", groupPath(8), `{"symbol":"`+symbols[over]+`"}`)
	assert.Equal(t, 404, status)
	assert.Equal(t, map[string]any{"error": "group not found"}, body)
}

func TestAddFavorite_ConcurrentRequestsRespectGroupLimit(t *testing.T) {
	rule := groupRuleFor(t, 0)
	n := rule.StocksPerGroup
	symbols := enabledSymbols(t, n+5)
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	type result struct{ group, code int }
	results := make([]result, 0, 2*len(symbols))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, groupNo := range []int{1, 2} {
		for _, s := range symbols {
			wg.Add(1)
			go func(groupNo int, s string) {
				defer wg.Done()
				code := postStatus(r, groupNo, s)
				mu.Lock()
				results = append(results, result{groupNo, code})
				mu.Unlock()
			}(groupNo, s)
		}
	}
	wg.Wait()

	counts := map[int]map[int]int{1: {}, 2: {}}
	for _, res := range results {
		counts[res.group][res.code]++
	}
	for _, groupNo := range []int{1, 2} {
		assert.Equal(t, map[int]int{201: n, 409: 5}, counts[groupNo], "group %d", groupNo)
		assert.Equal(t, n, groupCount(t, userID, groupNo), "group %d", groupNo)
	}
}

func TestAddFavorite_UnknownLevel(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 99)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
	assert.Equal(t, 0, groupCount(t, userID, 1))
}

func TestAddFavorite_UserGone(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]

	status, body := doJSONBody(t, newFavoritesRouter(uuid.NewString()), "POST", groupPath(1), `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}
