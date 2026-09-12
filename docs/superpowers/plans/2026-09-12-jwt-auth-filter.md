# JWT 驗證 Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `/api/*` 前加上 JWT 驗證 middleware，由 DB `config.JWT_TOKEN_ENABLE` 開關控制（預設關閉）。

**Architecture:** 新增 `auth/` package：`jwt.go` 驗 AccountService 簽發的 HS256 token；`flag.go` 以 60 秒 TTL 快取讀取 config 開關；`user.go` 以 `sub`（`users.id`）查 `users` 表；`middleware.go` 串起整個流程，並透過 `FlagSource` / `UserStore` interface 讓測試可以換成 fake。`main.go` 把 middleware 掛在 `/api` group，並在 CORS 允許 `Authorization`。

**Tech Stack:** Go 1.25、Gin、pgx/v5、`github.com/golang-jwt/jwt/v5 v5.3.1`、`github.com/google/uuid v1.6.0`、testify。

**Spec:** `docs/superpowers/specs/2026-09-12-jwt-auth-filter-design.md`

## Global Constraints

- Go module 名稱是 `main`（import path 例如 `main/db`、`main/auth`）。
- `github.com/golang-jwt/jwt/v5` 固定用 `v5.3.1`（與 AccountService 相同）。
- Config key 固定為 `JWT_TOKEN_ENABLE`；值 trim 後不分大小寫等於 `true` 才算開啟；沒有這一列視為關閉。
- Flag 快取 TTL 為 `time.Minute`。
- 回應 body 必須完全相同：過期 401 `{"error":"token expired"}`；其他驗證失敗 401 `{"error":"unauthorized"}`；DB 錯誤或未設 secret 500 `{"detail":"Internal server error"}`。
- Middleware 只掛在 `/api` group；`/health`、`/health/db` 不驗證。
- Flag 關閉時完全不讀 `Authorization`。
- Log 不可包含 token 內容。
- 不可把共用 DB 的 `JWT_TOKEN_ENABLE` 改成 `true`。
- 測試風格：testify `assert` / `require`、`httptest`、`gin.TestMode`。
- Commit message 不加 `Co-Authored-By:` 或任何署名行。

## File Structure

| File | 動作 | 職責 |
|---|---|---|
| `auth/jwt.go` | Create | `ParseToken`、`ErrTokenExpired`、`ErrInvalidToken` |
| `auth/jwt_test.go` | Create | `ParseToken` 單元測試；共用測試 helper `testSecret` / `signToken` / `validClaims` |
| `auth/flag.go` | Create | `FlagKey`、`FlagSource`、`FlagLoader`、`FlagCache`、`LoadJWTFlag`、`loadConfigValue` |
| `auth/flag_test.go` | Create | `FlagCache` 單元測試 |
| `auth/user.go` | Create | `User`、`ErrUserNotFound`、`UserStore`、`PgxUserStore` |
| `auth/store_test.go` | Create | `TestMain`、`requireDB`、DB 整合測試 |
| `auth/middleware.go` | Create | `RequireAuth`、`ContextUserKey` |
| `auth/middleware_test.go` | Create | Middleware 單元測試（fake flag / user store） |
| `main.go` | Modify | 讀 `JWT_SECRET`、CORS 加 `Authorization`、`/api` 掛 middleware |
| `db/config.sql` | Create | 插入 `JWT_TOKEN_ENABLE=false` |
| `.env.example` | Modify | 加 `JWT_SECRET` |
| `README.md` | Modify | 設定、認證、錯誤回應 |
| `CLAUDE.md` | Modify | Build & Run、Architecture |
| `go.mod` / `go.sum` | Modify | 加 jwt；uuid 改 direct |

---

### Task 1: JWT 解析（`auth/jwt.go`）

**Files:**
- Create: `auth/jwt.go`
- Test: `auth/jwt_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: 無
- Produces:
  - `var ErrTokenExpired error`、`var ErrInvalidToken error`
  - `func ParseToken(secret []byte, tokenStr string) (string, error)` — 回傳 `sub`
  - 測試 helper（`package auth`，Task 4 會用到）：`var testSecret []byte`、`func signToken(t *testing.T, method jwt.SigningMethod, secret []byte, claims jwt.RegisteredClaims) string`、`func validClaims(sub string, ttl time.Duration) jwt.RegisteredClaims`

- [ ] **Step 1: 加入 jwt 套件**

Run: `go get github.com/golang-jwt/jwt/v5@v5.3.1`
Expected: `go.mod` 出現 `github.com/golang-jwt/jwt/v5 v5.3.1`。

- [ ] **Step 2: 寫失敗的測試 `auth/jwt_test.go`**

```go
package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret")

