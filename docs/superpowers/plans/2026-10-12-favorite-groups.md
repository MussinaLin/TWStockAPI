# 我的最愛群組 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `/api/favorites` 從單一清單改成依 `member_level` 固定數量的群組（群組一、群組二……），GET 回傳所有群組與群組底下的個股，新增和刪除都針對指定群組。

**Architecture:** 不建群組表，在 `user_favorite_stocks` 加 `group_no`，PK 改成 `(user_id, group_no, symbol)`。群組數與每組上限放在 `user_config.FAVORITE_GROUPS`（JSONB），解析與群組命名放在新檔 `routers/favorites_config.go`（純函式）。`routers/favorites.go` 改寫三支 handler：POST 沿用 transaction + `SELECT ... FOR UPDATE` 鎖住 user 的 row，檢查的是「該群組」的數量。

**Tech Stack:** Go 1.25、Gin v1.12、pgx/v5 v5.9.1、github.com/google/uuid v1.6.0、testify。

**Spec:** `docs/superpowers/specs/2026-10-12-favorite-groups-design.md`

## Global Constraints

- Go module 名稱是 `main`（import path 例如 `main/db`、`main/auth`）。不新增任何依賴。
- 設定 key 固定為 `FAVORITE_GROUPS`；初始值固定為 `{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`；不設定 level 2；刪除 `FAVORITE_STOCKS_LIMIT`。
- 設定錯誤（不存在、JSON 格式錯誤、找不到 level、缺少欄位、`groups` 不在 1–99、`stocks_per_group` < 0、值不是整數）一律回 500。
- 路徑：`GET /api/favorites`、`POST /api/favorites/groups/:group_no`、`DELETE /api/favorites/groups/:group_no/:symbol`；舊的 `POST /api/favorites`、`DELETE /api/favorites/:symbol` 移除。
- GET 回應：`{"max_groups":int,"max_stocks_per_group":int,"groups":[{"group_no":int,"name":"群組一","count":int,"items":[{"symbol","name","created_time"}]}]}`；`groups` 固定 `max_groups` 個、依 `group_no` 由小到大；空群組 `items` 為 `[]`；群組內 `created_time DESC, symbol`；`created_time` 為 RFC 3339。
- POST 回應 body（201 / 200）：`{"group_no":int,"symbol","name","created_time"}`。
- 回應 body 必須完全相同：
  - 400 `{"error":"invalid request"}`
  - 404 `{"error":"group not found"}` / `{"error":"stock not found"}`
  - 409 `{"error":"favorite group full","limit":<stocks_per_group>}`
  - 401 `{"error":"unauthorized"}` / `{"error":"token expired"}`
  - 500 `{"detail":"Internal server error"}`
- `group_no` 用 `strconv.Atoi` 解析；失敗或 < 1 回 400；`+1`、`01` 視為 1；超過目前等級的群組數回 404 `group not found`，而且在任何使用 `group_no` 的 SQL 之前就回應。
- 同一檔股票可以放在多個群組；DELETE 只移除指定群組的那一筆。
- 降級：GET 只回傳 `group_no ≤ max_groups` 的資料；群組內超過上限照樣全部列出，但不能再加入。
- symbol 不合法時：POST（去掉空白後是空字串或含 NUL）回 400；DELETE（含 NUL **或不是合法的 UTF-8**）直接回 204、不查 DB。DELETE 多擋「不是合法的 UTF-8」是延伸 spec D15 的理由（不合法的輸入不應該產生 500）：PostgreSQL 對非 UTF-8 的參數同樣回 `22021`；POST 的 body 經過 JSON 解碼一定是合法的 UTF-8，不需要擋。
- 只能加入 `stocks.enabled = true` 的股票；`is_og_member` 不影響規則。
- `auth/`、`main.go` 不修改。
- 遷移：`db/user_favorites_v2_migration.sql` 只在 staging（`.env` 的 DB）跑一次，**執行前要先向使用者確認**；production 只跑 `db/user_favorites.sql`。
- 測試風格：testify `assert` / `require`、`httptest`、`gin.TestMode`。routers 的整合測試會寫入 `.env` 的 DB（使用者已同意），測試資料必須在 `t.Cleanup` 清除。
- 改到 API 時要同步更新 `CLAUDE.md` 與 `README.md`；`docs/frontend-favorites-integration.md` 要改寫成群組版（使用者指定的交付物）。
- Commit message 不加 `Co-Authored-By:` 或任何署名行。

## Review Focus

1. **DELETE 的 symbol 不是合法的 UTF-8**（`DELETE /api/favorites/groups/1/%FF`）：要回 204，不能回 500。由 Task 2 的 `TestDeleteFavorite_InvalidSymbol` 負責。
2. **symbol 中間夾著 NUL**（`{"symbol":"23\u000030"}`）：要回 400，不能回 500。由 Task 3 的 `TestAddFavorite_InvalidRequest`（`nul in middle`）負責。
3. **`group_no` 是很大的數字或特殊寫法**（`99999`、`+1`、`01`）：`99999` 回 404 `group not found`，不能因為超出 SMALLINT 而回 500；`+1`、`01` 視為群組一。由 Task 2 的 `TestDeleteFavorite_GroupNotFound`、Task 3 的 `TestAddFavorite_GroupNotFound` 與 `TestAddFavorite_GroupNoVariants` 負責。
4. **user 的資料全部在已隱藏的群組**（降級後只剩群組八有資料）：GET 要回傳全部可見群組、都是空的，不能 panic 或 index out of range。由 Task 2 的 `TestListFavorites_OnlyHiddenGroups` 負責。
5. **同一個 user 同時對兩個群組送出多個 POST**：每個群組都剛好到上限，不會互相影響，也不會 deadlock。由 Task 3 的 `TestAddFavorite_ConcurrentRequestsRespectGroupLimit`（兩個群組同時送）負責。

---

## File Structure

| File | 動作 | 職責 |
|---|---|---|
| `routers/favorites_config.go` | Create | `favoriteGroupsKey`、`maxFavoriteGroups`、`groupRule`、`parseFavoriteGroups`、`loadFavoriteGroups`、`groupName` |
| `routers/favorites_config_test.go` | Create | `parseFavoriteGroups`、`groupName` 單元測試 |
| `routers/favorites_limit_test.go` | Delete | 舊的 `parseFavoriteLimit` 測試 |
| `routers/favorites.go` | Rewrite | `RegisterFavorites`、GET / POST / DELETE handler、`loadUserRule`、`parseGroupNo`、`invalidSymbol` |
| `routers/favorites_test.go` | Rewrite | 群組版的 fixture 與整合測試 |
| `db/user_favorites.sql` | Rewrite | 新 schema 的最終狀態（可重複執行） |
| `db/user_favorites_v2_migration.sql` | Create | v1 → v2 遷移（只跑一次，會刪資料） |
| `CLAUDE.md`、`README.md`、`docs/frontend-favorites-integration.md` | Modify / Rewrite | 文件同步；前端串接文件改寫成群組版 |

---

### Task 1: 群組規則解析與群組命名

**Files:**
- Create: `routers/favorites_config.go`
- Test: `routers/favorites_config_test.go`

**Interfaces:**
- Consumes：`routers/favorites.go` 既有的 `type querier interface { QueryRow(ctx context.Context, sql string, args ...any) pgx.Row }`（Task 2 改寫時保留）。
- Produces（Task 2、3 會用到）：
  - `const favoriteGroupsKey = "FAVORITE_GROUPS"`
  - `const maxFavoriteGroups = 99`
  - `type groupRule struct { Groups int; StocksPerGroup int }`
  - `func parseFavoriteGroups(raw []byte, level int) (groupRule, error)`
  - `func loadFavoriteGroups(ctx context.Context, q querier, level int) (groupRule, error)`
  - `func groupName(n int) string`

- [ ] **Step 1: 寫失敗測試**

建立 `routers/favorites_config_test.go`：

