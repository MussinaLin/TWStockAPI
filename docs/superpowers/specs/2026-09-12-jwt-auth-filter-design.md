# JWT 驗證 Filter — Design

**Date:** 2026-09-12
**Status:** Approved
**Scope:** 在 TWStockAPI 所有 `/api/*` 路由前加上 JWT 驗證 middleware，驗證 AccountService 簽發的 token；由 DB `config` 表的 `JWT_TOKEN_ENABLE` 開關控制是否啟用（預設關閉）。

## Goal

- `JWT_TOKEN_ENABLE=true` 時，`/api/*` 只有帶有效 JWT 的 request 才拿得到 response。
- 從 token 解出使用者（`sub` = `users.id`），對應到 `users` 表取得 email。
- Token 過期回 401 `{"error":"token expired"}`。
- `JWT_TOKEN_ENABLE=false`（預設）時行為與現在完全相同，不帶 token 也可以。

## Background（現況）

- AccountService 簽發的 JWT：HS256、密鑰 `JWT_SECRET`、claims 只有 `sub`（`users.id` UUID）、`iat`、`exp`（預設 168 小時）。**Token 裡沒有 email**，email 要用 `sub` 查 `users` 表。
- TWStockAPI 與 AccountService 共用同一個 PostgreSQL。`users`（AccountService 擁有）與 `config`（TWStockAnalysis 擁有；`key VARCHAR(50) PK`、`value TEXT NOT NULL`、`created_time`、`updated_time`）都在同一個 DB。
- golang-jwt v5.3.1 先驗簽章（`parser.go:101-109`）再驗 claims（`parser.go:119`），所以只有簽章正確的 token 才可能得到 `ErrTokenExpired`。
- golang-jwt 的 HMAC 只檢查 key 型別，不會拒絕空的 key，所以空的 `JWT_SECRET` 必須由我們自己擋。
- 現有 CORS `AllowHeaders` 沒有 `Authorization`，瀏覽器 preflight 會擋掉這個 header。

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | TWStockAPI 自己驗 JWT（共用 `JWT_SECRET`），不呼叫 AccountService、不抽共用 module | 沒有額外網路呼叫、AccountService 不用改；重複的只有約 40 行解析邏輯 |
| D2 | Flag 用記憶體快取，TTL 60 秒 | DB 改值後 60 秒內生效、不用重新部署；每分鐘最多一次 config 查詢 |
| D3 | `config` 列由 `db/config.sql` 插入（值 `'false'`，`ON CONFLICT DO NOTHING`），在 `.env` 指向的 DB 執行一次 | TWStockAPI 維持純唯讀；沒有這一列時一律視為 false |
| D4 | 過期回 **401** `{"error":"token expired"}`；其他驗證失敗回 401 `{"error":"unauthorized"}` | RFC 6750：過期屬於 `invalid_token`，回 401。403 是「已識別但沒有權限」；419、440 不是標準碼 |
| D5 | Filter 只掛在 `/api` group；`/health`、`/health/db` 不驗證 | Railway healthcheck 需要免驗證 |
| D6 | Flag=false 時完全不看 `Authorization`（即使 token 無效或過期） | 維持現狀；前端殘留的舊 token 不會造成問題 |
| D7 | Fail closed：flag=true 但 `JWT_SECRET` 為空，或讀 config / users 出錯 → 500，絕不放行 | DB 短暫出錯不能等於關掉驗證 |
| D8 | 啟動時 `JWT_SECRET` 為空只記 warning，不擋啟動 | 在 Railway 設好環境變數前部署新版，也不影響現有服務 |
| D9 | Config key 用 `JWT_TOKEN_ENABLE`；值 trim 後不分大小寫等於 `true` 才算開啟 | 使用者指定的名稱；判斷方式與 RealtimeJob 的 `is_trading_date` 一致 |

## Request Flow