// signToken signs claims the same way AccountService does.
func signToken(t *testing.T, method jwt.SigningMethod, secret []byte, claims jwt.RegisteredClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	require.NoError(t, err)
	return s
}

// validClaims mirrors AccountService's claims: sub, iat, exp = now + ttl.
func validClaims(sub string, ttl time.Duration) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   sub,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
}

func TestParseTokenReturnsSubject(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("user-123", time.Hour))

	sub, err := ParseToken(testSecret, token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", sub)
}

func TestParseTokenExpired(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("user-123", -time.Hour))

	_, err := ParseToken(testSecret, token)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

// An expired token signed with another key must not be reported as expired.
func TestParseTokenExpiredWithWrongSecretIsInvalid(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims("user-123", -time.Hour))

	_, err := ParseToken(testSecret, token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseTokenRejectsInvalid(t *testing.T) {
	noExp := jwt.RegisteredClaims{Subject: "user-123", IssuedAt: jwt.NewNumericDate(time.Now())}

	cases := map[string]string{
		"wrong secret": signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims("user-123", time.Hour)),
		"HS384":        signToken(t, jwt.SigningMethodHS384, testSecret, validClaims("user-123", time.Hour)),
		"missing exp":  signToken(t, jwt.SigningMethodHS256, testSecret, noExp),
		"missing sub":  signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("", time.Hour)),
		"garbage":      "not-a-jwt",
		"empty":        "",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseToken(testSecret, token)
			assert.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}

// golang-jwt accepts an empty HMAC key, so ParseToken must refuse it itself.
func TestParseTokenRejectsEmptySecret(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, []byte{}, validClaims("user-123", time.Hour))

	_, err := ParseToken(nil, token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
```

- [ ] **Step 3: 確認測試失敗**

Run: `go test ./auth/ -run TestParseToken -v`
Expected: 編譯失敗，`undefined: ParseToken`（以及 `ErrTokenExpired`、`ErrInvalidToken`）。

- [ ] **Step 4: 實作 `auth/jwt.go`**

```go
package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrTokenExpired means the token is correctly signed but past its exp.
	ErrTokenExpired = errors.New("token expired")
	// ErrInvalidToken covers every other verification failure.
	ErrInvalidToken = errors.New("invalid token")
)

// ParseToken validates an HS256 JWT issued by AccountService and returns its
// subject (users.id). golang-jwt verifies the signature before the claims, so
// ErrTokenExpired is only returned for tokens signed with our secret.
func ParseToken(secret []byte, tokenStr string) (string, error) {
	// golang-jwt accepts an empty HMAC key; refuse it so a missing secret can
	// never validate a token signed with an empty key.
	if len(secret) == 0 {
		return "", ErrInvalidToken
	}

	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if errors.Is(err, jwt.ErrTokenExpired) {
		return "", ErrTokenExpired
	}
	if err != nil || claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
```

- [ ] **Step 5: 整理 go.mod 並確認測試通過**

Run: `go mod tidy && gofmt -l auth && go test ./auth/ -run TestParseToken -v`
Expected: `gofmt -l` 沒有輸出；全部 PASS；`git diff go.mod` 顯示 jwt 在第一個 require block（非 `// indirect`）。

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum auth/jwt.go auth/jwt_test.go
git commit -m "feat: 新增 auth.ParseToken 驗證 AccountService JWT"
```

---

### Task 2: Flag 快取（`auth/flag.go`）

**Files:**
- Create: `auth/flag.go`
- Test: `auth/flag_test.go`

**Interfaces:**
- Consumes: 無
- Produces:
  - `const FlagKey = "JWT_TOKEN_ENABLE"`
  - `type FlagSource interface { Enabled(ctx context.Context) (bool, error) }`
  - `type FlagLoader func(ctx context.Context) (value string, found bool, err error)`
  - `type FlagCache struct`（未匯出欄位 `load`、`ttl`、`now func() time.Time`、`mu`、`value`、`expiresAt`）
  - `func NewFlagCache(load FlagLoader, ttl time.Duration) *FlagCache`
  - `func (f *FlagCache) Enabled(ctx context.Context) (bool, error)`

- [ ] **Step 1: 寫失敗的測試 `auth/flag_test.go`**

```go
package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoader records calls and returns whatever its fields hold.
type fakeLoader struct {
	value string
	found bool
	err   error
	calls int
}

func (l *fakeLoader) load(context.Context) (string, bool, error) {
	l.calls++
	return l.value, l.found, l.err
}

// newTestFlagCache returns a cache whose clock reads *now.
func newTestFlagCache(l *fakeLoader, now *time.Time) *FlagCache {
	f := NewFlagCache(l.load, time.Minute)
	f.now = func() time.Time { return *now }
	return f
}

var t0 = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)