```go
package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFavoriteGroups(t *testing.T) {
	raw := []byte(`{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`)

	got, err := parseFavoriteGroups(raw, 0)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 5, StocksPerGroup: 5}, got)

	got, err = parseFavoriteGroups(raw, 1)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 10, StocksPerGroup: 10}, got)

	got, err = parseFavoriteGroups([]byte(`{"0": {"groups": 99, "stocks_per_group": 0}}`), 0)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 99, StocksPerGroup: 0}, got)
}

func TestParseFavoriteGroupsErrors(t *testing.T) {
	cases := map[string]struct {
		raw   string
		level int
	}{
		"level missing":            {`{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`, 2},
		"groups missing":           {`{"0": {"stocks_per_group": 5}}`, 0},
		"stocks_per_group missing": {`{"0": {"groups": 5}}`, 0},
		"level null":               {`{"0": null}`, 0},
		"groups zero":              {`{"0": {"groups": 0, "stocks_per_group": 5}}`, 0},
		"groups over 99":           {`{"0": {"groups": 100, "stocks_per_group": 5}}`, 0},
		"negative stocks":          {`{"0": {"groups": 5, "stocks_per_group": -1}}`, 0},
		"fractional":               {`{"0": {"groups": 5.5, "stocks_per_group": 5}}`, 0},
		"string value":             {`{"0": {"groups": "5", "stocks_per_group": 5}}`, 0},
		"old flat format":          {`{"0": 10}`, 0},
		"invalid json":             {`{`, 0},
		"null":                     {`null`, 0},
		"array":                    {`[5]`, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFavoriteGroups([]byte(tc.raw), tc.level)
			assert.Error(t, err)
		})
	}
}

func TestGroupName(t *testing.T) {
	cases := map[int]string{
		1:  "群組一",
		9:  "群組九",
		10: "群組十",
		11: "群組十一",
		19: "群組十九",
		20: "群組二十",
		21: "群組二十一",
		99: "群組九十九",
	}
	for n, want := range cases {
		assert.Equal(t, want, groupName(n), "groupName(%d)", n)
	}
}
```

- [ ] **Step 2: 執行測試，確認失敗**

Run: `go test ./routers/ -run 'ParseFavoriteGroups|GroupName' -v`
Expected：編譯失敗，`undefined: parseFavoriteGroups`、`undefined: groupRule`、`undefined: groupName`。

- [ ] **Step 3: 實作**

建立 `routers/favorites_config.go`：

```go
package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// favoriteGroupsKey is the user_config key holding the per-member_level group
// rules, e.g. {"0": {"groups": 5, "stocks_per_group": 5}}.
const favoriteGroupsKey = "FAVORITE_GROUPS"

// maxFavoriteGroups is the most groups groupName can name (群組九十九).
const maxFavoriteGroups = 99

// groupRule is one member_level's favorites layout.
type groupRule struct {
	Groups         int
	StocksPerGroup int
}

// loadFavoriteGroups 讀取 user_config 的 FAVORITE_GROUPS，回傳 level 對應的規則。
func loadFavoriteGroups(ctx context.Context, q querier, level int) (groupRule, error) {
	var raw string
	err := q.QueryRow(ctx,
		"SELECT value::text FROM user_config WHERE key = $1", favoriteGroupsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return groupRule{}, fmt.Errorf("favorites: %s is not set in user_config", favoriteGroupsKey)
	}
	if err != nil {
		return groupRule{}, fmt.Errorf("favorites: load %s: %w", favoriteGroupsKey, err)
	}
	return parseFavoriteGroups([]byte(raw), level)
}

// parseFavoriteGroups 解析 FAVORITE_GROUPS 的 JSONB value，回傳該 member_level 的規則。
// JSON 格式錯誤、找不到該 level、缺少欄位、groups 不在 1–99、stocks_per_group < 0，
// 或值不是整數，都回傳 error。
func parseFavoriteGroups(raw []byte, level int) (groupRule, error) {
	var rules map[string]*struct {
		Groups         *int `json:"groups"`
		StocksPerGroup *int `json:"stocks_per_group"`
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return groupRule{}, fmt.Errorf("favorites: parse %s: %w", favoriteGroupsKey, err)
	}
	r := rules[strconv.Itoa(level)]
	if r == nil {
		return groupRule{}, fmt.Errorf("favorites: %s has no rule for member_level %d", favoriteGroupsKey, level)
	}
	if r.Groups == nil || r.StocksPerGroup == nil {
		return groupRule{}, fmt.Errorf("favorites: %s rule for member_level %d needs groups and stocks_per_group", favoriteGroupsKey, level)
	}
	if *r.Groups < 1 || *r.Groups > maxFavoriteGroups {
		return groupRule{}, fmt.Errorf("favorites: %s groups for member_level %d must be 1-%d, got %d", favoriteGroupsKey, level, maxFavoriteGroups, *r.Groups)
	}
	if *r.StocksPerGroup < 0 {
		return groupRule{}, fmt.Errorf("favorites: %s stocks_per_group for member_level %d is negative: %d", favoriteGroupsKey, level, *r.StocksPerGroup)
	}
	return groupRule{Groups: *r.Groups, StocksPerGroup: *r.StocksPerGroup}, nil
}

var chineseDigits = [...]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// groupName 回傳群組名稱，例如 1 → 群組一、10 → 群組十、21 → 群組二十一。
// n 必須在 1 到 maxFavoriteGroups 之間（parseFavoriteGroups 已保證）。
func groupName(n int) string {
	tens, ones := n/10, n%10
	s := ""
	if tens > 1 {
		s = chineseDigits[tens]
	}
	if tens > 0 {
		s += "十"
	}
	return "群組" + s + chineseDigits[ones]
}
```

- [ ] **Step 4: 執行測試，確認通過**

Run: `gofmt -w routers && go test ./routers/ -run 'ParseFavoriteGroups|GroupName' -v -count=1`
Expected：`TestParseFavoriteGroups`、`TestParseFavoriteGroupsErrors`（13 個 subtest）、`TestGroupName` 全部 PASS。沒設 `DATABASE_URL` 時整個 package 會 skip，**這時要回報，不能當成通過**。

再執行 `go vet ./routers/ && go test ./routers/ -count=1`，確認既有的測試（包括舊版 favorites 測試）都還是綠的。

- [ ] **Step 5: Commit**

```bash
git add routers/favorites_config.go routers/favorites_config_test.go
git commit -m "feat: 新增我的最愛群組規則解析與群組命名"
```

---

### Task 2: 新 schema、遷移 staging、GET 與 DELETE

**Files:**
- Rewrite: `db/user_favorites.sql`
- Create: `db/user_favorites_v2_migration.sql`
- Rewrite: `routers/favorites.go`
- Rewrite: `routers/favorites_test.go`
- Delete: `routers/favorites_limit_test.go`

**Interfaces:**
- Consumes：Task 1 的 `groupRule`、`loadFavoriteGroups(ctx, q querier, level int) (groupRule, error)`、`groupName(n int) string`；`auth.CurrentUser(c) (auth.User, bool)`、`auth.ContextUserKey`、`auth.User{ID, Email}`；`db.Pool()`；`setup_test.go` 的 `doJSON(t, r, method, path) (int, any)`、`requireKeys(t, m, keys...)`。
- Produces（Task 3 會用到）：
  - `func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc)`：Task 2 只註冊 GET 與 DELETE。
  - `type favoriteItem struct { Symbol, Name string; CreatedTime time.Time }`（JSON：`symbol`、`name`、`created_time`）
  - `type favoriteGroup struct { GroupNo int; Name string; Count int; Items []favoriteItem }`（JSON：`group_no`、`name`、`count`、`items`）
  - `type querier interface { QueryRow(ctx context.Context, sql string, args ...any) pgx.Row }`
  - `var errUserGone error`
  - `const selectMemberLevel`、`const selectMemberLevelForUpdate`
  - `func loadUserRule(ctx context.Context, q querier, levelSQL, userID string) (groupRule, error)`
  - `func respondRuleError(c *gin.Context, err error)`
  - `func parseGroupNo(c *gin.Context) (int, bool)`
  - `func invalidSymbol(s string) bool`
  - `func favoritesServerError(c *gin.Context, err error)`
  - 測試 helper：`newFavoritesUser(t, level int) string`、`newFavoritesRouter(userID string) *gin.Engine`、`groupRuleFor(t, level int) groupRule`、`enabledSymbols(t, n int) []string`、`disabledSymbol(t) string`、`addFavoriteRow(t, userID string, groupNo int, symbol string, created time.Time)`、`groupCount(t, userID string, groupNo int) int`、`groupsOf(t, body any) []map[string]any`

- [ ] **Step 1: 改寫 `db/user_favorites.sql`，新增遷移檔**

`db/user_favorites.sql` 整個換成：

```sql
-- 我的最愛個股（TWStockAPI 擁有）；可重複執行
-- 全新環境：psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites.sql
-- 套用過舊版（單一清單）的環境：改跑 db/user_favorites_v2_migration.sql

-- user 相關設定（通用 key/value；value 為 JSONB）
CREATE TABLE IF NOT EXISTS user_config (
    key          VARCHAR(50) PRIMARY KEY,
    value        JSONB       NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 各 member_level 的群組數與每組上限；JSON 的 key 為 member_level 的字串
INSERT INTO user_config (key, value, description) VALUES
  ('FAVORITE_GROUPS',
   '{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}',
   '各 member_level 的我的最愛群組數與每組個股上限')
ON CONFLICT (key) DO NOTHING;

-- user 的我的最愛個股；group_no 從 1 開始，群組名稱由後端依 group_no 產生
CREATE TABLE IF NOT EXISTS user_favorite_stocks (
    user_id      UUID        NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    group_no     SMALLINT    NOT NULL CHECK (group_no >= 1),
    symbol       VARCHAR(10) NOT NULL REFERENCES stocks(symbol) ON DELETE CASCADE,
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_no, symbol)
);
```

建立 `db/user_favorites_v2_migration.sql`：

```sql
-- 我的最愛 v1（單一清單）→ v2（群組）遷移
-- ⚠ 會刪除所有我的最愛資料。只在套用過 v1 的環境執行一次（目前只有 staging）。
-- 執行方式（在 repo 根目錄）：psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites_v2_migration.sql
BEGIN;
DROP TABLE IF EXISTS user_favorite_stocks;
DELETE FROM user_config WHERE key = 'FAVORITE_STOCKS_LIMIT';
\ir user_favorites.sql
COMMIT;
```

- [ ] **Step 2: 在 staging 執行遷移（要先向使用者確認）**

這一步會**刪除 staging 上所有我的最愛資料**並改 schema。執行前先向使用者確認；取得同意後執行：

```bash
set -a; source .env; set +a
psql "$DATABASE_URL" -Atc "SELECT count(*) FROM user_favorite_stocks"   # 記下刪除前的筆數
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites_v2_migration.sql
psql "$DATABASE_URL" -Atc "SELECT column_name FROM information_schema.columns WHERE table_name = 'user_favorite_stocks' ORDER BY ordinal_position"
psql "$DATABASE_URL" -Atc "SELECT key, value FROM user_config ORDER BY key"
psql "$DATABASE_URL" -Atc "SELECT count(*) FROM user_favorite_stocks"
```

Expected：
- 遷移輸出 `BEGIN`、`DROP TABLE`、`DELETE 1`、`CREATE TABLE`（user_config 已存在時為 NOTICE + `CREATE TABLE`）、`INSERT 0 1`、`CREATE TABLE`、`COMMIT`。
- 欄位依序為 `user_id`、`group_no`、`symbol`、`created_time`。
- `user_config` 只剩 `FAVORITE_GROUPS|{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`。
- 筆數為 `0`。

遷移後舊版的 favorites 程式與測試會失效（INSERT 沒有 `group_no`），由下面的步驟取代。

- [ ] **Step 3: 改寫測試（RED）**

刪除 `routers/favorites_limit_test.go`：

```bash
git rm -q routers/favorites_limit_test.go
```

`routers/favorites_test.go` 整個換成：

```go
package routers

import (
	"context"
	"errors"
	"fmt"
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
```

- [ ] **Step 4: 執行測試，確認失敗**

Run: `go test ./routers/ -run 'Favorite' -count=1 2>&1 | tail -20`
Expected：可以編譯（新測試只用到 Task 1 的 `loadFavoriteGroups`、`groupName` 與既有的 `RegisterFavorites`），但測試 FAIL：`TestListFavorites_*` 因為舊的回應格式沒有 `groups`（或舊版查詢對新 schema 出錯回 500）而失敗；`TestDeleteFavorite_*` 因為路由不存在，得到 `404 page not found`。確認失敗原因是「功能還沒實作」，不是測試本身的錯字。

- [ ] **Step 5: 改寫 `routers/favorites.go`（GET 與 DELETE）**

整個換成：

```go
package routers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// RegisterFavorites mounts /favorites behind requireLogin; every handler needs
// the authenticated user.
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.DELETE("/groups/:group_no/:symbol", deleteFavorite)
}

type favoriteItem struct {
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

type favoriteGroup struct {
	GroupNo int            `json:"group_no"`
	Name    string         `json:"name"`
	Count   int            `json:"count"`
	Items   []favoriteItem `json:"items"`
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// errUserGone means the authenticated user's users row no longer exists.
var errUserGone = errors.New("favorites: user no longer exists")

// selectMemberLevel 讀取 user 的 member_level；selectMemberLevelForUpdate 同時鎖住
// user 的 row，讓同一個 user 的 POST 排隊處理。
const (
	selectMemberLevel          = "SELECT member_level FROM users WHERE id = $1::uuid"
	selectMemberLevelForUpdate = selectMemberLevel + " FOR UPDATE"
)

// loadUserRule 讀取 user 的 member_level 與對應的群組規則。levelSQL 為
// selectMemberLevel 或 selectMemberLevelForUpdate；user 不存在時回傳 errUserGone。
func loadUserRule(ctx context.Context, q querier, levelSQL, userID string) (groupRule, error) {
	var level int
	err := q.QueryRow(ctx, levelSQL, userID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return groupRule{}, errUserGone
	}
	if err != nil {
		return groupRule{}, fmt.Errorf("favorites: load member_level: %w", err)
	}
	return loadFavoriteGroups(ctx, q, level)
}

// respondRuleError 依 loadUserRule 的錯誤回 401（user 已不存在）或 500。
func respondRuleError(c *gin.Context, err error) {
	if errors.Is(err, errUserGone) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	favoritesServerError(c, err)
}

// parseGroupNo 解析 :group_no；只接受 >= 1 的整數。
func parseGroupNo(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("group_no"))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// invalidSymbol 回報 PostgreSQL 無法當成字串參數的 symbol（含 NUL 或不是合法的 UTF-8）；
// 這種 symbol 不可能存在於 stocks。
func invalidSymbol(s string) bool {
	return strings.ContainsRune(s, 0) || !utf8.ValidString(s)
}

// listFavorites 回傳目前等級的所有群組（包括空的群組）與群組底下的個股。
// group_no 超過目前群組數的資料（降級後）保留在 DB，但不回傳。
func listFavorites(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	ctx := c.Request.Context()

	rule, err := loadUserRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}

	groups := make([]favoriteGroup, rule.Groups)
	for i := range groups {
		groups[i] = favoriteGroup{GroupNo: i + 1, Name: groupName(i + 1), Items: []favoriteItem{}}
	}

	rows, err := db.Pool().Query(ctx,
		`SELECT f.group_no, f.symbol, s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid AND f.group_no <= $2
		ORDER BY f.group_no, f.created_time DESC, f.symbol`, user.ID, rule.Groups)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var groupNo int
		var item favoriteItem
		if err := rows.Scan(&groupNo, &item.Symbol, &item.Name, &item.CreatedTime); err != nil {
			favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
			return
		}
		g := &groups[groupNo-1]
		g.Items = append(g.Items, item)
		g.Count++
	}
	if err := rows.Err(); err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"max_groups":           rule.Groups,
		"max_stocks_per_group": rule.StocksPerGroup,
		"groups":               groups,
	})
}

// deleteFavorite 從指定群組移除一檔股票；不論原本是否存在都回 204。
func deleteFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	groupNo, ok := parseGroupNo(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	symbol := c.Param("symbol")
	if invalidSymbol(symbol) {
		c.Status(http.StatusNoContent)
		return
	}
	ctx := c.Request.Context()

	rule, err := loadUserRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}
	if groupNo > rule.Groups {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	_, err = db.Pool().Exec(ctx,
		"DELETE FROM user_favorite_stocks WHERE user_id = $1::uuid AND group_no = $2 AND symbol = $3",
		user.ID, groupNo, symbol)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: delete: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}

// favoritesServerError records err for slog-gin and responds 500.
func favoritesServerError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
```

- [ ] **Step 6: 執行測試，確認通過**

Run: `gofmt -w routers && go vet ./routers/ && go test ./routers/ -run 'Favorite|GroupName' -v -count=1`
Expected：vet 沒有輸出；`TestParseFavoriteGroups*`、`TestGroupName`、`TestListFavorites_*`（9 個）、`TestDeleteFavorite_*`（8 個）全部 PASS；沒有已停用股票時 `TestListFavorites_IncludesDisabledStock` 會 SKIP。

再執行 `go test ./routers/ -count=1`，確認其他 router 測試沒受影響；並確認測試資料已清除：

```bash
set -a; source .env; set +a
psql "$DATABASE_URL" -Atc "SELECT (SELECT count(*) FROM users WHERE google_sub LIKE 'test-favorites-%'), (SELECT count(*) FROM user_favorite_stocks)"
```

Expected：`0|0`。

- [ ] **Step 7: Commit**

```bash
git add db/user_favorites.sql db/user_favorites_v2_migration.sql routers/favorites.go routers/favorites_test.go
git commit -m "feat: 我的最愛改為群組，GET 回傳所有群組並改用群組版 DELETE"
```

（`routers/favorites_limit_test.go` 已在 Step 3 用 `git rm` 加入 index。）

---

### Task 3: `POST /api/favorites/groups/:group_no`

**Files:**
- Modify: `routers/favorites.go`
- Test: `routers/favorites_test.go`

**Interfaces:**
- Consumes：Task 2 的 `RegisterFavorites`、`loadUserRule`、`selectMemberLevelForUpdate`、`respondRuleError`、`parseGroupNo`、`favoritesServerError`、所有測試 helper（`newFavoritesUser`、`newFavoritesRouter`、`groupRuleFor`、`enabledSymbols`、`disabledSymbol`、`addFavoriteRow`、`groupCount`、`groupsOf`、`groupPath`）；Task 1 的 `groupRule`。
- Produces：`g.POST("/groups/:group_no", addFavorite)`；`type favoriteGroupItem struct { GroupNo int; Symbol, Name string; CreatedTime time.Time }`；`type addFavoriteRequest struct { Symbol string }`；`func addFavorite(c *gin.Context)`；測試 helper `doJSONBody(t, r, method, path, body string) (int, any)`、`postStatus(r *gin.Engine, groupNo int, symbol string) int`。

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
```

- [ ] **Step 2: 執行測試，確認失敗**

Run: `go test ./routers/ -run 'AddFavorite' -count=1 2>&1 | grep -E '^\s*--- |^(ok|FAIL)' | head -30`
Expected：FAIL。POST 路由還沒註冊，回 `404 page not found`（純文字），`doJSONBody` 解析 body 失敗，或狀態碼斷言失敗；同時送出的測試得到全部 404。

- [ ] **Step 3: 實作 `addFavorite`**

在 `routers/favorites.go`：

1. `RegisterFavorites` 改成：

```go
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.POST("/groups/:group_no", addFavorite)
	g.DELETE("/groups/:group_no/:symbol", deleteFavorite)
}
```

2. 在 `type favoriteGroup` 後面加上：

```go
// favoriteGroupItem is the POST response: one stock in one group.
type favoriteGroupItem struct {
	GroupNo     int       `json:"group_no"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}
```

3. 在 `// deleteFavorite 從指定群組移除一檔股票` 前面加上：

```go
type addFavoriteRequest struct {
	Symbol string `json:"symbol"`
}

// addFavorite 把一檔股票加入指定群組。整個流程在同一個 transaction 裡，先以 FOR UPDATE
// 鎖住 user 的 row，讓同一個 user 的請求排隊處理，避免同時送出時超過群組上限。
func addFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	groupNo, ok := parseGroupNo(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	var req addFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" || invalidSymbol(symbol) {
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

	rule, err := loadUserRule(ctx, tx, selectMemberLevelForUpdate, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}
	if groupNo > rule.Groups {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	item := favoriteGroupItem{GroupNo: groupNo, Symbol: symbol}
	err = tx.QueryRow(ctx,
		`SELECT s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid AND f.group_no = $2 AND f.symbol = $3`,
		user.ID, groupNo, symbol).Scan(&item.Name, &item.CreatedTime)
	if err == nil {
		c.JSON(http.StatusOK, item)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		favoritesServerError(c, fmt.Errorf("favorites: find existing: %w", err))
		return
	}

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

	var count int
	err = tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1::uuid AND group_no = $2",
		user.ID, groupNo).Scan(&count)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: count: %w", err))
		return
	}
	if count >= rule.StocksPerGroup {
		c.JSON(http.StatusConflict, gin.H{"error": "favorite group full", "limit": rule.StocksPerGroup})
		return
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO user_favorite_stocks (user_id, group_no, symbol)
		VALUES ($1::uuid, $2, $3) RETURNING created_time`,
		user.ID, groupNo, symbol).Scan(&item.CreatedTime)
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

Run: `gofmt -w routers && go vet ./routers/ && go test ./routers/ -run 'Favorite|GroupName' -v -count=1 2>&1 | grep -E '^--- |^(ok|FAIL)|SKIP'`
Expected：全部 PASS（沒有已停用股票時，`IncludesDisabledStock` / `DisabledStock` 會 SKIP）。

- [ ] **Step 5: 確認同時送出的測試抓得到問題（mutation check），再用 race detector 跑**

暫時拿掉鎖，確認測試會失敗，然後還原：

```bash
cp routers/favorites.go routers/favorites.go.orig
sed -i '' 's/selectMemberLevelForUpdate = selectMemberLevel + " FOR UPDATE"/selectMemberLevelForUpdate = selectMemberLevel/' routers/favorites.go
go test ./routers/ -run 'ConcurrentRequestsRespectGroupLimit' -count=1 2>&1 | grep -E 'expected|actual|^(ok|FAIL)' | head -8
mv routers/favorites.go.orig routers/favorites.go
git diff --stat routers/favorites.go
```

Expected：拿掉鎖時 FAIL（`201` 的數量大於 `n`）；還原後 `git diff` 只顯示 Step 3 的改動（還沒 commit）。如果拿掉鎖仍然 PASS，代表測試抓不到 race，要回報，不能繼續。

接著：`go test ./routers/ -run 'ConcurrentRequestsRespectGroupLimit' -race -count=3 -v 2>&1 | grep -E '^--- |DATA RACE|^(ok|FAIL)'`
Expected：3 次都 PASS，沒有 `DATA RACE`。

確認測試資料已清除（同 Task 2 Step 6 的 psql 查詢）→ `0|0`。

- [ ] **Step 6: Commit**

```bash
git add routers/favorites.go routers/favorites_test.go
git commit -m "feat: 新增 POST /api/favorites/groups/:group_no，以 FOR UPDATE 保證群組不超過上限"
```

---

### Task 4: 全部測試、端對端測試與文件

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`
- Rewrite: `docs/frontend-favorites-integration.md`
- Scratchpad（不加進 repo）：`<scratchpad>/sign_jwt.py`、`<scratchpad>/e2e_favorite_groups.sh`（`<scratchpad>` 為本 session 的 scratchpad 目錄）

**Interfaces:**
- Consumes：Task 2、3 完成的三支 API。
- Produces：無（文件與驗證）。

- [ ] **Step 1: Build、vet、跑全部測試**

Run: `go build ./... && go vet ./... && go test ./... -count=1`
Expected：沒有 build / vet 輸出；`main/auth`、`main/routers` 都是 `ok`。

- [ ] **Step 2: 端對端測試（真的 JWT、本機 server）**

建立 `<scratchpad>/sign_jwt.py`：

```python
# Signs an HS256 JWT the way AccountService does (sub, iat, exp). Usage: sign_jwt.py <sub> <ttl_seconds>
import base64, hashlib, hmac, json, os, sys, time
def b64(b): return base64.urlsafe_b64encode(b).rstrip(b"=").decode()
sub, ttl = sys.argv[1], int(sys.argv[2])
now = int(time.time())
h = b64(json.dumps({"alg": "HS256", "typ": "JWT"}, separators=(",", ":")).encode())
p = b64(json.dumps({"sub": sub, "iat": now, "exp": now + ttl}, separators=(",", ":")).encode())
sig = b64(hmac.new(os.environ["JWT_SECRET"].encode(), f"{h}.{p}".encode(), hashlib.sha256).digest())
print(f"{h}.{p}.{sig}")
```

建立 `<scratchpad>/e2e_favorite_groups.sh`（`chmod +x`）：

```bash
#!/bin/bash
# End-to-end test of the grouped /api/favorites through the real server with a real JWT.
set -u
SP=$(cd "$(dirname "$0")" && pwd)
REPO=/Users/mussina/Repo/TWStockAPI
cd "$REPO"
set -a; source .env; set +a
BASE=http://localhost:18080
PASS=0; FAIL=0
q() { psql "$DATABASE_URL" -Atc "$1"; }

USER_ID=$(q "INSERT INTO users (google_sub, email, name) VALUES ('test-favorites-e2e-$(date +%s)', 'favorites-e2e@example.com', 'favorites e2e') RETURNING id" | head -1)
cleanup() {
  pkill -f "$SP/server" 2>/dev/null
  q "DELETE FROM users WHERE id = '$USER_ID'" >/dev/null
  echo "cleanup: leftover test users=$(q "SELECT count(*) FROM users WHERE google_sub LIKE 'test-favorites-%'"), favorites rows=$(q "SELECT count(*) FROM user_favorite_stocks WHERE user_id = '$USER_ID'")"
}
trap cleanup EXIT

TOKEN=$(python3 "$SP/sign_jwt.py" "$USER_ID" 3600)
EXPIRED=$(python3 "$SP/sign_jwt.py" "$USER_ID" -3600)
GHOST=$(python3 "$SP/sign_jwt.py" "00000000-0000-0000-0000-000000000000" 3600)
SYMS=($(q "SELECT symbol FROM stocks WHERE enabled ORDER BY symbol LIMIT 6"))
DISABLED=$(q "SELECT symbol FROM stocks WHERE NOT enabled ORDER BY symbol LIMIT 1")
echo "flag JWT_TOKEN_ENABLE=$(q "SELECT value FROM config WHERE key='JWT_TOKEN_ENABLE'"); rule=$(q "SELECT value->'0' FROM user_config WHERE key='FAVORITE_GROUPS'")"

go build -o "$SP/server" . || exit 1
PORT=18080 "$SP/server" > "$SP/server.log" 2>&1 &
for i in $(seq 1 40); do curl -s "$BASE/health" >/dev/null && break; sleep 0.25; done

# check <name> <expected_code> <expected_body_substring or ""> <curl args...>
check() {
  local name=$1 want=$2 needle=$3; shift 3
  local out code body ok=1
  out=$(curl -s -w '\n%{http_code}' "$@")
  code=${out##*$'\n'}; body=${out%$'\n'*}
  [ "$code" = "$want" ] || ok=0
  [ -z "$needle" ] || [[ "$body" == *"$needle"* ]] || ok=0
  if [ $ok = 1 ]; then PASS=$((PASS+1)); r=PASS; else FAIL=$((FAIL+1)); r=FAIL; fi
  printf '%-4s %-44s want %s got %s  %s\n' "$r" "$name" "$want" "$code" "$(echo "$body" | cut -c1-140)"
}
AUTH=(-H "Authorization: Bearer $TOKEN")
JSON=(-H "Content-Type: application/json")
G=$BASE/api/favorites/groups
post() { echo "{\"symbol\":\"$1\"}"; }

check "GET no token"                     401 '"unauthorized"'   "$BASE/api/favorites"
check "GET expired token"                401 '"token expired"'  -H "Authorization: Bearer $EXPIRED" "$BASE/api/favorites"
check "GET token for nonexistent user"   401 '"unauthorized"'   -H "Authorization: Bearer $GHOST" "$BASE/api/favorites"
check "GET 5 empty groups"               200 '"name":"群組五","count":0,"items":[]' "${AUTH[@]}" "$BASE/api/favorites"
check "POST g1 ${SYMS[0]}"               201 '"group_no":1'     "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[0]})" "$G/1"
check "POST g1 ${SYMS[0]} again"         200 '"group_no":1'     "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[0]})" "$G/1"
check "POST g2 ${SYMS[0]} (same stock)"  201 '"group_no":2'     "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[0]})" "$G/2"
check "POST g1 ' ${SYMS[1]} ' (trimmed)" 201 "\"symbol\":\"${SYMS[1]}\"" "${AUTH[@]}" "${JSON[@]}" -d "{\"symbol\":\" ${SYMS[1]} \"}" "$G/1"
check "GET g1 has 2, g2 has 1"           200 '"group_no":2,"name":"群組二","count":1' "${AUTH[@]}" "$BASE/api/favorites"
check "POST g1 unknown symbol"           404 '"stock not found"' "${AUTH[@]}" "${JSON[@]}" -d '{"symbol":"__NOPE__"}' "$G/1"
check "POST g1 disabled $DISABLED"       404 '"stock not found"' "${AUTH[@]}" "${JSON[@]}" -d "$(post $DISABLED)" "$G/1"
check "POST g6 (no such group)"          404 '"group not found"' "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[2]})" "$G/6"
check "POST g0"                          400 '"invalid request"' "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[2]})" "$G/0"
check "POST invalid json"                400 '"invalid request"' "${AUTH[@]}" "${JSON[@]}" -d '{' "$G/1"
check "POST nul symbol"                  400 '"invalid request"' "${AUTH[@]}" "${JSON[@]}" -d '{"symbol":"\u0000"}' "$G/1"
check "POST no token"                    401 '"unauthorized"'   "${JSON[@]}" -d "$(post ${SYMS[2]})" "$G/1"
for i in 2 3 4; do
  curl -s -o /dev/null -X POST "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[$i]})" "$G/1"
done
check "POST g1 6th -> group full"        409 '"favorite group full","limit":5' "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[5]})" "$G/1"
check "POST g1 existing while full"      200 "\"symbol\":\"${SYMS[0]}\"" "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[0]})" "$G/1"
check "POST g3 6th symbol"               201 '"group_no":3'     "${AUTH[@]}" "${JSON[@]}" -d "$(post ${SYMS[5]})" "$G/3"
check "DELETE g1 ${SYMS[0]}"             204 ''                 -X DELETE "${AUTH[@]}" "$G/1/${SYMS[0]}"
check "DELETE g1 ${SYMS[0]} again"       204 ''                 -X DELETE "${AUTH[@]}" "$G/1/${SYMS[0]}"
check "GET g2 still has ${SYMS[0]}"      200 "\"group_no\":2,\"name\":\"群組二\",\"count\":1,\"items\":[{\"symbol\":\"${SYMS[0]}\"" "${AUTH[@]}" "$BASE/api/favorites"
check "DELETE g6 (no such group)"        404 '"group not found"' -X DELETE "${AUTH[@]}" "$G/6/${SYMS[0]}"
check "DELETE g1 %00"                    204 ''                 -X DELETE "${AUTH[@]}" "$G/1/%00"
check "DELETE g1 %FF"                    204 ''                 -X DELETE "${AUTH[@]}" "$G/1/%FF"
check "DELETE no token"                  401 '"unauthorized"'   -X DELETE "$G/1/${SYMS[1]}"
check "CORS preflight POST"              204 ''                 -X OPTIONS -H 'Origin: https://darvishkzone.com' -H 'Access-Control-Request-Method: POST' -H 'Access-Control-Request-Headers: authorization,content-type' "$G/1"
check "CORS preflight DELETE"            204 ''                 -X OPTIONS -H 'Origin: https://darvishkzone.com' -H 'Access-Control-Request-Method: DELETE' -H 'Access-Control-Request-Headers: authorization' "$G/1/${SYMS[1]}"

echo "RESULT: $PASS passed, $FAIL failed"
echo "server log errors: $(grep -c '"level":"ERROR"' "$SP/server.log")"
```

Run: `<scratchpad>/e2e_favorite_groups.sh`
Expected：`RESULT: 28 passed, 0 failed`；`server log errors: 0`；`cleanup: leftover test users=0, favorites rows=0`。（末尾的 `Terminated: 15` 是關掉 server 的訊息，屬正常。）

- [ ] **Step 3: 更新 `CLAUDE.md`**

1. `db/*.sql` 那一行：

```
`user_favorites.sql`（`user_config`、`user_favorite_stocks` 與 `FAVORITE_STOCKS_LIMIT` 初始值）。
```
改成
```
`user_favorites.sql`（`user_config`、`user_favorite_stocks` 與 `FAVORITE_GROUPS` 初始值）、`user_favorites_v2_migration.sql`（v1 單一清單 → v2 群組；會刪除所有我的最愛資料，只在套用過 v1 的環境跑一次）。
```

2. `routers/` 清單中以 `  - `favorites.go` — ` 開頭的那一行整行換成：

```
  - `favorites.go` / `favorites_config.go` — `/api/favorites` — 使用者的我的最愛（`user_favorite_stocks`），一律需要登入。分成固定數量的群組（群組一、群組二……；`group_no` 從 1 開始，名稱由 `groupName` 產生；不建群組表），同一檔可以放在多個群組。群組數與每組上限依 `users.member_level`，設定在 `user_config` 的 `FAVORITE_GROUPS`（JSONB，例如 `{"0":{"groups":5,"stocks_per_group":5}}`；找不到 level 或格式錯誤回 500）。`GET /api/favorites` 回傳 `{max_groups, max_stocks_per_group, groups[{group_no,name,count,items[{symbol,name,created_time}]}]}`（包括空的群組；群組內 `created_time DESC`）；`POST /api/favorites/groups/:group_no` `{"symbol"}`：201 新增、200 已存在、400 格式錯誤、404 `group not found` / `stock not found`、409 `favorite group full`；`DELETE /api/favorites/groups/:group_no/:symbol` 回 204（群組不存在回 404）。降級時超過目前群組數的資料保留但不顯示。POST 在 transaction 裡用 `SELECT ... FOR UPDATE` 鎖住 user 的 row。
```

- [ ] **Step 4: 更新 `README.md` 的 Favorites 段落**

把 `README.md` 從 `### Favorites — 我的最愛` 開始、到下一個 `---\n\n### Trade Records` 之前（不含）的內容整段換成：

````markdown
### Favorites — 我的最愛

**一律需要登入**：不論 `JWT_TOKEN_ENABLE` 為何，都必須帶 `Authorization: Bearer <token>`；驗證失敗的回應見[認證](#認證)。

我的最愛分成固定數量的群組（群組一、群組二……），群組數與每組上限依 `users.member_level` 而定；同一檔股票可以放在多個群組。

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/favorites` | 查詢自己所有的群組與群組底下的個股 |
| POST | `/api/favorites/groups/:group_no` | 把一檔股票加入指定群組 |
| DELETE | `/api/favorites/groups/:group_no/:symbol` | 從指定群組移除一檔股票 |

規則設定在 `user_config` 表的 `FAVORITE_GROUPS`（JSONB，key 為 member_level 字串；`is_og_member` 不影響）。修改規則：

```sql
UPDATE user_config
SET value = '{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}',
    updated_time = now()
WHERE key = 'FAVORITE_GROUPS';
```

`groups` 必須在 1–99、`stocks_per_group` ≥ 0。找不到設定、找不到該 level 或格式錯誤時，三支 API 都回 `500`。會員等級調降時，超過新群組數的資料會保留但不顯示；群組內超過上限時照樣列出，但不能再加入。

**資料表：** 全新環境執行 `db/user_favorites.sql`。套用過舊版（單一清單）的環境改執行一次 `db/user_favorites_v2_migration.sql`（**會刪除所有我的最愛資料**）。

前端串接方式見 [docs/frontend-favorites-integration.md](docs/frontend-favorites-integration.md)。

#### `GET /api/favorites`

**Response (200):**

```json
{
  "max_groups": 5,
  "max_stocks_per_group": 5,
  "groups": [
    {
      "group_no": 1,
      "name": "群組一",
      "count": 2,
      "items": [
        { "symbol": "2330", "name": "台積電", "created_time": "2026-10-12T09:12:00Z" },
        { "symbol": "2317", "name": "鴻海", "created_time": "2026-10-11T15:00:00Z" }
      ]
    },
    { "group_no": 2, "name": "群組二", "count": 0, "items": [] }
  ]
}
```

- `groups` 固定有 `max_groups` 個，依 `group_no` 由小到大；空的群組 `items` 為 `[]`。
- 群組內依 `created_time` 由新到舊排序；加入後被停用（`stocks.enabled = false`）的股票仍會列出。

#### `POST /api/favorites/groups/:group_no`

**Path Parameters:** `group_no` — 群組編號（從 1 開始）

**Request Body:**

```json
{ "symbol": "2330" }
```

`symbol` 會先去掉前後空白。只能加入 `stocks.enabled = true` 的股票。

| HTTP | Body | 意義 |
|------|------|------|
| `201` | `{"group_no":1,"symbol":"2330","name":"台積電","created_time":"..."}` | 新增成功 |
| `200` | 同上（既有的那筆） | 這個群組裡本來就有（即使群組已滿） |
| `400` | `{"error":"invalid request"}` | `group_no` 不是 ≥ 1 的整數、body 不是合法的 JSON，或 `symbol` 是空的 |
| `404` | `{"error":"group not found"}` | `group_no` 超過目前等級的群組數 |
| `404` | `{"error":"stock not found"}` | 股票不存在或未啟用 |
| `409` | `{"error":"favorite group full","limit":5}` | 群組已滿 |

#### `DELETE /api/favorites/groups/:group_no/:symbol`

從指定群組移除一檔股票（其他群組裡的同一檔不受影響）。不論原本是否存在，一律回 `204`（沒有 body）；`group_no` 不是 ≥ 1 的整數回 `400`，超過目前等級的群組數回 `404 {"error":"group not found"}`。
````

- [ ] **Step 5: 改寫 `docs/frontend-favorites-integration.md`**

整個檔案換成：

````markdown
# 前端串接：我的最愛群組（`/api/favorites`）

本文件給**前端**參考，說明如何串接 TWStockAPI 的「我的最愛」群組功能：取得所有群組、把股票加入群組、從群組移除。

- 登入與帶 token 的方式：見 [frontend-api-auth-integration.md](frontend-api-auth-integration.md)
- 完整 API 規格：見 TWStockAPI `README.md` 的 Favorites 段落

> **改版說明（2026-10-12）：** 我的最愛從「單一清單」改成「群組」。舊的 `POST /api/favorites`、`DELETE /api/favorites/:symbol` 已移除，GET 的回應格式也改了。舊版留下的資料已清除。從舊版改過來時要調整的地方見第 8 節。

---

## 1. 功能概覽

| 項目 | 說明 |
|------|------|
| 用途 | 使用者把個股分組收藏，方便快速選取 |
| 歸屬 | 每個使用者各自一份，只看得到、也只改得到自己的 |
| 登入 | **一律需要登入**：不論後端 `JWT_TOKEN_ENABLE` 開關怎麼設，都必須帶 `Authorization: Bearer <token>` |
| 群組 | 數量固定，依會員等級（`member_level`）而定；名稱固定為「群組一、群組二……」，**不能新增、刪除或改名** |
| 每組上限 | 依會員等級而定；**請一律用 GET 回傳的 `max_groups`、`max_stocks_per_group`，不要寫死在前端** |
| 重複 | 同一檔股票**可以放在多個群組**；同一個群組裡不會重複 |
| 排序 | 群組依編號排列；群組內依加入時間，最新加入的排最前面 |
| 可加入的股票 | 只能加入後端已啟用的股票；不存在或未啟用回 `404` |

目前的規則（僅供參考，後端可以隨時調整）：

| `member_level` | 群組數 | 每組上限 |
|----------------|--------|----------|
| `0` | 5（群組一～群組五） | 5 檔 |
| `1` | 10（群組一～群組十） | 10 檔 |

---

## 2. API 一覽

| Method | Path | 說明 |
|--------|------|------|
| `GET` | `/api/favorites` | 取得所有群組與群組底下的個股 |
| `POST` | `/api/favorites/groups/:group_no` | 把一檔股票加入指定群組 |
| `DELETE` | `/api/favorites/groups/:group_no/:symbol` | 從指定群組移除一檔股票 |

- `group_no` 是群組編號，**從 1 開始**（群組一 = `1`）。
- Base URL 與環境同 [frontend-api-auth-integration.md §2](frontend-api-auth-integration.md#2-環境)（staging：`https://twstockapi-staging.up.railway.app`）。
- 所有 request 都要帶 `Authorization: Bearer <token>`。

---

## 3. API 規格

### 3.1 `GET /api/favorites` — 取得所有群組

**Response `200`：**

```json
{
  "max_groups": 5,
  "max_stocks_per_group": 5,
  "groups": [
    {
      "group_no": 1,
      "name": "群組一",
      "count": 2,
      "items": [
        { "symbol": "2330", "name": "台積電", "created_time": "2026-10-12T09:12:00Z" },
        { "symbol": "2317", "name": "鴻海", "created_time": "2026-10-11T15:00:00Z" }
      ]
    },
    { "group_no": 2, "name": "群組二", "count": 0, "items": [] },
    { "group_no": 3, "name": "群組三", "count": 0, "items": [] },
    { "group_no": 4, "name": "群組四", "count": 0, "items": [] },
    { "group_no": 5, "name": "群組五", "count": 0, "items": [] }
  ]
}
```

| 欄位 | 型別 | 說明 |
|------|------|------|
| `max_groups` | number | 這個使用者的群組數 |
| `max_stocks_per_group` | number | 每個群組最多幾檔 |
| `groups` | array | **固定有 `max_groups` 個**，依 `group_no` 由小到大；空的群組也會列出 |
| `groups[].group_no` | number | 群組編號，從 1 開始；呼叫 POST / DELETE 時用這個值 |
| `groups[].name` | string | 群組名稱（「群組一」……），直接顯示即可 |
| `groups[].count` | number | 這個群組目前的數量（等於 `items.length`） |
| `groups[].items` | array | 群組內的個股，依 `created_time` 由新到舊；**空的群組是 `[]`，不會是 `null`** |
| `items[].symbol` | string | 股票代號 |
| `items[].name` | string | 股票名稱 |
| `items[].created_time` | string | 加入這個群組的時間，**含時區的 RFC 3339 字串**；可能是 `...Z` 或 `...+08:00`，請用 `new Date(created_time)` 解析 |

注意：

- 股票加入之後如果被後端停用，**仍然會出現在群組中**，可以正常移除。
- 會員等級被調降時，超過新群組數的群組不會出現（資料保留，升級回來會再出現）；群組內的數量可能大於 `max_stocks_per_group`（例如 `9/5`），這時可以移除，但不能再加入。

### 3.2 `POST /api/favorites/groups/:group_no` — 加入群組

**Request：**

```
POST /api/favorites/groups/1
Content-Type: application/json
Authorization: Bearer <token>

{ "symbol": "2330" }
```

- `symbol` 前後的空白會被去掉（`" 2330 "` 等同 `"2330"`）。
- 代號要完全一致（例如 `00631L` 不能寫成 `00631l`）；建議直接用 `/api/stocks` 或 GET 回傳的 `symbol`。

**Responses：**

| HTTP | Body | 意義 |
|------|------|------|
| `201` | `{"group_no":1,"symbol":"2330","name":"台積電","created_time":"..."}` | 加入成功，回傳新的那一筆 |
| `200` | `{"group_no":1,"symbol":"2330","name":"台積電","created_time":"..."}` | **這個群組裡本來就有**，回傳既有的那一筆（即使群組已滿也一樣）。前端當作成功處理即可 |
| `400` | `{"error":"invalid request"}` | `group_no` 不是 ≥ 1 的整數；body 不是合法的 JSON、沒有 `symbol`、`symbol` 不是字串，或去掉空白後是空字串 |
| `404` | `{"error":"group not found"}` | `group_no` 超過這個使用者的群組數（例如 level 0 傳 `6`） |
| `404` | `{"error":"stock not found"}` | 股票不存在，或後端未啟用 |
| `409` | `{"error":"favorite group full","limit":5}` | 群組已滿；`limit` 是目前每組上限 |
| `401` | `{"error":"token expired"}` / `{"error":"unauthorized"}` | 未登入、token 過期或無效，處理方式同 [auth 文件 §5](frontend-api-auth-integration.md#5-錯誤回應與處理) |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤（例如規則設定有誤） |

- **重複送出是安全的**：同一個群組連點兩次，第二次回 `200`，不會產生重複資料。
- 同一檔加入**另一個**群組是新的一筆，回 `201`。
- 同時送出多個加入請求時，後端保證每個群組都不會超過上限，超過的那幾個回 `409`。
- 兩個 `404` 要看 `error` 區分：`group not found` 通常代表前端的群組資訊過期了（例如等級剛被調整），請重新 GET。

### 3.3 `DELETE /api/favorites/groups/:group_no/:symbol` — 從群組移除

```
DELETE /api/favorites/groups/1/2330
Authorization: Bearer <token>
```

| HTTP | Body | 意義 |
|------|------|------|
| `204` | （沒有 body） | 已移除；**本來就不在這個群組也回 `204`** |
| `400` | `{"error":"invalid request"}` | `group_no` 不是 ≥ 1 的整數 |
| `404` | `{"error":"group not found"}` | `group_no` 超過這個使用者的群組數 |
| `401` | 同上 | 未登入、token 過期或無效 |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 |

- **只移除這個群組的那一筆**，其他群組裡的同一檔不受影響。
- 重複送出是安全的，不會出錯。
- `symbol` 放在 URL path，請用 `encodeURIComponent(symbol)` 編碼。
- **`204` 沒有 body，不要呼叫 `res.json()`**（見第 4 節）。

---

## 4. 程式範例

### 4.1 `apiFetch`（fetch 版）

需要能處理 `204`（沒有 body）並保留錯誤 body（`409` 的 `limit`、`404` 的 `error`）。如果你們已經照上一版文件改過 `apiFetch`，可以直接沿用：

```js
const API_BASE = "https://twstockapi-staging.up.railway.app";

// 401 時丟出；reason 為 "token expired" 或 "unauthorized"
export class AuthError extends Error {
  constructor(reason) {
    super(reason);
    this.name = "AuthError";
    this.reason = reason;
  }
}

// 其他非 2xx 時丟出；status 為 HTTP 狀態碼，body 為解析後的 JSON（可能是 null）
export class ApiError extends Error {
  constructor(status, body) {
    super(body?.error ?? body?.detail ?? `TWStockAPI ${status}`);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

export async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers);
  const token = localStorage.getItem("token");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }
  if (options.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });

  if (res.status === 204) {
    return null;
  }
  const body = await res.json().catch(() => null);

  if (res.status === 401) {
    localStorage.removeItem("token");
    throw new AuthError(body?.error ?? "unauthorized");
  }
  if (!res.ok) {
    throw new ApiError(res.status, body);
  }
  return body;
}
```

### 4.2 我的最愛 API 封裝

```js
// favorites.js
import { apiFetch } from "./api";

/** @returns {Promise<FavoriteGroups>} */
export function listFavoriteGroups() {
  return apiFetch("/api/favorites");
}

/**
 * 把一檔股票加入群組。201（新加入）與 200（本來就在這個群組）都會回傳該筆資料。
 * @returns {Promise<FavoriteGroupItem>}
 */
export function addToGroup(groupNo, symbol) {
  return apiFetch(`/api/favorites/groups/${groupNo}`, {
    method: "POST",
    body: JSON.stringify({ symbol }),
  });
}

/** 從群組移除一檔股票；本來就不在這個群組也算成功。 */
export async function removeFromGroup(groupNo, symbol) {
  await apiFetch(`/api/favorites/groups/${groupNo}/${encodeURIComponent(symbol)}`, {
    method: "DELETE",
  });
}
```

TypeScript 型別（參考）：

```ts
export interface FavoriteItem {
  symbol: string;
  name: string;
  created_time: string; // RFC 3339
}

export interface FavoriteGroup {
  group_no: number; // 從 1 開始
  name: string; // "群組一"……
  count: number;
  items: FavoriteItem[];
}

export interface FavoriteGroups {
  max_groups: number;
  max_stocks_per_group: number;
  groups: FavoriteGroup[];
}

export interface FavoriteGroupItem extends FavoriteItem {
  group_no: number;
}

export type FavoriteError =
  | { error: "invalid request" }
  | { error: "group not found" }
  | { error: "stock not found" }
  | { error: "favorite group full"; limit: number };
```

### 4.3 使用範例（與框架無關）

```js
import { AuthError, ApiError } from "./api";
import { listFavoriteGroups, addToGroup, removeFromGroup } from "./favorites";

const state = { maxGroups: 0, maxPerGroup: 0, groups: [] };

// 頁面載入、登入後：取一次所有群組
export async function loadFavorites() {
  const data = await listFavoriteGroups();
  state.maxGroups = data.max_groups;
  state.maxPerGroup = data.max_stocks_per_group;
  state.groups = data.groups;
}

function groupOf(groupNo) {
  return state.groups.find((g) => g.group_no === groupNo);
}

// 這檔股票在哪些群組裡（例如畫星號、或在選單中打勾）
export function groupsContaining(symbol) {
  return state.groups
    .filter((g) => g.items.some((i) => i.symbol === symbol))
    .map((g) => g.group_no);
}

export function isGroupFull(groupNo) {
  const g = groupOf(groupNo);
  return !g || g.items.length >= state.maxPerGroup;
}

export async function onAdd(groupNo, symbol) {
  try {
    const { group_no, ...item } = await addToGroup(groupNo, symbol);
    const g = groupOf(group_no);
    // 201 或 200 都放到最前面；先過濾掉同代號，避免 200 時重複
    g.items = [item, ...g.items.filter((i) => i.symbol !== item.symbol)];
    g.count = g.items.length;
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      state.maxPerGroup = err.body.limit; // 以後端回傳的上限為準
      toast(`${groupOf(groupNo)?.name ?? "這個群組"}已滿（每組 ${err.body.limit} 檔）`);
    } else if (err instanceof ApiError && err.status === 404 && err.body?.error === "group not found") {
      await loadFavorites(); // 群組資訊過期（例如等級剛被調整）
      toast("群組已變更，請重新選擇");
    } else if (err instanceof ApiError && err.status === 404) {
      toast("這檔股票無法加入我的最愛");
    } else if (err instanceof AuthError) {
      redirectToLogin(); // 你們自己的導回登入頁邏輯
    } else {
      toast("加入失敗，請稍後再試");
    }
  }
}

export async function onRemove(groupNo, symbol) {
  const g = groupOf(groupNo);
  const before = g.items;
  g.items = g.items.filter((i) => i.symbol !== symbol); // 樂觀更新
  g.count = g.items.length;
  try {
    await removeFromGroup(groupNo, symbol);
  } catch (err) {
    g.items = before; // 失敗時還原
    g.count = before.length;
    if (err instanceof AuthError) {
      redirectToLogin();
    } else if (err instanceof ApiError && err.status === 404) {
      await loadFavorites();
    } else {
      toast("移除失敗，請稍後再試");
    }
  }
}

// 把股票從一個群組移到另一個群組：先加入新群組，成功後再從舊群組移除
export async function onMove(fromGroupNo, toGroupNo, symbol) {
  await onAdd(toGroupNo, symbol);
  if (groupOf(toGroupNo)?.items.some((i) => i.symbol === symbol)) {
    await onRemove(fromGroupNo, symbol);
  }
}
```

### 4.4 axios 版

沿用 [auth 文件 §4.2](frontend-api-auth-integration.md#42-axios) 的 `api` instance（已經會自動帶 token、統一處理 401）：

```js
import { api } from "./api";

export const listFavoriteGroups = () => api.get("/api/favorites").then((r) => r.data);

export const addToGroup = (groupNo, symbol) =>
  api.post(`/api/favorites/groups/${groupNo}`, { symbol }).then((r) => r.data); // 201 或 200

export const removeFromGroup = (groupNo, symbol) =>
  api.delete(`/api/favorites/groups/${groupNo}/${encodeURIComponent(symbol)}`); // 204，r.data 為 ""

// 錯誤處理
try {
  await addToGroup(1, "2330");
} catch (err) {
  const { status, data } = err.response ?? {};
  if (status === 409) toast(`群組已滿（每組 ${data.limit} 檔）`);
  else if (status === 404 && data?.error === "group not found") await reloadFavorites();
  else if (status === 404) toast("這檔股票無法加入我的最愛");
  else if (status !== 401) toast("加入失敗，請稍後再試"); // 401 已由 interceptor 處理
}
```

axios 會自動帶 `Content-Type: application/json`，`204` 也不會解析失敗，不需要額外調整。

---

## 5. UI 建議

| 情境 | 建議做法 |
|------|----------|
| 未登入 | 不要呼叫 `/api/favorites`（一定回 `401`）；收藏按鈕改成「登入後使用」或點了導去登入 |
| 進入頁面 / 登入後 | 呼叫一次 `GET /api/favorites`，用結果畫出所有群組（包括空的） |
| 群組分頁 / 清單 | 直接用 `groups` 的順序與 `name`；不要自己產生「群組一」等名稱 |
| 加入股票 | 點收藏時讓使用者選群組；用 `groupsContaining(symbol)` 標出已經在哪些群組 |
| 顯示數量 | 每個群組顯示 `items.length` / `max_stocks_per_group`，例如「群組一 2 / 5」 |
| 群組已滿 | `items.length >= max_stocks_per_group` 時，選單裡該群組設成不可選並標示「已滿」；仍然可以移除 |
| 加入 / 移除進行中 | 把按鈕暫時設成不可按，避免連點（後端已保證連點不會出錯，這只是讓畫面比較穩定） |
| 加入成功 | 直接用回傳的那一筆更新該群組，不需要重新 GET |
| 收到 `409` | 用回傳的 `limit` 更新每組上限 |
| 收到 `404 group not found` | 重新 GET（群組數可能因等級調整而改變） |
| 移到其他群組 | 沒有「移動」API：先 POST 到新群組，成功後再 DELETE 舊群組（見 §4.3 `onMove`） |
| 被停用的股票 | 仍然會出現在群組中；點進去沒有行情資料屬正常現象，可以讓使用者移除 |
| 顯示時間 | `new Date(item.created_time).toLocaleString("zh-TW")` |

---

## 6. CORS

`GET`、`POST`、`DELETE` 都在允許清單內（見 [auth 文件 §6](frontend-api-auth-integration.md#6-cors)）。

- `POST` 帶 `Content-Type: application/json` 與 `Authorization`，`DELETE` 帶 `Authorization`，瀏覽器都會先送 `OPTIONS` preflight，後端回 `204` 屬正常現象。
- 本機開發要透過 dev server proxy（`localhost` 不在允許清單內）。

---

## 7. 測試方式

```bash
BASE=https://twstockapi-staging.up.railway.app
TOKEN=<token>

# 取得所有群組
curl -H "Authorization: Bearer $TOKEN" $BASE/api/favorites

# 把 2330 加入群組一
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"symbol":"2330"}' -w '\nHTTP %{http_code}\n' $BASE/api/favorites/groups/1

# 從群組一移除 2330
curl -X DELETE -H "Authorization: Bearer $TOKEN" -w 'HTTP %{http_code}\n' $BASE/api/favorites/groups/1/2330
```

取得測試用 token 的方式見 [auth 文件 §7](frontend-api-auth-integration.md#7-測試方式)。

---

## 8. 從單一清單版改過來

| 舊版 | 新版 |
|------|------|
| `GET /api/favorites` 回 `{limit, count, items}` | 回 `{max_groups, max_stocks_per_group, groups[{group_no, name, count, items}]}` |
| `POST /api/favorites` body `{"symbol"}` | `POST /api/favorites/groups/:group_no` body `{"symbol"}`；回應多了 `group_no` |
| `DELETE /api/favorites/:symbol` | `DELETE /api/favorites/groups/:group_no/:symbol`（只移除該群組的那一筆） |
| `409 {"error":"favorite limit reached","limit":10}` | `409 {"error":"favorite group full","limit":5}`（每組上限） |
| 一檔股票只會出現一次 | 同一檔可以出現在多個群組 |
| — | 新增 `404 {"error":"group not found"}`、`group_no` 格式錯誤的 `400` |

舊版留下的我的最愛資料已清除，使用者需要重新加入。`apiFetch` 的調整（處理 `204`、保留錯誤 body）與上一版相同。

---

## 9. 常見問題排查

| 症狀 | 可能原因 |
|------|----------|
| 其他 API 不帶 token 都正常，只有 `/api/favorites` 回 `401` | 正常：我的最愛**一律需要登入**，不受後端開關影響 |
| 呼叫舊的 `POST /api/favorites` 或 `DELETE /api/favorites/2330` 回 `404` | 舊 API 已移除，改用 `/api/favorites/groups/:group_no/...`（見第 8 節） |
| `DELETE` 成功了但前端拋出 JSON 解析錯誤 | `204` 沒有 body，不要呼叫 `res.json()`（見 §4.1） |
| `POST` 回 `400 invalid request` | `group_no` 傳了 `0`、`abc` 等不是 ≥ 1 的整數；body 沒有 `JSON.stringify`；`symbol` 送成數字（例如 `2330` 而不是 `"2330"`） |
| `POST` / `DELETE` 回 `404 group not found` | `group_no` 超過這個使用者的群組數（例如 level 0 傳 `6`），或前端的群組資訊過期 → 重新 GET |
| `POST` 回 `404 stock not found`，但股票確實存在 | 該股票後端未啟用，或代號大小寫不一致 |
| `POST` 回 `409`，但畫面上這個群組還沒滿 | 前端資料過期（例如在別的裝置加過）→ 重新 GET |
| 某個群組的數量大於每組上限 | 會員等級被調降，屬正常；可以移除，但不能再加入 |
| 等級調降後少了幾個群組 | 屬正常：超過新群組數的群組不會顯示，資料保留，升級後會再出現 |
| `500 Internal server error` | 後端設定或 DB 問題 → 通知後端 |
````

- [ ] **Step 6: 確認文件連結與內容**

Run:
```bash
grep -n "favorites/groups" README.md CLAUDE.md docs/frontend-favorites-integration.md | head -20
grep -n "FAVORITE_STOCKS_LIMIT\|favorite limit reached\|DELETE /api/favorites/:symbol" README.md CLAUDE.md docs/frontend-api-auth-integration.md
```
Expected：第一個指令三個檔案都有結果；第二個指令**沒有任何輸出**（舊設定、舊錯誤訊息與舊路徑都不再出現在文件中；`docs/frontend-favorites-integration.md` 第 8、9 節刻意提到舊版，不在檢查範圍內）。

- [ ] **Step 7: Commit**

```bash
git add CLAUDE.md README.md docs/frontend-favorites-integration.md
git commit -m "docs: 我的最愛群組版的 README、CLAUDE.md 與前端串接文件"
```
