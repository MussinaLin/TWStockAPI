# 我的最愛個股 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `/api/favorites`（GET / POST / DELETE），讓登入的使用者管理自己的我的最愛個股，數量上限依 `member_level` 由 `user_config` 設定。

**Architecture:** `auth` 新增 `RequireLogin`（不看 `JWT_TOKEN_ENABLE`，一律驗證；context 已有 user 時直接放行）與 `CurrentUser`。原本 `RequireAuth` 的驗證流程抽成共用的 `authenticate`，對外行為不變。`routers/favorites.go` 實作三支 handler：POST 在 transaction 裡先用 `SELECT ... FOR UPDATE` 鎖住 user 的 row，讓同一個 user 的請求排隊處理，再檢查上限。上限設定存在 `user_config` 的 JSONB，由純函式 `parseFavoriteLimit` 解析。

**Tech Stack:** Go 1.25、Gin v1.12、pgx/v5 v5.9.1、github.com/google/uuid v1.6.0、testify。

**Spec:** `docs/superpowers/specs/2026-10-05-user-favorite-stocks-design.md`

## Global Constraints

- Go module 名稱是 `main`（import path 例如 `main/db`、`main/auth`）。不新增任何依賴。
- 設定 key 固定為 `FAVORITE_STOCKS_LIMIT`；初始值固定為 `{"0": 10, "1": 15, "2": 20}`；JSON 的 key 是 `member_level` 的字串。
- 回應 body 必須完全相同：
  - 400 `{"error":"invalid request"}`
  - 404 `{"error":"stock not found"}`
  - 409 `{"error":"favorite limit reached","limit":<int>}`
  - 401 `{"error":"unauthorized"}` / `{"error":"token expired"}`
  - 500 `{"detail":"Internal server error"}`
- 狀態碼：POST 新增成功 201、本來就已加入 200（即使已額滿）；DELETE 一律 204，沒有 body；GET 200。
- GET 回應格式為 `{"limit":int,"count":int,"items":[{"symbol","name","created_time"}]}`；`items` 依 `created_time DESC` 排序，清單是空的時候回 `[]`，不回 `null`；`created_time` 是 RFC 3339 字串。
- 只能加入 `stocks.enabled = true` 的股票；加入後被停用的股票 GET 照樣列出。
- `is_og_member` 不影響上限。
- 找不到 `FAVORITE_STOCKS_LIMIT`、JSON 格式錯誤、找不到該 level，或值不是 ≥ 0 的整數，一律回 500。
- `/api/favorites/*` 一律要求登入，不受 `JWT_TOKEN_ENABLE` 影響；不可修改共用 DB 的 `JWT_TOKEN_ENABLE`。
- `RequireAuth` 的對外行為不可改變；現有測試必須全部維持綠燈。
- Log 不可包含 token 內容。
- 測試風格：testify `assert` / `require`、`httptest`、`gin.TestMode`。routers 的整合測試會實際寫入 `.env` 的 `DATABASE_URL` 指向的 DB（使用者已同意），測試建立的資料必須在 `t.Cleanup` 清除。
- 每次改到 API 時都要同步更新 `CLAUDE.md` 與 `README.md`（Task 4）。
- Commit message 不加 `Co-Authored-By:` 或任何署名行。

## Review Focus

1. **POST 的 symbol 有前後空白或超過 10 字元**：空白要先去掉再處理；超過 10 字元的 symbol 回 404，不能回 500。由 Task 3 的 `TestAddFavorite_TrimsSymbol` 與 `TestAddFavorite_StockNotFound`（`overlong`）負責。
2. **已額滿時再加入已在清單中的股票**：要回 200 並附上既有資料，不能回 409。由 Task 3 的 `TestAddFavorite_LimitReached` 負責。
3. **看到或刪到別人的我的最愛**：GET 只能列出自己的，DELETE 只能刪自己的。由 Task 2 的 `TestListFavorites_OnlyOwn` 與 `TestDeleteFavorite_OnlyOwn` 負責。
4. **股票在加入後被停用**：GET 仍然要列出。由 Task 2 的 `TestListFavorites_IncludesDisabledStock` 負責。
5. **token 有效，但 user 的 row 已被刪除，或 member_level 沒有對應的上限設定**：前者回 401，後者回 500，都不能 panic 或回 200。由 Task 2 的 `TestListFavorites_UserGone` / `TestListFavorites_UnknownLevel`，以及 Task 3 的 `TestAddFavorite_UserGone` / `TestAddFavorite_UnknownLevel` 負責。

---

## File Structure

