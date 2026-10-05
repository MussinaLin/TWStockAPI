# 我的最愛個股 — Design

**Date:** 2026-10-05
**Status:** Draft（待使用者審閱）
**Scope:** 新增「我的最愛」功能：使用者可以把個股加入或移除我的最愛，並查詢自己的清單，方便快速選取個股。數量上限依 `users.member_level` 而定，由新的 `user_config` 表設定。

## Goal

- 登入的使用者可以新增（POST）、刪除（DELETE）、查詢（GET）自己的我的最愛個股。
- 每個 `member_level` 有各自的上限，設定存在 `user_config`；超過上限時新增會被拒絕，**同時送出多個請求也不會超過上限**。
- 同一檔股票重複加入不會產生重複資料。
- 新增 `user_config` 表作為「所有 user 相關設定」的通用存放處，不限於 member level。

## Background（現況）

- TWStockAPI 原本全部是唯讀的 SELECT；這是第一批寫入型 endpoint。
- `users` 表（AccountService 擁有）已經有 `member_level INTEGER NOT NULL DEFAULT 0` 與 `is_og_member BOOLEAN`。
- `stocks.symbol` 是 `VARCHAR(10) PRIMARY KEY`。TWStockAnalysis 相關專案（RawData、RealtimeJob 等）都沒有刪除 `stocks` 列的程式。只有 `enabled = true` 的股票才有每日資料。
- 現有的 `config` 表欄位：`key VARCHAR(50) PK`、`value TEXT`、`created_time`、`updated_time`。
- `/api` group 掛了 `auth.RequireAuth`，由 `config.JWT_TOKEN_ENABLE` 控制；**開關關閉時 context 裡沒有 user**。
- CORS 的 `AllowMethods` 目前只有 `GET`。
- `rowsToMaps` 會把 `time.Time` 轉成 `YYYY-MM-DD`，時間部分會被丟掉。

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | `/api/favorites/*` **一律要求登入**，不受 `JWT_TOKEN_ENABLE` 影響 | 我的最愛必須知道屬於哪個 user；其他 endpoint 的行為不變 |
| D2 | `user_config` 用通用 key/value，`value` 是 `JSONB` | 表的定位是「所有 user 相關設定」，不能以 member_level 為主鍵；JSONB 可以存依 level 區分的對照表、單一值或陣列，新增設定不用改 schema |
| D3 | 上限的設定為 `key = 'FAVORITE_STOCKS_LIMIT'`，`value = {"0":10,"1":15,"2":20}`（JSON 的 key 是 member_level 字串） | 使用者指定的初始值 |
| D4 | 找不到設定、JSON 格式錯誤、找不到該 level，或值不是 ≥ 0 的整數，一律回 **500** | 設定漏設要讓人立刻發現，不能默默用預設值 |
| D5 | `is_og_member` 不影響上限 | 使用者指定 |
| D6 | `user_favorite_stocks` 用複合 PK `(user_id, symbol)` | 由 DB 保證不重複；`WHERE user_id = $1` 也直接用這個 index |
| D7 | FK `user_id → users(id) ON DELETE CASCADE`；`symbol → stocks(symbol) ON DELETE CASCADE` | 刪除帳號時一併清除；保證 symbol 存在，而且不會擋到刪除 stocks 列 |
| D8 | 依加入時間排序（`created_time DESC`），不加 `sort_order` | YAGNI；以後要自訂順序再加欄位，不影響現有資料 |
| D9 | 只能加入 `enabled = true` 的股票；加入後被停用的股票仍保留在清單中，GET 照樣列出 | 沒啟用的股票沒有資料 |
| D10 | 上限檢查在同一個 transaction 裡完成：`SELECT member_level FROM users WHERE id = $1 FOR UPDATE` 先鎖住該 user 的 row | 同一個 user 的 POST 會排隊處理，其他 user 不受影響；邏輯全部在 Go 裡，好測試；`member_level` 也順便在這裡讀取，不用改 `auth.User` |
| D11 | 重複 POST 回 **200** 並附上既有的那筆資料（即使已經額滿） | Idempotent，前端連點不會報錯 |
| D12 | DELETE 不論那筆是否存在都回 **204** | Idempotent |
| D13 | GET 回傳 `{limit, count, items}`，而不是單純的陣列 | 前端可以直接顯示「2/10」，額滿時把加入按鈕設成不可按 |
| D14 | `created_time` 回傳 RFC 3339 時間字串（直接 scan 進 struct，不經過 `rowsToMaps`） | `rowsToMaps` 只保留日期，會丟掉時間 |
| D15 | DDL 放在 `db/user_favorites.sql`（可重複執行），部署前在 `.env` 指向的 DB 手動執行 | 沿用 `db/config.sql` 的做法 |
| D16 | 時間欄位命名沿用 `config` 表的 `created_time` / `updated_time` | 與同一個 DB 裡的設定表一致 |

