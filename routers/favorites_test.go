package routers

import (
	"context"
	"encoding/json"
	"errors"
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

// favoriteLimitFor reads the configured limit for level from user_config.
func favoriteLimitFor(t *testing.T, level int) int {
	t.Helper()
	limit, err := loadFavoriteLimit(context.Background(), db.Pool(), level)
	require.NoError(t, err, "apply db/user_favorites.sql first")
	return limit
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
func addFavoriteRow(t *testing.T, userID, symbol string, created time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		"INSERT INTO user_favorite_stocks (user_id, symbol, created_time) VALUES ($1::uuid, $2, $3)",
		userID, symbol, created)
	require.NoError(t, err)
}

func favoriteCount(t *testing.T, userID string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1::uuid", userID).Scan(&n)
	require.NoError(t, err)
	return n
}

// ── GET /api/favorites ──

func TestListFavorites_Empty(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok, "expected object, got %T", body)
	assert.Equal(t, float64(favoriteLimitFor(t, 0)), m["limit"])
	assert.Equal(t, float64(0), m["count"])
	assert.Equal(t, []any{}, m["items"])
}

func TestListFavorites_NewestFirst(t *testing.T) {
	symbols := enabledSymbols(t, 2)
	userID := newFavoritesUser(t, 0)
	now := time.Now()
	addFavoriteRow(t, userID, symbols[0], now.Add(-time.Hour))
	addFavoriteRow(t, userID, symbols[1], now)

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)

	m := body.(map[string]any)
	assert.Equal(t, float64(2), m["count"])
	items := m["items"].([]any)
	require.Len(t, items, 2)

	first := items[0].(map[string]any)
	requireKeys(t, first, "symbol", "name", "created_time")
	assert.Equal(t, symbols[1], first["symbol"])
	assert.Equal(t, symbols[0], items[1].(map[string]any)["symbol"])

	_, err := time.Parse(time.RFC3339Nano, first["created_time"].(string))
	assert.NoError(t, err, "created_time should be RFC 3339, got %v", first["created_time"])
}

func TestListFavorites_IncludesDisabledStock(t *testing.T) {
	symbol := disabledSymbol(t)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, symbol, time.Now())

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	items := body.(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, symbol, items[0].(map[string]any)["symbol"])
}

func TestListFavorites_OnlyOwn(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	addFavoriteRow(t, owner, symbols[0], time.Now())

	status, body := doJSON(t, newFavoritesRouter(other), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	assert.Equal(t, float64(0), body.(map[string]any)["count"])
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

// ── DELETE /api/favorites/:symbol ──

func TestDeleteFavorite_Idempotent(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, symbols[0], time.Now())
	r := newFavoritesRouter(userID)

	status, body := doJSON(t, r, "DELETE", "/api/favorites/"+symbols[0])
	assert.Equal(t, 204, status)
	assert.Nil(t, body)
	assert.Equal(t, 0, favoriteCount(t, userID))

	status, _ = doJSON(t, r, "DELETE", "/api/favorites/"+symbols[0])
	assert.Equal(t, 204, status)
}

func TestDeleteFavorite_OnlyOwn(t *testing.T) {
	symbols := enabledSymbols(t, 1)
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	addFavoriteRow(t, owner, symbols[0], time.Now())

	status, _ := doJSON(t, newFavoritesRouter(other), "DELETE", "/api/favorites/"+symbols[0])
	assert.Equal(t, 204, status)
	assert.Equal(t, 1, favoriteCount(t, owner))
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

// ── POST /api/favorites ──

func TestAddFavorite_CreatedThenIdempotent(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, body := doJSONBody(t, r, "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 201, status, "body: %v", body)
	created := body.(map[string]any)
	requireKeys(t, created, "symbol", "name", "created_time")
	assert.Equal(t, symbol, created["symbol"])
	_, err := time.Parse(time.RFC3339Nano, created["created_time"].(string))
	assert.NoError(t, err)

	status, body = doJSONBody(t, r, "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, created, body)
	assert.Equal(t, 1, favoriteCount(t, userID))
}

func TestAddFavorite_TrimsSymbol(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", "/api/favorites", `{"symbol":"  `+symbol+` "}`)
	require.Equal(t, 201, status, "body: %v", body)
	assert.Equal(t, symbol, body.(map[string]any)["symbol"])
}

func TestAddFavorite_InvalidRequest(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	cases := map[string]string{
		"invalid json":     `{`,
		"empty body":       ``,
		"missing symbol":   `{}`,
		"blank symbol":     `{"symbol":"   "}`,
		"non-string value": `{"symbol":2330}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "POST", "/api/favorites", payload)
			assert.Equal(t, 400, status)
			assert.Equal(t, map[string]any{"error": "invalid request"}, body)
		})
	}
}

func TestAddFavorite_StockNotFound(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)
	cases := map[string]string{
		"unknown":  "__NOPE__",
		"overlong": "12345678901234567890",
	}
	for name, symbol := range cases {
		t.Run(name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
			assert.Equal(t, 404, status)
			assert.Equal(t, map[string]any{"error": "stock not found"}, body)
		})
	}
	assert.Equal(t, 0, favoriteCount(t, userID))
}

func TestAddFavorite_DisabledStock(t *testing.T) {
	symbol := disabledSymbol(t)
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 404, status)
	assert.Equal(t, map[string]any{"error": "stock not found"}, body)
}

func TestAddFavorite_LimitReached(t *testing.T) {
	limit := favoriteLimitFor(t, 0)
	symbols := enabledSymbols(t, limit+1)
	userID := newFavoritesUser(t, 0)
	for _, s := range symbols[:limit] {
		addFavoriteRow(t, userID, s, time.Now())
	}
	r := newFavoritesRouter(userID)

	// 已經在清單中的股票，即使已額滿也回 200
	status, _ := doJSONBody(t, r, "POST", "/api/favorites", `{"symbol":"`+symbols[0]+`"}`)
	assert.Equal(t, 200, status)

	status, body := doJSONBody(t, r, "POST", "/api/favorites", `{"symbol":"`+symbols[limit]+`"}`)
	assert.Equal(t, 409, status)
	assert.Equal(t, map[string]any{"error": "favorite limit reached", "limit": float64(limit)}, body)
	assert.Equal(t, limit, favoriteCount(t, userID))
}

func TestAddFavorite_ConcurrentRequestsRespectLimit(t *testing.T) {
	limit := favoriteLimitFor(t, 0)
	symbols := enabledSymbols(t, limit+5)
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	codes := make([]int, len(symbols))
	var wg sync.WaitGroup
	for i, s := range symbols {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			req := httptest.NewRequest("POST", "/api/favorites", strings.NewReader(`{"symbol":"`+s+`"}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
		}(i, s)
	}
	wg.Wait()

	counts := map[int]int{}
	for _, c := range codes {
		counts[c]++
	}
	assert.Equal(t, map[int]int{201: limit, 409: 5}, counts)
	assert.Equal(t, limit, favoriteCount(t, userID))
}

func TestAddFavorite_UnknownLevel(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]
	userID := newFavoritesUser(t, 99)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
	assert.Equal(t, 0, favoriteCount(t, userID))
}

func TestAddFavorite_UserGone(t *testing.T) {
	symbol := enabledSymbols(t, 1)[0]

	status, body := doJSONBody(t, newFavoritesRouter(uuid.NewString()), "POST", "/api/favorites", `{"symbol":"`+symbol+`"}`)
	assert.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}