| File | 動作 | 職責 |
|---|---|---|
| `auth/middleware.go` | Modify | 抽出 `authenticate`；新增 `RequireLogin`、`CurrentUser` |
| `auth/middleware_test.go` | Modify | `serve` 改用通用的 `serveWith`；新增 `RequireLogin` / `CurrentUser` 測試 |
| `db/user_favorites.sql` | Create | `user_config`、`user_favorite_stocks` 的 DDL，以及上限的初始設定 |
| `routers/favorites.go` | Create | `RegisterFavorites`、三支 handler、`parseFavoriteLimit`、`loadFavoriteLimit` |
| `routers/favorites_limit_test.go` | Create | `parseFavoriteLimit` 測試（不碰 DB，但 routers package 沒設 DB 時會整包 skip） |
| `routers/favorites_test.go` | Create | GET / POST / DELETE 整合測試，包含同時送出多個 POST 的測試 |
| `main.go` | Modify | CORS 加上 `POST`、`DELETE`；註冊 favorites routes |
| `CLAUDE.md`、`README.md`、`docs/frontend-api-auth-integration.md` | Modify | 文件同步 |

---

### Task 1: `auth.RequireLogin` 與 `auth.CurrentUser`

**Files:**
- Modify: `auth/middleware.go`
- Test: `auth/middleware_test.go`

**Interfaces:**
- Consumes：既有的 `UserStore`、`User`、`ContextUserKey`、`ParseToken`、`ErrTokenExpired`、`bearerToken`、`abortUnauthorized`、`abortServerError`、`FlagSource`。
- Produces：
  - `func RequireLogin(users UserStore, secret []byte) gin.HandlerFunc`
  - `func CurrentUser(c *gin.Context) (User, bool)`
  - 內部函式：`func authenticate(c *gin.Context, users UserStore, secret []byte) bool`

- [ ] **Step 1: 把測試 helper `serve` 改成通用的 `serveWith`，並寫新的失敗測試**

在 `auth/middleware_test.go` 把現有的 `serve` 函式整個換成下面兩個函式（`serve` 的簽名不變，現有測試不用改）：

```go
// serve sends one GET /api/ping through RequireAuth.
func serve(t *testing.T, flags FlagSource, users UserStore, secret []byte, authHeader string) (int, map[string]any) {
	t.Helper()
	return serveWith(t, authHeader, RequireAuth(flags, users, secret))
}

// serveWith sends one GET /api/ping through the given middleware chain. The
// handler echoes the authenticated user's email so tests can see what reached
// the context.
func serveWith(t *testing.T, authHeader string, chain ...gin.HandlerFunc) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", chain...)
	api.GET("/ping", func(c *gin.Context) {
		body := gin.H{"ok": true}
		if u, ok := CurrentUser(c); ok {
			body["email"] = u.Email
		}
		c.JSON(http.StatusOK, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
	return w.Code, body
}
```

在檔案最後加上：

```go
// presetUser simulates RequireAuth having already authenticated the request.
func presetUser(u User) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextUserKey, u)
		c.Next()
	}
}

func TestRequireLoginValidTokenSetsUser(t *testing.T) {
	users := knownUsers()
	code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(users, testSecret))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "user@example.com", body["email"])
	assert.Equal(t, 1, users.calls)
}

func TestRequireLoginExpiredToken(t *testing.T) {
	code, body := serveWith(t, bearer(t, testUserID, -time.Hour), RequireLogin(knownUsers(), testSecret))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "token expired"}, body)
}

func TestRequireLoginUnauthorized(t *testing.T) {
	cases := map[string]string{
		"missing header":   "",
		"garbage token":    "Bearer garbage",
		"bad signature":    "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims(testUserID, time.Hour)),
		"unknown user":     bearer(t, "00000000-0000-0000-0000-000000000000", time.Hour),
		"non-uuid subject": bearer(t, "not-a-uuid", time.Hour),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serveWith(t, header, RequireLogin(knownUsers(), testSecret))
			assert.Equal(t, http.StatusUnauthorized, code)
			assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
		})
	}
}

func TestRequireLoginSkipsLookupWhenUserPresent(t *testing.T) {
	users := knownUsers()
	preset := User{ID: testUserID, Email: "preset@example.com"}
	code, body := serveWith(t, "", presetUser(preset), RequireLogin(users, testSecret))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "preset@example.com", body["email"])
	assert.Zero(t, users.calls)
}

func TestRequireLoginServerErrors(t *testing.T) {
	serverError := map[string]any{"detail": "Internal server error"}

	t.Run("secret not set", func(t *testing.T) {
		code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(knownUsers(), nil))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		users := &fakeUsers{err: errors.New("db down")}
		code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})
}

// RequireLogin must enforce login even while JWT_TOKEN_ENABLE is off, and must
// not look the user up a second time when RequireAuth already did.
func TestRequireLoginBehindRequireAuth(t *testing.T) {
	t.Run("flag off, no token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, "",
			RequireAuth(fakeFlags{enabled: false}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusUnauthorized, code)
		assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
	})

	t.Run("flag off, valid token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, bearer(t, testUserID, time.Hour),
			RequireAuth(fakeFlags{enabled: false}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "user@example.com", body["email"])
		assert.Equal(t, 1, users.calls)
	})

	t.Run("flag on, valid token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, bearer(t, testUserID, time.Hour),
			RequireAuth(fakeFlags{enabled: true}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "user@example.com", body["email"])
		assert.Equal(t, 1, users.calls)
	})
}

func TestCurrentUserMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, ok := CurrentUser(c)
	assert.False(t, ok)
}
```