```
Request → sloggin → Recovery → CORS（OPTIONS preflight 在這裡回 204）
  ├─ /health, /health/db ─────────▶ handler（不驗證）
  └─ /api/* ─▶ auth.RequireAuth
        1. flags.Enabled()（60 秒快取）
             error → 500
             false → c.Next()（不看 Authorization）
        2. secret 為空 → 500
        3. Authorization: Bearer <jwt>；缺少或格式錯 → 401 unauthorized
        4. ParseToken：ErrTokenExpired → 401 token expired；其他錯誤 → 401 unauthorized
        5. uuid.Parse(sub) 失敗 → 401 unauthorized（不查 DB）
        6. users.GetByID：ErrUserNotFound → 401 unauthorized；其他錯誤 → 500
        7. c.Set(auth.ContextUserKey, auth.User{ID, Email}) → c.Next()
```

## Components

新增 `auth/` package（結構與 AccountService 的 `auth/` 一致）。`auth` 依賴 `main/db`；`db` 不依賴 `auth`。

### `auth/jwt.go`

```go
var (
    ErrTokenExpired = errors.New("token expired")
    ErrInvalidToken = errors.New("invalid token")
)

// ParseToken validates an AccountService-issued HS256 JWT and returns its subject (users.id).
func ParseToken(secret []byte, tokenStr string) (string, error)
```

- `len(secret) == 0` → `ErrInvalidToken`（避免空 key 被當成合法密鑰）。
- `jwt.ParseWithClaims` 搭配 `jwt.RegisteredClaims`，options：`jwt.WithValidMethods([]string{"HS256"})`、`jwt.WithExpirationRequired()`。不設 leeway。
- `errors.Is(err, jwt.ErrTokenExpired)` → `ErrTokenExpired`；其他錯誤 → `ErrInvalidToken`。
- `sub` 為空 → `ErrInvalidToken`。

### `auth/flag.go`

```go
const FlagKey = "JWT_TOKEN_ENABLE"

type FlagSource interface {
    Enabled(ctx context.Context) (bool, error)
}

// FlagLoader returns the raw config value; found is false when the key is absent.
type FlagLoader func(ctx context.Context) (value string, found bool, err error)

type FlagCache struct {
    load FlagLoader
    ttl  time.Duration
    now  func() time.Time

    mu        sync.Mutex
    value     bool
    expiresAt time.Time
}

func NewFlagCache(load FlagLoader, ttl time.Duration) *FlagCache
func (f *FlagCache) Enabled(ctx context.Context) (bool, error)

// LoadJWTFlag runs: SELECT value FROM config WHERE key = 'JWT_TOKEN_ENABLE'
func LoadJWTFlag(ctx context.Context) (string, bool, error)
```

- `Enabled` 持有 mutex 檢查 `now() < expiresAt`，命中就回快取值；同一時間只有一個 goroutine 會查 DB。
- 快取過期就呼叫 `load`：失敗時直接回傳錯誤、不更新快取（下一個 request 會重試）；成功時 `value = found && EqualFold(TrimSpace(v), "true")`、`expiresAt = now() + ttl`。
- `now` 預設是 `time.Now`，測試可以替換。
- `LoadJWTFlag`：`pgx.ErrNoRows` → `("", false, nil)`。

### `auth/user.go`

```go
type User struct {
    ID    string
    Email string
}

var ErrUserNotFound = errors.New("user not found")

type UserStore interface {
    GetByID(ctx context.Context, id string) (User, error)
}

type PgxUserStore struct{}

// GetByID runs: SELECT id::text, email FROM users WHERE id = $1::uuid
func (PgxUserStore) GetByID(ctx context.Context, id string) (User, error)
```

- `pgx.ErrNoRows` → `ErrUserNotFound`。

### `auth/middleware.go`

```go
const ContextUserKey = "authUser"

func RequireAuth(flags FlagSource, users UserStore, secret []byte) gin.HandlerFunc
```

- 流程見上方 Request Flow。通過驗證後，context 存的是 `auth.User` 值（不是指標）。
- Bearer 解析與 AccountService 相同：`strings.SplitN(header, " ", 2)`、`Bearer` 不分大小寫、token trim 後不可為空。
- 傳給 `GetByID` 的是 `uuid.Parse(sub)` 正規化後的 `String()`。
- 每個失敗分支先 `_ = c.Error(reason)` 再 `c.AbortWithStatusJSON(...)`。slog-gin 會把 `c.Errors` 當成 log 訊息（4xx 記 Warn、5xx 記 Error）。reason 不包含 token 內容。