func TestFlagCacheUsesCachedValueWithinTTL(t *testing.T) {
	l := &fakeLoader{value: "true", found: true}
	now := t0
	f := newTestFlagCache(l, &now)

	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on)

	l.value = "false"
	now = t0.Add(59 * time.Second)
	on, err = f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on, "value should still come from the cache")
	assert.Equal(t, 1, l.calls)
}

func TestFlagCacheReloadsAfterTTL(t *testing.T) {
	l := &fakeLoader{value: "true", found: true}
	now := t0
	f := newTestFlagCache(l, &now)

	_, err := f.Enabled(context.Background())
	require.NoError(t, err)

	l.value = "false"
	now = t0.Add(time.Minute)
	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.False(t, on)
	assert.Equal(t, 2, l.calls)
}

func TestFlagCacheDoesNotCacheErrors(t *testing.T) {
	l := &fakeLoader{err: errors.New("db down")}
	now := t0
	f := newTestFlagCache(l, &now)

	_, err := f.Enabled(context.Background())
	require.Error(t, err)

	l.err = nil
	l.value, l.found = "true", true
	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on)
	assert.Equal(t, 2, l.calls)
}

func TestFlagCacheValueParsing(t *testing.T) {
	cases := []struct {
		name  string
		value string
		found bool
		want  bool
	}{
		{"missing key", "", false, false},
		{"true", "true", true, true},
		{"upper case", "TRUE", true, true},
		{"padded", " true ", true, true},
		{"false", "false", true, false},
		{"yes", "yes", true, false},
		{"empty", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeLoader{value: tc.value, found: tc.found}
			now := t0
			on, err := newTestFlagCache(l, &now).Enabled(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, on)
		})
	}
}
```

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./auth/ -run TestFlagCache -v`
Expected: 編譯失敗，`undefined: NewFlagCache`、`undefined: FlagCache`。

- [ ] **Step 3: 實作 `auth/flag.go`**

```go
package auth

import (
	"context"
	"strings"
	"sync"
	"time"
)

// FlagKey is the config-table key that turns JWT verification on.
const FlagKey = "JWT_TOKEN_ENABLE"

// FlagSource reports whether JWT verification is enabled.
type FlagSource interface {
	Enabled(ctx context.Context) (bool, error)
}

// FlagLoader returns the raw config value; found is false when the key is absent.
type FlagLoader func(ctx context.Context) (value string, found bool, err error)

// FlagCache is a FlagSource that caches the loaded flag for ttl.
// Load errors are returned to the caller and never cached.
type FlagCache struct {
	load FlagLoader
	ttl  time.Duration
	now  func() time.Time

	mu        sync.Mutex
	value     bool
	expiresAt time.Time
}

func NewFlagCache(load FlagLoader, ttl time.Duration) *FlagCache {
	return &FlagCache{load: load, ttl: ttl, now: time.Now}
}

// Enabled returns the cached flag, reloading it once the TTL has passed.
// The lock is held across the load so only one request queries the DB.
func (f *FlagCache) Enabled(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.now().Before(f.expiresAt) {
		return f.value, nil
	}

	v, found, err := f.load(ctx)
	if err != nil {
		return false, err
	}
	f.value = found && strings.EqualFold(strings.TrimSpace(v), "true")
	f.expiresAt = f.now().Add(f.ttl)
	return f.value, nil
}
```

- [ ] **Step 4: 確認測試通過**

Run: `gofmt -l auth && go test ./auth/ -v`
Expected: `gofmt -l` 沒有輸出；`TestParseToken*`、`TestFlagCache*` 全部 PASS。