- [ ] **Step 2: 執行測試，確認失敗**

Run: `go test ./auth/ -run 'RequireLogin|CurrentUser|RequireAuth' -v`
Expected：編譯失敗，`undefined: CurrentUser`、`undefined: RequireLogin`。

- [ ] **Step 3: 實作**

把 `auth/middleware.go` 的 `RequireAuth` 整個函式換成下面的程式碼（`bearerToken`、`abortUnauthorized`、`abortServerError` 不動）：

```go
// RequireAuth returns middleware that, while the JWT_TOKEN_ENABLE flag is on,
// requires a valid AccountService Bearer JWT whose subject exists in users.
// While the flag is off, requests pass through without reading Authorization.
func RequireAuth(flags FlagSource, users UserStore, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, err := flags.Enabled(c.Request.Context())
		if err != nil {
			abortServerError(c, fmt.Errorf("auth: load %s: %w", FlagKey, err))
			return
		}
		if !enabled {
			c.Next()
			return
		}
		if authenticate(c, users, secret) {
			c.Next()
		}
	}
}

// RequireLogin returns middleware that always requires a valid AccountService
// Bearer JWT, regardless of JWT_TOKEN_ENABLE. If RequireAuth already
// authenticated the request, it passes through without looking the user up again.
func RequireLogin(users UserStore, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := CurrentUser(c); ok {
			c.Next()
			return
		}
		if authenticate(c, users, secret) {
			c.Next()
		}
	}
}

// CurrentUser returns the User stored by RequireAuth or RequireLogin.
func CurrentUser(c *gin.Context) (User, bool) {
	v, ok := c.Get(ContextUserKey)
	if !ok {
		return User{}, false
	}
	u, ok := v.(User)
	return u, ok
}

// authenticate verifies the Bearer JWT and stores the matching User in the
// context. On failure it aborts with the error response and returns false.
func authenticate(c *gin.Context, users UserStore, secret []byte) bool {
	if len(secret) == 0 {
		abortServerError(c, errors.New("auth: JWT_SECRET is not set"))
		return false
	}

	tokenStr, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		abortUnauthorized(c, errors.New("auth: missing bearer token"))
		return false
	}

	sub, err := ParseToken(secret, tokenStr)
	if errors.Is(err, ErrTokenExpired) {
		_ = c.Error(fmt.Errorf("auth: %w", err))
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
		return false
	}
	if err != nil {
		abortUnauthorized(c, fmt.Errorf("auth: %w", err))
		return false
	}

	id, err := uuid.Parse(sub)
	if err != nil {
		abortUnauthorized(c, errors.New("auth: subject is not a uuid"))
		return false
	}

	user, err := users.GetByID(c.Request.Context(), id.String())
	if errors.Is(err, ErrUserNotFound) {
		abortUnauthorized(c, fmt.Errorf("auth: %w", err))
		return false
	}
	if err != nil {
		abortServerError(c, fmt.Errorf("auth: lookup user: %w", err))
		return false
	}

	c.Set(ContextUserKey, user)
	return true
}
```

- [ ] **Step 4: 執行 auth 全部的測試，確認通過**

Run: `go test ./auth/ -v`
Expected：全部 PASS，包含原本的 `TestRequireAuth*`。沒設 DB 時 `TestPgxUserStore*` 顯示 SKIP。

- [ ] **Step 5: Commit**

```bash
git add auth/middleware.go auth/middleware_test.go
git commit -m "feat: 新增 auth.RequireLogin，不論開關一律要求登入"
```