### `main.go`

- 讀 `JWT_SECRET`；空字串時 `logger.Warn(...)`。
- CORS `AllowHeaders` 加上 `"Authorization"`。
- `api := r.Group("/api", auth.RequireAuth(auth.NewFlagCache(auth.LoadJWTFlag, time.Minute), auth.PgxUserStore{}, jwtSecret))`。

### `db/config.sql`

```sql
INSERT INTO config (key, value) VALUES ('JWT_TOKEN_ENABLE', 'false')
ON CONFLICT (key) DO NOTHING;
```

### `go.mod`

- 新增 `github.com/golang-jwt/jwt/v5 v5.3.1`（與 AccountService 同版本）。
- `github.com/google/uuid v1.6.0` 由 indirect 改為 direct。

## Error Responses

只有 `JWT_TOKEN_ENABLE=true` 時才會出現：

| 情況 | HTTP | Body |
|---|---|---|
| 沒帶 `Authorization`，或格式不是 `Bearer <token>` | 401 | `{"error":"unauthorized"}` |
| 簽章錯、被竄改、不是 HS256、沒有 exp、沒有 sub | 401 | `{"error":"unauthorized"}` |
| 簽章正確但已過期 | 401 | `{"error":"token expired"}` |
| sub 不是 UUID，或 users 表查不到 | 401 | `{"error":"unauthorized"}` |
| 讀 config 或 users 時 DB 出錯 | 500 | `{"detail":"Internal server error"}` |
| flag=true 但 `JWT_SECRET` 為空 | 500 | `{"detail":"Internal server error"}` |

## Testing

| File | 需要 DB | 內容 |
|---|---|---|
| `auth/jwt_test.go` | 否 | 正常、過期、用錯 secret 簽的過期 token（→ `ErrInvalidToken`）、錯 secret、HS384、沒有 exp、沒有 sub、空 secret、亂碼 |
| `auth/flag_test.go` | 否 | TTL 內用快取、過期後重讀、錯誤不快取、沒有 key → false、值判斷（`true` / `TRUE` / ` true ` / `false` / `yes` / 空字串） |
| `auth/middleware_test.go` | 否 | Error Responses 表每一列；flag=false 時（無 token / 亂碼 token）放行；通過後 context 有 `User`；sub 不是 UUID 時不查 DB |
| `auth/store_test.go` | 是 | `GetByID` 查現有 user、查不存在的 UUID → `ErrUserNotFound`、`LoadJWTFlag` 不報錯 |

- `auth` package 的 `TestMain`：載入 `../.env`，有 `DATABASE_URL` 才 init pool，並且一律執行 `m.Run()`。DB 測試用 `requireDB(t)`，pool 為 nil 時 `t.Skip`，單元測試照跑。
- 測試風格沿用 AccountService：testify `assert` / `require`、`httptest`、`gin.TestMode`。
- 本機實測：flag=false 時啟動 server，`/api/stocks` 不帶 token、帶亂碼 token 都回 200。不把共用 DB 的 flag 改成 true。

## Docs

- `README.md`：新增「認證」段落（header 格式、開關、401 回應表、health 免驗證）；「設定」加 `JWT_SECRET`；「錯誤回應」補上 401。
- `CLAUDE.md`：Architecture 加上 `auth/` package 與 middleware；記錄 `JWT_SECRET`、`JWT_TOKEN_ENABLE`。
- `.env.example`：加上 `JWT_SECRET`（必須與 AccountService 相同）。

## Rollout

1. 部署新版（flag=false，行為不變）。
2. 在 Railway 設定 TWStockAPI 的 `JWT_SECRET`，值與 AccountService 相同。
3. 前端呼叫 TWStockAPI 時帶 `Authorization: Bearer <token>`，並處理 401。
4. `UPDATE config SET value = 'true', updated_time = now() WHERE key = 'JWT_TOKEN_ENABLE';` → 60 秒內生效；還原時改回 `'false'`。

步驟 2–4 由使用者執行。

## Out of Scope

- 依 `member_level` 分級存取。
- Token 撤銷 / 登出黑名單。
- 修改 AccountService 或它的前端串接文件。