- [ ] **Step 5: Commit**

```bash
git add auth/flag.go auth/flag_test.go
git commit -m "feat: 新增 auth.FlagCache 快取 JWT_TOKEN_ENABLE 開關"
```

---

### Task 3: DB 存取（`auth/user.go`、`LoadJWTFlag`）

**Files:**
- Create: `auth/user.go`
- Modify: `auth/flag.go`（加 `LoadJWTFlag`、`loadConfigValue`）
- Test: `auth/store_test.go`

**Interfaces:**
- Consumes: `FlagKey`（Task 2）、`db.Pool() *pgxpool.Pool`、`db.InitPool() error`、`db.ClosePool()`（`main/db`）
- Produces:
  - `type User struct { ID string; Email string }`
  - `var ErrUserNotFound error`
  - `type UserStore interface { GetByID(ctx context.Context, id string) (User, error) }`
  - `type PgxUserStore struct{}`，`func (PgxUserStore) GetByID(ctx context.Context, id string) (User, error)`
  - `func LoadJWTFlag(ctx context.Context) (string, bool, error)`（型別符合 `FlagLoader`）
  - `func loadConfigValue(ctx context.Context, key string) (string, bool, error)`

- [ ] **Step 1: 寫失敗的測試 `auth/store_test.go`**

```go
package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"main/db"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain opens the DB pool only when DATABASE_URL is set. Unit tests in
// this package always run; DB tests skip themselves via requireDB.
func TestMain(m *testing.M) {
	_ = godotenv.Load("../.env")
	if os.Getenv("DATABASE_URL") != "" {
		if err := db.InitPool(); err != nil {
			fmt.Fprintln(os.Stderr, "db init failed:", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	db.ClosePool()
	os.Exit(code)
}

func requireDB(t *testing.T) {
	t.Helper()
	if db.Pool() == nil {
		t.Skip("DATABASE_URL not set, skipping DB test")
	}
}

func TestPgxUserStoreGetByID(t *testing.T) {
	requireDB(t)
	ctx := context.Background()

	var want User
	err := db.Pool().QueryRow(ctx, "SELECT id::text, email FROM users LIMIT 1").Scan(&want.ID, &want.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no rows in users")
	}
	require.NoError(t, err)

	got, err := PgxUserStore{}.GetByID(ctx, want.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPgxUserStoreGetByIDNotFound(t *testing.T) {
	requireDB(t)

	_, err := PgxUserStore{}.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestLoadConfigValueMissingKey(t *testing.T) {
	requireDB(t)

	v, found, err := loadConfigValue(context.Background(), "__no_such_key__")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, v)
}

func TestLoadJWTFlag(t *testing.T) {
	requireDB(t)

	_, _, err := LoadJWTFlag(context.Background())
	assert.NoError(t, err)
}
```

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./auth/ -run 'TestPgxUserStore|TestLoadConfigValue|TestLoadJWTFlag' -v`
Expected: 編譯失敗，`undefined: User`、`PgxUserStore`、`ErrUserNotFound`、`loadConfigValue`、`LoadJWTFlag`。

- [ ] **Step 3: 實作 `auth/user.go`**

```go
package auth

import (
	"context"
	"errors"

	"main/db"

	"github.com/jackc/pgx/v5"
)

// User is the authenticated caller, resolved from the JWT subject.
type User struct {
	ID    string
	Email string
}

// ErrUserNotFound means the JWT subject has no row in the users table.
var ErrUserNotFound = errors.New("user not found")

// UserStore looks up users by id.
type UserStore interface {
	GetByID(ctx context.Context, id string) (User, error)
}

// PgxUserStore is a UserStore backed by the shared pgx pool.
// The users table is owned by AccountService.
type PgxUserStore struct{}

// GetByID fetches a user by UUID.
func (PgxUserStore) GetByID(ctx context.Context, id string) (User, error) {
	var u User
	err := db.Pool().QueryRow(ctx,
		"SELECT id::text, email FROM users WHERE id = $1::uuid", id).Scan(&u.ID, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}
```

- [ ] **Step 4: 在 `auth/flag.go` 加上 DB loader**

把 import 改為：

```go
import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"main/db"

	"github.com/jackc/pgx/v5"
)
```

並在檔案結尾加上：

```go
// LoadJWTFlag reads JWT_TOKEN_ENABLE from the config table (owned by TWStockAnalysis).
func LoadJWTFlag(ctx context.Context) (string, bool, error) {
	return loadConfigValue(ctx, FlagKey)
}