---

### Task 2: 資料表、上限解析、GET 與 DELETE

**Files:**
- Create: `db/user_favorites.sql`
- Create: `routers/favorites.go`
- Test: `routers/favorites_limit_test.go`
- Test: `routers/favorites_test.go`

**Interfaces:**
- Consumes：Task 1 的 `auth.CurrentUser(c *gin.Context) (auth.User, bool)`、`auth.ContextUserKey`、`auth.User{ID, Email}`；`db.Pool()`。
- Produces（Task 3 與 Task 4 會用到）：
  - `func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc)`：Task 2 只註冊 GET 和 DELETE。
  - `const favoriteLimitKey = "FAVORITE_STOCKS_LIMIT"`
  - `type favoriteItem struct { Symbol string; Name string; CreatedTime time.Time }`（JSON 欄位：`symbol`、`name`、`created_time`）
  - `type querier interface { QueryRow(ctx context.Context, sql string, args ...any) pgx.Row }`
  - `func parseFavoriteLimit(raw []byte, level int) (int, error)`
  - `func loadFavoriteLimit(ctx context.Context, q querier, level int) (int, error)`
  - `func favoritesServerError(c *gin.Context, err error)`
  - 測試 helper（`favorites_test.go`）：`newFavoritesUser(t, level int) string`、`newFavoritesRouter(userID string) *gin.Engine`、`favoriteLimitFor(t, level int) int`、`enabledSymbols(t, n int) []string`、`disabledSymbol(t) string`、`addFavoriteRow(t, userID, symbol string, created time.Time)`、`favoriteCount(t, userID string) int`

- [ ] **Step 1: 建立 DDL 檔**

建立 `db/user_favorites.sql`：

```sql
-- 我的最愛個股（TWStockAPI 擁有）；可重複執行
-- 部署前在 .env 指向的 DB 執行：psql "$DATABASE_URL" -f db/user_favorites.sql

-- user 相關設定（通用 key/value；value 為 JSONB）
CREATE TABLE IF NOT EXISTS user_config (
    key          VARCHAR(50) PRIMARY KEY,
    value        JSONB       NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 各 member_level 的我的最愛上限；JSON 的 key 為 member_level 的字串
INSERT INTO user_config (key, value, description) VALUES
  ('FAVORITE_STOCKS_LIMIT', '{"0": 10, "1": 15, "2": 20}', '各 member_level 的我的最愛個股上限')
ON CONFLICT (key) DO NOTHING;

-- user 的我的最愛個股
CREATE TABLE IF NOT EXISTS user_favorite_stocks (
    user_id      UUID        NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    symbol       VARCHAR(10) NOT NULL REFERENCES stocks(symbol) ON DELETE CASCADE,
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, symbol)
);
```

- [ ] **Step 2: 套用 DDL 到 `.env` 的 DB（要先向使用者確認）**

這一步會在共用 DB 建立兩張表並插入設定。**執行前先告訴使用者要套用 `db/user_favorites.sql`，取得同意後再執行**：

```bash
set -a; source .env; set +a; psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites.sql
psql "$DATABASE_URL" -c "SELECT key, value FROM user_config WHERE key = 'FAVORITE_STOCKS_LIMIT'"
```

Expected：`CREATE TABLE`、`INSERT 0 1`（如果已經存在則是 `INSERT 0 0`）、`CREATE TABLE`；查詢結果顯示 `{"0": 10, "1": 15, "2": 20}`。

- [ ] **Step 3: 寫 `parseFavoriteLimit` 的失敗測試**

建立 `routers/favorites_limit_test.go`：