## Data Model — `db/user_favorites.sql`

```sql
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

`user_favorite_stocks` 的資料只會新增或刪除，不會修改，所以沒有 `updated_time`。

## API

三支 API 都在 `/api/favorites`，一律需要 `Authorization: Bearer <jwt>`。錯誤格式沿用現有慣例：一般錯誤回 `{"error": "..."}`，500 回 `{"detail": "Internal server error"}`。驗證失敗的回應與 `RequireAuth` 相同（401 `token expired` 或 `unauthorized`；沒設 `JWT_SECRET` 時回 500）。

### `GET /api/favorites`

```json
{
  "limit": 10,
  "count": 2,
  "items": [
    {"symbol": "2330", "name": "台積電", "created_time": "2026-10-05T09:12:00Z"},
    {"symbol": "2317", "name": "鴻海",   "created_time": "2026-10-04T15:00:00Z"}
  ]
}
```

- `items` 依 `created_time DESC` 排序，join `stocks` 取得 `name`；清單是空的時候回 `[]`，不回 `null`。
- `limit` 依 D4 的規則讀取（不加鎖），讀取失敗回 500。
- `count` = `len(items)`。

### `POST /api/favorites`

Request body：`{"symbol": "2330"}`；symbol 會先去掉前後空白。

| 情況 | 回應 |
|---|---|
| 新增成功 | **201** `{"symbol","name","created_time"}` |
| 本來就已加入 | **200** 回傳既有的那筆資料（即使已經額滿） |
| body 不是合法的 JSON，或 symbol 是空的 | 400 `{"error":"invalid request"}` |
| 股票不存在，或 `enabled = false` | 404 `{"error":"stock not found"}` |
| 已達上限 | 409 `{"error":"favorite limit reached","limit":10}` |
| 鎖定時 user 的 row 已經不存在 | 401 `{"error":"unauthorized"}` |
| 上限設定錯誤（D4），或其他 DB 錯誤 | 500 |

Transaction 流程（`db.Pool().Begin`，預設 READ COMMITTED）：

```
1. SELECT member_level FROM users WHERE id = $1 FOR UPDATE
     沒有這一列 → rollback，回 401
2. SELECT s.symbol, s.name, f.created_time FROM user_favorite_stocks f JOIN stocks s USING (symbol)
     WHERE f.user_id = $1 AND f.symbol = $2
     找到 → rollback，回 200 + 既有資料
3. SELECT name FROM stocks WHERE symbol = $1 AND enabled = true
     找不到 → rollback，回 404
4. SELECT value FROM user_config WHERE key = 'FAVORITE_STOCKS_LIMIT' → parseFavoriteLimit(value, level)
     錯誤 → rollback，回 500
5. SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1
     count >= limit → rollback，回 409
6. INSERT INTO user_favorite_stocks (user_id, symbol) VALUES ($1, $2) RETURNING created_time
7. COMMIT → 回 201
```

### `DELETE /api/favorites/:symbol`

- 執行 `DELETE FROM user_favorite_stocks WHERE user_id = $1 AND symbol = $2`，不論影響幾列都回 **204**，沒有 body。
- 不加鎖，也不讀上限。DB 錯誤回 500。

## Components

### `auth/middleware.go`

- 把 `RequireAuth` 第 2–7 步（檢查 secret、取 Bearer、`ParseToken`、解析 uuid、`users.GetByID`、`c.Set`）抽成內部函式 `authenticate(c *gin.Context, users UserStore, secret []byte) bool`。驗證失敗時，函式內會先 abort 並回應，再回傳 false。`RequireAuth` 的對外行為完全不變。
- 新增：

```go
// RequireLogin always requires a valid AccountService JWT, regardless of
// JWT_TOKEN_ENABLE. If RequireAuth already authenticated the request, it
// passes through without looking the user up again.
func RequireLogin(users UserStore, secret []byte) gin.HandlerFunc

// CurrentUser returns the authenticated User stored by RequireAuth/RequireLogin.
func CurrentUser(c *gin.Context) (User, bool)
```

### `routers/favorites.go`

```go
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc)
// g := rg.Group("/favorites", requireLogin)
// g.GET("", listFavorites); g.POST("", addFavorite); g.DELETE("/:symbol", deleteFavorite)

const favoriteLimitKey = "FAVORITE_STOCKS_LIMIT"