func loadConfigValue(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := db.Pool().QueryRow(ctx, "SELECT value FROM config WHERE key = $1", key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}
```

- [ ] **Step 5: 確認測試通過（會連 `.env` 的 DB，只做 SELECT）**

Run: `gofmt -l auth && go test ./auth/ -v`
Expected: `gofmt -l` 沒有輸出；全部 PASS（若 `.env` 沒有 `DATABASE_URL`，DB 測試顯示 SKIP，其餘 PASS）。

- [ ] **Step 6: Commit**

```bash
git add auth/user.go auth/flag.go auth/store_test.go
git commit -m "feat: 新增 auth 的 users 查詢與 config 開關讀取"
```

---

### Task 4: 驗證 Middleware（`auth/middleware.go`）

**Files:**
- Create: `auth/middleware.go`
- Test: `auth/middleware_test.go`
- Modify: `go.mod`（`github.com/google/uuid` 改為 direct）

**Interfaces:**
- Consumes:
  - `ParseToken`、`ErrTokenExpired`（Task 1）；測試 helper `testSecret`、`signToken`、`validClaims`（`auth/jwt_test.go`）
  - `FlagSource`、`FlagKey`（Task 2）
  - `User`、`UserStore`、`ErrUserNotFound`（Task 3）
- Produces:
  - `const ContextUserKey = "authUser"` — context 裡存的是 `User` 值
  - `func RequireAuth(flags FlagSource, users UserStore, secret []byte) gin.HandlerFunc`

- [ ] **Step 1: 寫失敗的測試 `auth/middleware_test.go`**

```go
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUserID = "6f1c1f5e-7a5e-4d7b-9a3b-2f0e5c1d8a11"

type fakeFlags struct {
	enabled bool
	err     error
}

func (f fakeFlags) Enabled(context.Context) (bool, error) { return f.enabled, f.err }

type fakeUsers struct {
	users map[string]User
	err   error
	calls int
}

func (s *fakeUsers) GetByID(_ context.Context, id string) (User, error) {
	s.calls++
	if s.err != nil {
		return User{}, s.err
	}
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func knownUsers() *fakeUsers {
	return &fakeUsers{users: map[string]User{
		testUserID: {ID: testUserID, Email: "user@example.com"},
	}}
}

// serve sends one GET /api/ping through RequireAuth. The handler echoes the
// authenticated user's email so tests can see what reached the context.
func serve(t *testing.T, flags FlagSource, users UserStore, secret []byte, authHeader string) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", RequireAuth(flags, users, secret))
	api.GET("/ping", func(c *gin.Context) {
		body := gin.H{"ok": true}
		if u, exists := c.Get(ContextUserKey); exists {
			body["email"] = u.(User).Email
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

func bearer(t *testing.T, sub string, ttl time.Duration) string {
	t.Helper()
	return "Bearer " + signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(sub, ttl))
}

func TestRequireAuthDisabledPassesThrough(t *testing.T) {
	cases := map[string]string{
		"no token":      "",
		"garbage token": "Bearer garbage",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			users := knownUsers()
			code, body := serve(t, fakeFlags{enabled: false}, users, testSecret, header)
			assert.Equal(t, http.StatusOK, code)
			assert.NotContains(t, body, "email")
			assert.Zero(t, users.calls)
		})
	}
}

func TestRequireAuthValidTokenSetsUser(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(testUserID, time.Hour))
	cases := map[string]string{
		"Bearer": "Bearer " + token,
		"bearer": "bearer " + token,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, header)
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, "user@example.com", body["email"])
		})
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, bearer(t, testUserID, -time.Hour))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "token expired"}, body)
}

func TestRequireAuthUnauthorized(t *testing.T) {
	cases := map[string]string{
		"missing header": "",
		"wrong scheme":   "Token " + signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(testUserID, time.Hour)),
		"empty bearer":   "Bearer   ",
		"bad signature":  "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims(testUserID, time.Hour)),
		"unknown user":   bearer(t, "00000000-0000-0000-0000-000000000000", time.Hour),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, header)
			assert.Equal(t, http.StatusUnauthorized, code)
			assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
		})
	}
}