```go
package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFavoriteLimit(t *testing.T) {
	raw := []byte(`{"0": 10, "1": 15, "2": 20}`)

	got, err := parseFavoriteLimit(raw, 1)
	require.NoError(t, err)
	assert.Equal(t, 15, got)

	got, err = parseFavoriteLimit([]byte(`{"0": 0}`), 0)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestParseFavoriteLimitErrors(t *testing.T) {
	cases := map[string]struct {
		raw   string
		level int
	}{
		"level missing": {`{"0": 10, "1": 15, "2": 20}`, 3},
		"invalid json":  {`{`, 0},
		"fractional":    {`{"0": 10.5}`, 0},
		"string value":  {`{"0": "10"}`, 0},
		"negative":      {`{"0": -1}`, 0},
		"null":          {`null`, 0},
		"array":         {`[10]`, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFavoriteLimit([]byte(tc.raw), tc.level)
			assert.Error(t, err)
		})
	}
}
```

- [ ] **Step 4: 寫 GET 和 DELETE 的失敗整合測試**

建立 `routers/favorites_test.go`：

```go
package routers

import (
	"context"
	"errors"
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
```

- [ ] **Step 5: 執行測試，確認失敗**

Run: `go test ./routers/ -run 'Favorite' -v`
Expected：編譯失敗，`undefined: RegisterFavorites`、`undefined: parseFavoriteLimit`、`undefined: loadFavoriteLimit`。

- [ ] **Step 6: 實作 `routers/favorites.go`**

```go
package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// favoriteLimitKey is the user_config key holding the per-member_level limits,
// e.g. {"0": 10, "1": 15, "2": 20}.
const favoriteLimitKey = "FAVORITE_STOCKS_LIMIT"

// RegisterFavorites mounts /favorites behind requireLogin; every handler needs
// the authenticated user.
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.DELETE("/:symbol", deleteFavorite)
}

type favoriteItem struct {
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// listFavorites 回傳 user 的我的最愛（最新加入在前），以及該 member_level 的上限。
func listFavorites(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	ctx := c.Request.Context()

	var level int
	err := db.Pool().QueryRow(ctx,
		"SELECT member_level FROM users WHERE id = $1::uuid", user.ID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: load member_level: %w", err))
		return
	}

	limit, err := loadFavoriteLimit(ctx, db.Pool(), level)
	if err != nil {
		favoritesServerError(c, err)
		return
	}

	rows, err := db.Pool().Query(ctx,
		`SELECT f.symbol, s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid
		ORDER BY f.created_time DESC, f.symbol`, user.ID)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[favoriteItem])
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	if items == nil {
		items = []favoriteItem{}
	}

	c.JSON(http.StatusOK, gin.H{"limit": limit, "count": len(items), "items": items})
}

// deleteFavorite 移除一檔我的最愛；不論原本是否存在都回 204。
func deleteFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}

	_, err := db.Pool().Exec(c.Request.Context(),
		"DELETE FROM user_favorite_stocks WHERE user_id = $1::uuid AND symbol = $2",
		user.ID, c.Param("symbol"))
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: delete: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}

// loadFavoriteLimit 讀取 user_config 的 FAVORITE_STOCKS_LIMIT，回傳 level 對應的上限。
func loadFavoriteLimit(ctx context.Context, q querier, level int) (int, error) {
	var raw string
	err := q.QueryRow(ctx,
		"SELECT value::text FROM user_config WHERE key = $1", favoriteLimitKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("favorites: %s is not set in user_config", favoriteLimitKey)
	}
	if err != nil {
		return 0, fmt.Errorf("favorites: load %s: %w", favoriteLimitKey, err)
	}
	return parseFavoriteLimit([]byte(raw), level)
}

// parseFavoriteLimit 解析 FAVORITE_STOCKS_LIMIT 的 JSONB value，回傳該 member_level 的上限。
// JSON 格式錯誤、找不到該 level、值不是 >= 0 的整數，都回傳 error。
func parseFavoriteLimit(raw []byte, level int) (int, error) {
	var limits map[string]int
	if err := json.Unmarshal(raw, &limits); err != nil {
		return 0, fmt.Errorf("favorites: parse %s: %w", favoriteLimitKey, err)
	}
	limit, ok := limits[strconv.Itoa(level)]
	if !ok {
		return 0, fmt.Errorf("favorites: %s has no limit for member_level %d", favoriteLimitKey, level)
	}
	if limit < 0 {
		return 0, fmt.Errorf("favorites: %s limit for member_level %d is negative: %d", favoriteLimitKey, level, limit)
	}
	return limit, nil
}

// favoritesServerError records err for slog-gin and responds 500.
func favoritesServerError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
```

- [ ] **Step 7: 執行測試，確認通過**

Run: `go test ./routers/ -run 'Favorite' -v`
Expected：全部 PASS。如果 DB 沒有已停用的股票，`TestListFavorites_IncludesDisabledStock` 會 SKIP；如果沒設 `DATABASE_URL`，整個 package 都會 skip，**這時要回報，不能當成通過**。

再執行 `go test ./routers/ -v`，確認既有的 router 測試沒有被影響。

- [ ] **Step 8: Commit**

```bash
git add db/user_favorites.sql routers/favorites.go routers/favorites_limit_test.go routers/favorites_test.go
git commit -m "feat: 新增我的最愛資料表與 GET/DELETE /api/favorites"
```

---

### Task 3: `POST /api/favorites`（transaction 與 FOR UPDATE）

**Files:**
- Modify: `routers/favorites.go`
- Test: `routers/favorites_test.go`

**Interfaces:**
- Consumes：Task 2 的 `favoriteItem`、`loadFavoriteLimit(ctx, q querier, level int) (int, error)`、`favoritesServerError`、所有 `favorites_test.go` helper；Task 1 的 `auth.CurrentUser`。
- Produces：`RegisterFavorites` 新增 `g.POST("", addFavorite)`；`type addFavoriteRequest struct { Symbol string }`；`func addFavorite(c *gin.Context)`；測試 helper `doJSONBody(t, r, method, path, body string) (int, any)`。

- [ ] **Step 1: 寫失敗測試**

在 `routers/favorites_test.go` 的 import 加上 `"encoding/json"`、`"net/http/httptest"`、`"strings"`、`"sync"`，並在檔案最後加上：

```go
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
```

- [ ] **Step 2: 執行測試，確認失敗**

Run: `go test ./routers/ -run 'AddFavorite' -v`
Expected：FAIL。route 還沒註冊，所有 POST 都回 404（gin 預設回 `404 page not found` 純文字，`doJSONBody` 解析 body 失敗，或者狀態碼斷言失敗）。

- [ ] **Step 3: 實作 `addFavorite`**

在 `routers/favorites.go`：

1. import 加上 `"strings"`。
2. `RegisterFavorites` 改成：

```go
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.POST("", addFavorite)
	g.DELETE("/:symbol", deleteFavorite)
}
```

3. 在 `deleteFavorite` 前面加上：

```go
type addFavoriteRequest struct {
	Symbol string `json:"symbol"`
}

// addFavorite 新增一檔我的最愛。整個流程在同一個 transaction 裡，先以 FOR UPDATE
// 鎖住 user 的 row，讓同一個 user 的請求排隊處理，避免同時送出時超過上限。
func addFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}

	var req addFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	ctx := c.Request.Context()
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: begin: %w", err))
		return
	}
	defer tx.Rollback(ctx) // 已 Commit 時為 no-op

	var level int
	err = tx.QueryRow(ctx,
		"SELECT member_level FROM users WHERE id = $1::uuid FOR UPDATE", user.ID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: lock user: %w", err))
		return
	}

	var existing favoriteItem
	err = tx.QueryRow(ctx,
		`SELECT f.symbol, s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid AND f.symbol = $2`,
		user.ID, symbol).Scan(&existing.Symbol, &existing.Name, &existing.CreatedTime)
	if err == nil {
		c.JSON(http.StatusOK, existing)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		favoritesServerError(c, fmt.Errorf("favorites: find existing: %w", err))
		return
	}

	item := favoriteItem{Symbol: symbol}
	err = tx.QueryRow(ctx,
		"SELECT name FROM stocks WHERE symbol = $1 AND enabled", symbol).Scan(&item.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock not found"})
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: find stock: %w", err))
		return
	}

	limit, err := loadFavoriteLimit(ctx, tx, level)
	if err != nil {
		favoritesServerError(c, err)
		return
	}

	var count int
	err = tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1::uuid", user.ID).Scan(&count)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: count: %w", err))
		return
	}
	if count >= limit {
		c.JSON(http.StatusConflict, gin.H{"error": "favorite limit reached", "limit": limit})
		return
	}

	err = tx.QueryRow(ctx,
		"INSERT INTO user_favorite_stocks (user_id, symbol) VALUES ($1::uuid, $2) RETURNING created_time",
		user.ID, symbol).Scan(&item.CreatedTime)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: insert: %w", err))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: commit: %w", err))
		return
	}
	c.JSON(http.StatusCreated, item)
}
```

- [ ] **Step 4: 執行測試，確認通過**

Run: `go test ./routers/ -run 'Favorite' -v -count=1`
Expected：全部 PASS（沒有已停用股票時 `TestAddFavorite_DisabledStock` 會 SKIP）。

再用 race detector 跑一次同時送出的測試：`go test ./routers/ -run 'ConcurrentRequestsRespectLimit' -race -count=3 -v`
Expected：3 次都 PASS，沒有 `DATA RACE`。

- [ ] **Step 5: Commit**

```bash
git add routers/favorites.go routers/favorites_test.go
git commit -m "feat: 新增 POST /api/favorites，以 FOR UPDATE 保證不超過上限"
```

---

### Task 4: 接上 `main.go`、CORS 與文件

**Files:**
- Modify: `main.go`
- Modify: `CLAUDE.md`
- Modify: `README.md`
- Modify: `docs/frontend-api-auth-integration.md`

**Interfaces:**
- Consumes：`routers.RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc)`、`auth.RequireLogin(users auth.UserStore, secret []byte) gin.HandlerFunc`、`auth.PgxUserStore{}`。
- Produces：無（最終接線）。

- [ ] **Step 1: 修改 `main.go`**

CORS：

```go
		AllowMethods: []string{"GET"},
```
改成
```go
		AllowMethods: []string{"GET", "POST", "DELETE"},
```

routes：在 `routers.RegisterPeriod(api)` 後面加上一行：

```go
	routers.RegisterPeriod(api)
	// Favorites always require login, even while JWT_TOKEN_ENABLE is off.
	routers.RegisterFavorites(api, auth.RequireLogin(auth.PgxUserStore{}, jwtSecret))
```

- [ ] **Step 2: Build、vet、跑全部測試**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected：build 與 vet 沒有輸出，所有 package 都是 `ok`。

- [ ] **Step 3: 本機 smoke test（preflight 與不帶 token）**

背景啟動 server：`PORT=18080 go run .`（用 `run_in_background`），等 log 出現 `Server starting` 之後執行：

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X OPTIONS http://localhost:18080/api/favorites \
  -H 'Origin: https://darvishkzone.com' -H 'Access-Control-Request-Method: POST' \
  -H 'Access-Control-Request-Headers: authorization,content-type' -D - | grep -i -E 'access-control-allow-methods|^204'
curl -s -w ' %{http_code}\n' http://localhost:18080/api/favorites
```

Expected：
- preflight 回 `204`，`Access-Control-Allow-Methods` 包含 `POST` 與 `DELETE`。
- 不帶 token 的 GET：`.env` 有 `JWT_SECRET` 時回 `{"error":"unauthorized"} 401`；沒有時回 `{"detail":"Internal server error"} 500`（符合 D1 / fail closed）。

完成後停止 server。

- [ ] **Step 4: 更新 `CLAUDE.md`**

1. Project Overview 第一段：

```
TWStockAPI-Gin is a read-only REST API for Taiwan stock market analysis data, built with Go/Gin. It queries a PostgreSQL database populated by the separate TWStockAnalysis batch processing project. Designed for deployment on Railway.
```
改成
```
TWStockAPI-Gin is a REST API for Taiwan stock market analysis data, built with Go/Gin. It queries a PostgreSQL database populated by the separate TWStockAnalysis batch processing project; the only writes are per-user favorites (`/api/favorites`). Designed for deployment on Railway.
```

2. `main.go` 那一行最後的 `the `/api` group runs `auth.RequireAuth`.` 改成：

```
the `/api` group runs `auth.RequireAuth`; `/api/favorites` additionally runs `auth.RequireLogin`.
```

3. `db/db.go` 那一行下面加上：

```
- **`db/*.sql`** — 需手動在 DB 執行的 DDL / seed（可重複執行）：`config.sql`（`JWT_TOKEN_ENABLE`）、`user_favorites.sql`（`user_config`、`user_favorite_stocks` 與 `FAVORITE_STOCKS_LIMIT` 初始值）。
```

4. `auth/` 那一段的 `- `jwt.go`（`ParseToken`）...` 這一行改成：

```
  - `RequireLogin`：不論開關一律要求有效 JWT（context 已有 user 時直接放行，不重查 DB），目前只掛在 `/api/favorites`。handler 用 `auth.CurrentUser(c)` 取得 user。
  - `jwt.go`（`ParseToken`）、`flag.go`（`FlagCache`、`LoadJWTFlag`）、`user.go`（`PgxUserStore`）、`middleware.go`（`RequireAuth`、`RequireLogin`、`CurrentUser`）。
```

5. `routers/` 清單的 `period.go` 那一行後面加上：

```
  - `favorites.go` — `/api/favorites`（GET / POST / DELETE `/:symbol`）— 使用者的我的最愛個股（`user_favorite_stocks`），一律需要登入。上限依 `users.member_level`，設定在 `user_config` 的 `FAVORITE_STOCKS_LIMIT`（JSONB，例如 `{"0":10,"1":15,"2":20}`；找不到 level 回 500）。GET 回傳 `{limit, count, items[{symbol,name,created_time}]}`（`created_time DESC`）；POST `{"symbol"}`：201 新增、200 已存在、400 格式錯誤、404 股票不存在或未啟用、409 額滿；POST 在 transaction 裡用 `SELECT ... FOR UPDATE` 鎖住 user 的 row；DELETE 一律回 204。
```

6. `All endpoints are read-only SELECT queries. Response format is JSON. Dates are returned as ISO 8601 strings.` 改成：

```
All endpoints except `/api/favorites` are read-only SELECT queries. Response format is JSON. Dates are returned as ISO 8601 strings (`created_time` in favorites is a full RFC 3339 timestamp).
```

- [ ] **Step 5: 更新 `README.md`**

1. 開頭的 `資料來源為 PostgreSQL，由 [TWStockAnalysis](../TWStockAnalysis) 批次作業負責寫入。` 改成：

```
資料來源為 PostgreSQL，由 [TWStockAnalysis](../TWStockAnalysis) 批次作業負責寫入。唯一的例外是「我的最愛」（`/api/favorites`），由本服務寫入 `user_favorite_stocks`。
```

2. 認證段落的 `前端串接方式（如何在 request 帶 token、401 處理、CORS）見 ...` 那一行前面加上：

```
**例外：`/api/favorites/*` 一律需要登入**，不受 `JWT_TOKEN_ENABLE` 影響，驗證失敗的回應同上表。
```

3. 在 `### Trade Records` 的前一個 `---` 之前（也就是 Period 段落的 `找不到資料時回傳空陣列 `[]`。` 後面）插入下面整段：

````markdown
---

### Favorites — 我的最愛

**一律需要登入**：不論 `JWT_TOKEN_ENABLE` 為何，都必須帶 `Authorization: Bearer <token>`；驗證失敗的回應見[認證](#認證)。

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/favorites` | 查詢自己的我的最愛（最新加入的在前） |
| POST | `/api/favorites` | 新增一檔我的最愛 |
| DELETE | `/api/favorites/:symbol` | 移除一檔我的最愛 |

數量上限依 `users.member_level` 而定，設定在 `user_config` 表的 `FAVORITE_STOCKS_LIMIT`（JSONB，key 為 member_level 字串；`is_og_member` 不影響）。修改上限：

```sql
UPDATE user_config SET value = '{"0": 10, "1": 15, "2": 20}', updated_time = now()
WHERE key = 'FAVORITE_STOCKS_LIMIT';
```

第一次部署前執行 `db/user_favorites.sql`，建立 `user_config`、`user_favorite_stocks` 兩張表與上限的初始設定。找不到設定或找不到該 level 的上限時，GET / POST 回 `500`。

#### `GET /api/favorites`

**Response (200):**

```json
{
  "limit": 10,
  "count": 2,
  "items": [
    { "symbol": "2330", "name": "台積電", "created_time": "2026-10-05T09:12:00Z" },
    { "symbol": "2317", "name": "鴻海", "created_time": "2026-10-04T15:00:00Z" }
  ]
}
```

- `items` 依 `created_time` 由新到舊排序；沒有資料時為 `[]`。
- 加入後被停用（`stocks.enabled = false`）的股票仍會列出。

#### `POST /api/favorites`

**Request Body:**

```json
{ "symbol": "2330" }
```

`symbol` 會先去掉前後空白。只能加入 `stocks.enabled = true` 的股票。

| HTTP | Body | 意義 |
|------|------|------|
| `201` | `{"symbol":"2330","name":"台積電","created_time":"..."}` | 新增成功 |
| `200` | 同上（既有的那筆） | 本來就已加入（即使已額滿） |
| `400` | `{"error":"invalid request"}` | body 不是合法的 JSON，或 `symbol` 是空的 |
| `404` | `{"error":"stock not found"}` | 股票不存在或未啟用 |
| `409` | `{"error":"favorite limit reached","limit":10}` | 已達上限 |

#### `DELETE /api/favorites/:symbol`

移除一檔我的最愛。不論原本是否存在，一律回 `204`（沒有 body）。
````

- [ ] **Step 6: 更新 `docs/frontend-api-auth-integration.md`**

1. §6 CORS 表格的 `| Methods | `GET` |` 改成 `| Methods | `GET`、`POST`、`DELETE` |`。
2. §9 後端開關那一段最後加上：

```
**例外：`/api/favorites/*`（我的最愛）一律需要 token**，即使開關關閉也一樣；沒帶或 token 無效時回 `401`，處理方式同 §5。API 規格見 TWStockAPI `README.md` 的 Favorites 段落。
```

- [ ] **Step 7: Commit**

```bash
git add main.go CLAUDE.md README.md docs/frontend-api-auth-integration.md
git commit -m "feat: 掛上 /api/favorites 並開放 POST/DELETE CORS，同步更新文件"
```