type favoriteItem struct {
    Symbol      string    `json:"symbol"`
    Name        string    `json:"name"`
    CreatedTime time.Time `json:"created_time"`
}

// parseFavoriteLimit 解析 FAVORITE_STOCKS_LIMIT 的 JSONB value，回傳該 member_level 的上限。
// JSON 格式錯誤、找不到該 level、值不是 >= 0 的整數，都回傳 error。
func parseFavoriteLimit(raw []byte, level int) (int, error)
```

- SQL 直接寫在 handler 裡，沿用其他 router 的寫法。
- 讀取上限的查詢（`user_config` + `parseFavoriteLimit`）抽成 helper，讓 GET 和 POST 共用，參數接受 `pgx.Tx` 或 pool 共通的 query interface。
- Handler 用 `auth.CurrentUser(c)` 取得 user；理論上一定拿得到（`RequireLogin` 保證），萬一沒有就回 500。

### `main.go`

- CORS `AllowMethods`：`[]string{"GET", "POST", "DELETE"}`。
- `routers.RegisterFavorites(api, auth.RequireLogin(auth.PgxUserStore{}, jwtSecret))`。

Request flow：

```
Request → sloggin → Recovery → CORS（OPTIONS preflight 在這裡回 204）
  └─ /api/favorites/* ─▶ auth.RequireAuth（看開關）─▶ auth.RequireLogin（一律驗證；已經有 user 就直接放行）─▶ handler
```

## Testing

### 單元測試（不需要 DB）

- `auth/middleware_test.go` — `RequireLogin`：
  - 開關不影響（`RequireLogin` 本身不看開關）：沒帶 token → 401 `unauthorized`。
  - token 有效 → 放行，`CurrentUser` 拿得到正確的 user。
  - token 過期 → 401 `token expired`。
  - context 已經有 user → 放行，而且**不呼叫** `UserStore`（用 fake 的呼叫次數驗證）。
  - 沒設 secret → 500。
  - 現有的 `RequireAuth` 測試全部維持綠燈（確認抽出 `authenticate` 後行為不變）。
- `routers/favorites_limit_test.go` — `parseFavoriteLimit`：正常取值、值為 0、level 不存在、JSON 格式錯誤、值是小數、字串或負數。這個檔案不依賴 DB；由於 routers package 的 `TestMain` 在沒有 DB 時會跳過整個 package，這些測試同樣只有在設定 DB 時才會執行。

### 整合測試 — `routers/favorites_test.go`（真的會寫入 `.env` 的 DB；已經使用者同意）

- Setup：在 `users` 插入一個暫時的測試 user（`google_sub = 'test-favorites-<uuid>'`、`member_level = 0`），`t.Cleanup` 刪除這個 user，他的我的最愛會一併被 cascade 刪除。
- Test router：`RegisterFavorites(api, fakeLogin)`，`fakeLogin` 直接把測試 user `c.Set` 進 context。
- 上限從 `user_config` 讀取，測試不寫死數字；`enabled` 的股票數量不足 `limit + 5` 檔時 `t.Skip`。
- 案例：
  1. GET 空清單 → 200，`count = 0`、`items = []`、`limit` 等於設定值。
  2. POST 一檔已啟用的股票 → 201；再 POST 一次 → 200；GET 的 `count = 1`。
  3. POST 不存在的 symbol → 404；POST `enabled = false` 的股票 → 404（找不到這種股票時 skip）。
  4. POST 空的 symbol 或 body 格式錯誤 → 400。
  5. 加到上限 → 下一個 POST 回 409，回應帶 `limit`。
  6. DELETE 已加入的股票 → 204；再 DELETE 一次 → 204；GET 確認已移除。
  7. **同時送出多個 POST**：用 goroutine 同時送出 `limit + 5` 個不同 symbol 的 POST，結束後 DB 中的數量必須剛好等於 `limit`，201 的數量也等於 `limit`，其餘都是 409。

## Docs

- `CLAUDE.md`：Architecture 新增 `favorites.go` 與 `user_config` / `user_favorite_stocks` 的說明；「All endpoints are read-only」改成「除了 `/api/favorites` 之外都是唯讀」；說明 `RequireLogin`。
- `README.md`：新增三支 API 的規格，以及 `db/user_favorites.sql` 的套用方式。
- `docs/frontend-api-auth-integration.md`：註明 `/api/favorites` 一律需要 token，不受開關影響。

## Out of Scope

- 自訂排序（`sort_order`）。
- 管理 `user_config` 的 API（目前直接改 DB）。
- `user_config` 的記憶體快取（POST 和 GET 頻率低，每次查詢就好）。