func TestRequireAuthNonUUIDSubjectSkipsLookup(t *testing.T) {
	users := knownUsers()
	code, body := serve(t, fakeFlags{enabled: true}, users, testSecret, bearer(t, "not-a-uuid", time.Hour))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
	assert.Zero(t, users.calls)
}

func TestRequireAuthServerErrors(t *testing.T) {
	serverError := map[string]any{"detail": "Internal server error"}

	t.Run("flag load fails", func(t *testing.T) {
		code, body := serve(t, fakeFlags{err: errors.New("db down")}, knownUsers(), testSecret, "")
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("secret not set", func(t *testing.T) {
		code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), nil, bearer(t, testUserID, time.Hour))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		users := &fakeUsers{err: errors.New("db down")}
		code, body := serve(t, fakeFlags{enabled: true}, users, testSecret, bearer(t, testUserID, time.Hour))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})
}
```

- [ ] **Step 2: 確認測試失敗**

Run: `go test ./auth/ -run TestRequireAuth -v`
Expected: 編譯失敗，`undefined: RequireAuth`、`undefined: ContextUserKey`。

- [ ] **Step 3: 實作 `auth/middleware.go`**

```go
package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ContextUserKey is the gin.Context key holding the authenticated User.
const ContextUserKey = "authUser"

// RequireAuth returns middleware that, while the JWT_TOKEN_ENABLE flag is on,
// requires a valid AccountService Bearer JWT whose subject exists in users.
// While the flag is off, requests pass through without reading Authorization.
func RequireAuth(flags FlagSource, users UserStore, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		enabled, err := flags.Enabled(ctx)
		if err != nil {
			abortServerError(c, fmt.Errorf("auth: load %s: %w", FlagKey, err))
			return
		}
		if !enabled {
			c.Next()
			return
		}

		if len(secret) == 0 {
			abortServerError(c, errors.New("auth: JWT_SECRET is not set"))
			return
		}

		tokenStr, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			abortUnauthorized(c, errors.New("auth: missing bearer token"))
			return
		}

		sub, err := ParseToken(secret, tokenStr)
		if errors.Is(err, ErrTokenExpired) {
			_ = c.Error(fmt.Errorf("auth: %w", err))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
			return
		}
		if err != nil {
			abortUnauthorized(c, fmt.Errorf("auth: %w", err))
			return
		}

		id, err := uuid.Parse(sub)
		if err != nil {
			abortUnauthorized(c, errors.New("auth: subject is not a uuid"))
			return
		}

		user, err := users.GetByID(ctx, id.String())
		if errors.Is(err, ErrUserNotFound) {
			abortUnauthorized(c, fmt.Errorf("auth: %w", err))
			return
		}
		if err != nil {
			abortServerError(c, fmt.Errorf("auth: lookup user: %w", err))
			return
		}

		c.Set(ContextUserKey, user)
		c.Next()
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) (string, bool) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

// abortUnauthorized records the reason for slog-gin and responds 401.
func abortUnauthorized(c *gin.Context, reason error) {
	_ = c.Error(reason)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// abortServerError records the reason for slog-gin and responds 500.
func abortServerError(c *gin.Context, reason error) {
	_ = c.Error(reason)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
```

- [ ] **Step 4: 整理 go.mod 並確認測試通過**

Run: `go mod tidy && gofmt -l auth && go vet ./auth/ && go test ./auth/ -v`
Expected: `gofmt -l` 沒有輸出；vet 沒有問題；全部 PASS；`git diff go.mod` 顯示 `github.com/google/uuid v1.6.0` 移到第一個 require block。

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum auth/middleware.go auth/middleware_test.go
git commit -m "feat: 新增 auth.RequireAuth JWT 驗證 middleware"
```

---

### Task 5: 接上 main.go、config 列、文件

**Files:**
- Modify: `main.go:3-17`（imports）、`main.go:22-28`（讀 secret）、`main.go:35-39`（CORS）、`main.go:56-57`（`/api` group）
- Create: `db/config.sql`
- Modify: `.env.example`、`README.md`、`CLAUDE.md`

**Interfaces:**
- Consumes: `auth.NewFlagCache`、`auth.LoadJWTFlag`（Task 2、3）、`auth.PgxUserStore`（Task 3）、`auth.RequireAuth`（Task 4）
- Produces: 無（最後一個 task）

- [ ] **Step 1: 修改 `main.go`**

Import block 改為：

```go
import (
	"cmp"
	"log/slog"
	"net/http"
	"os"
	"time"

	"main/auth"
	"main/db"
	"main/routers"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	sloggin "github.com/samber/slog-gin"
)
```

在 `defer db.ClosePool()` 之後加上：

```go
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		logger.Warn("JWT_SECRET is not set; /api requests will fail with 500 if JWT_TOKEN_ENABLE is turned on")
	}
```

CORS 的 `AllowHeaders` 改為：

```go
		AllowHeaders: []string{"Origin", "Content-Type", "Authorization"},
```

`// API routes` 區塊的前兩行改為：

```go
	// API routes — JWT verification is toggled by config.JWT_TOKEN_ENABLE
	flags := auth.NewFlagCache(auth.LoadJWTFlag, time.Minute)
	api := r.Group("/api", auth.RequireAuth(flags, auth.PgxUserStore{}, jwtSecret))
```

- [ ] **Step 2: 建立 `db/config.sql`**

```sql
-- JWT 驗證開關（config 表由 TWStockAnalysis 擁有）
-- value 為 'true'（不分大小寫）才啟用；其他值或沒有這一列都視為關閉
INSERT INTO config (key, value) VALUES ('JWT_TOKEN_ENABLE', 'false')
ON CONFLICT (key) DO NOTHING;
```

- [ ] **Step 3: 在 `.env` 指向的 DB 執行 `db/config.sql`**

Run:

```bash
set -a; . ./.env; set +a
psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 -f db/config.sql
psql "$DATABASE_URL" -X -c "SELECT key, value FROM config WHERE key = 'JWT_TOKEN_ENABLE';"
```

Expected: `INSERT 0 1`（已存在時是 `INSERT 0 0`）；查詢結果為 `JWT_TOKEN_ENABLE | false`。

- [ ] **Step 4: 更新 `.env.example`**

完整內容：

```
# PostgreSQL 連線（與 TWStockAnalysis 共用同一個 DB）
DATABASE_URL=postgresql://twstock:twstock@localhost:5432/twstock

# JWT 驗證密鑰：必須與 AccountService 的 JWT_SECRET 相同
# 只有 config 表 JWT_TOKEN_ENABLE = 'true' 時才會用到
JWT_SECRET=change-me-to-the-account-service-secret
```

- [ ] **Step 5: 更新 `README.md`**

(a) 「## 設定」段落中，`確保 DATABASE_URL 指向與 TWStockAnalysis 相同的 PostgreSQL。` 之後加上：

```markdown
| 環境變數 | 必填 | 說明 |
|----------|------|------|
| `DATABASE_URL` | 是 | PostgreSQL 連線字串 |
| `JWT_SECRET` | 啟用 JWT 驗證時必填 | 驗證 JWT 的密鑰，必須與 AccountService 的 `JWT_SECRET` 相同 |
| `PORT` | 否 | 服務 port，預設 `8080` |
```

(b) 在「## 啟動」段落之後、`---` 與「## API Endpoints」之前插入：

````markdown
## 認證

`/api/*` 前面有 JWT 驗證 middleware，是否啟用由 DB `config` 表的 `JWT_TOKEN_ENABLE` 決定。`/health`、`/health/db` 永遠不驗證。

| `JWT_TOKEN_ENABLE` | 行為 |
|--------------------|------|
| `false`（預設；沒有這一列也視為 `false`） | 不驗證，不帶 token 也能存取 |
| `true`（不分大小寫） | 必須帶 AccountService 簽發的 JWT |

啟用時，每個 request 需帶：

```
Authorization: Bearer <AccountService 回傳的 token>
```

Token 的 `sub`（使用者 id）必須存在於 `users` 表。

**驗證失敗回應：**

| HTTP | Body | 意義 |
|------|------|------|
| `401` | `{"error":"token expired"}` | Token 已過期，請重新登入 |
| `401` | `{"error":"unauthorized"}` | 沒帶 token、格式錯誤、簽章無效，或使用者不存在 |
| `500` | `{"detail":"Internal server error"}` | 讀取設定或使用者時 DB 出錯，或伺服器沒有設定 `JWT_SECRET` |

**切換開關**（60 秒內生效，不需重新部署）：

```sql
UPDATE config SET value = 'true', updated_time = now() WHERE key = 'JWT_TOKEN_ENABLE';
```

第一次設定時執行 `db/config.sql` 建立這一列（預設 `false`）。

---
````

(c) 「## 錯誤回應」段落結尾（`HTTP Status Code: 500` 之後）加上：

```markdown

啟用 JWT 驗證時，驗證失敗回傳 `401`，詳見[認證](#認證)。
```

- [ ] **Step 6: 更新 `CLAUDE.md`**

(a) 「## Build & Run」中，`Requires DATABASE_URL env var ...` 那一行改為：

```markdown
Requires `DATABASE_URL` env var (PostgreSQL connection string). Server listens on `PORT` (default `8080`). `JWT_SECRET`（必須與 AccountService 相同）只有在啟用 JWT 驗證時才需要；沒設時啟動只會記 warning。
```

(b) 「## Architecture」中，`main.go` 那一項改為：

```markdown
- **`main.go`** — App entry point: loads `.env`, inits DB pool, registers middleware (`slog-gin`, `gin.Recovery`, CORS allowing `Authorization`), mounts health checks and API route groups; the `/api` group runs `auth.RequireAuth`.
```

(c) 在 `db/db.go` 那一項之後加上：

```markdown
- **`auth/`** — JWT 驗證 filter，掛在 `/api` group（`/health*` 不驗證）。開關為 DB `config` 表的 `JWT_TOKEN_ENABLE`（值為 `'true'` 才啟用；沒有這一列視為 false；60 秒記憶體快取）。`db/config.sql` 建立這一列（預設 `false`）。
  - 啟用時需帶 `Authorization: Bearer <jwt>`（AccountService 簽發，HS256，`JWT_SECRET` 須相同）。`sub` = `users.id`，查 `users` 表取得 email，以 `auth.User{ID, Email}` 存入 `gin.Context`（key `auth.ContextUserKey`）。
  - 過期回 401 `{"error":"token expired"}`；其他驗證失敗回 401 `{"error":"unauthorized"}`；讀 config / users 出錯或未設 `JWT_SECRET` 回 500。
  - `jwt.go`（`ParseToken`）、`flag.go`（`FlagCache`、`LoadJWTFlag`）、`user.go`（`PgxUserStore`）、`middleware.go`（`RequireAuth`）。
```

- [ ] **Step 7: 完整驗證**

Run: `gofmt -l . && go vet ./... && go build -o /dev/null . && go test ./...`
Expected: `gofmt -l` 沒有輸出；vet、build 成功；`auth` 與 `routers` 全部 `ok`。

- [ ] **Step 8: 本機實測（flag = false）**

Run（在 repo 根目錄；server 會讀 `.env`）：

```bash
SMOKE_DIR=$(mktemp -d)
go build -o "$SMOKE_DIR/server" .
PORT=18080 "$SMOKE_DIR/server" > "$SMOKE_DIR/server.log" 2>&1 &
PID=$!
curl -s --retry 20 --retry-connrefused --retry-delay 1 -o /dev/null -w 'health %{http_code}\n' localhost:18080/health
curl -s -o /dev/null -w 'no token %{http_code}\n' localhost:18080/api/stocks
curl -s -o /dev/null -w 'garbage token %{http_code}\n' -H 'Authorization: Bearer garbage' localhost:18080/api/stocks
curl -s -i -X OPTIONS localhost:18080/api/stocks \
  -H 'Origin: https://darvishkzone.com' \
  -H 'Access-Control-Request-Method: GET' \
  -H 'Access-Control-Request-Headers: authorization' | grep -iE '^HTTP|^access-control-allow-headers'
kill $PID
grep -c JWT_SECRET "$SMOKE_DIR/server.log"
```

Expected：
- `health 200`、`no token 200`、`garbage token 200`
- preflight 回 `HTTP/1.1 204`，且 `Access-Control-Allow-Headers` 包含 `Authorization`
- 如果 `.env` 沒有 `JWT_SECRET`，log 中有 1 筆 warning

- [ ] **Step 9: Commit**

```bash
git add main.go db/config.sql .env.example README.md CLAUDE.md
git commit -m "feat: /api 掛上 JWT 驗證 filter，由 config.JWT_TOKEN_ENABLE 控制"
```
