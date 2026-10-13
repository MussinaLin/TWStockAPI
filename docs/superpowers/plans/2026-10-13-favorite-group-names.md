# 我的最愛群組自訂名稱 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增 `PUT /api/favorites/groups/:group_no` 讓 user 自訂群組名稱，`GET /api/favorites` 回傳自訂名稱（沒改過名的群組仍是「群組N」）。

**Architecture:** 新表 `user_favorite_groups(user_id, group_no, name)` 只存改過名的群組；群組本身維持虛擬。名稱檢查是純函式 `normalizeGroupName`（`routers/favorites_config.go`）。PUT 用 `INSERT ... ON CONFLICT DO UPDATE` 寫入，不開 transaction、不加鎖；GET 多查一次自訂名稱，取代預設名稱。

**Tech Stack:** Go 1.25、Gin v1.12、pgx/v5 v5.9.1、gin-contrib/cors v1.7.7、testify。

**Spec:** `docs/superpowers/specs/2026-10-13-favorite-group-names-design.md`

## Global Constraints

- Go module 名稱是 `main`（import path 例如 `main/db`、`main/auth`）。不新增任何依賴。
- 所有 commit 都接在 `feat/favorite-groups` branch 上（群組版還沒 merge，兩者一起 merge、一起部署）。
- API 只使用 `GET`、`POST`、`PUT`、`DELETE`，**不使用 `PATCH`**。新路徑：`PUT /api/favorites/groups/:group_no`，body `{"name": "..."}`。
- 名稱規則：`strings.TrimSpace` 去掉前後空白（包括全形空白 U+3000）；去掉後 1–10 個字元（`utf8.RuneCountInString`）；不含 `unicode.IsControl` 的字元；必須是合法的 UTF-8；中間的空白保留；不同群組可以重名；沒有還原預設名稱的方式。
- 回應 body 必須完全相同：
  - 200 `{"group_no":<int>,"name":"<去掉空白後的名稱>"}`（名稱跟原本一樣也回 200）
  - 400 `{"error":"invalid request"}`（所有 400 都是這個，不區分原因）
  - 404 `{"error":"group not found"}`
  - 401 `{"error":"unauthorized"}`
  - 500 `{"detail":"Internal server error"}`
- PUT 的檢查順序：`group_no` → body → 名稱 → 讀 user 的規則 → `group_no > rule.Groups` 回 404 → upsert。名稱不合法且群組不存在時回 400。
- PUT 寫入：`INSERT INTO user_favorite_groups (user_id, group_no, name) VALUES ($1::uuid, $2, $3) ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()`；不開 transaction、不鎖 user 的 row。
- GET 的回應格式不變；只查 `group_no <= max_groups` 的自訂名稱。降級時名稱保留在 DB 但不回傳，PUT 超過目前群組數回 404。
- 新表 DDL（加進 `db/user_favorites.sql`，可重複執行）：

```sql
CREATE TABLE IF NOT EXISTS user_favorite_groups (
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_no     SMALLINT    NOT NULL CHECK (group_no >= 1),
    name         VARCHAR(10) NOT NULL CHECK (name <> ''),
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_no)
);
```

- 在 staging（`.env` 的 DB）執行 `db/user_favorites.sql` **之前要先向使用者確認**。不要動 `config` 表的 `JWT_TOKEN_ENABLE`。不要印出 `JWT_SECRET` 或 token。
- CORS `AllowMethods` 變成 `[]string{"GET", "POST", "PUT", "DELETE"}`。
- 測試風格：testify `assert` / `require`、`httptest`、`gin.TestMode`。routers 的整合測試會寫入 `.env` 的 DB（使用者已同意），測試資料必須在 `t.Cleanup` 清除（刪除測試 user 時 `ON DELETE CASCADE` 會一起刪掉自訂名稱）。
- 改到 API 時要同步更新 `CLAUDE.md` 與 `README.md`；更新 `docs/frontend-favorites-integration.md`（使用者指定的交付物）與 `docs/frontend-api-auth-integration.md` 的 CORS 表格。
- Commit message 不加 `Co-Authored-By:` 或任何署名行。

## Review Focus

1. **同一個群組同時送出兩個 PUT**（連點儲存、兩個分頁）：兩個都回 200，DB 只有一列；不能因為 unique violation 回 500。由 Task 2 的 `TestRenameFavoriteGroup_ConcurrentRequests` 負責。
2. **4-byte 字元（emoji）剛好到上限**：10 個 `📈` 要回 200 且 DB 存得下（`VARCHAR(10)` 以字元計，不是 bytes）；11 個回 400。由 Task 2 的 `TestRenameFavoriteGroup_Emoji` 負責。
3. **名稱含 HTML 字元**（`<b>&"'</b>`）：原樣存下、原樣回傳；前端文件提醒當成純文字顯示。由 Task 2 的 `TestRenameFavoriteGroup_HTMLCharacters` 負責。
4. **`group_no` 很大或群組不存在，同時名稱不合法**：`/groups/99999` 帶合法名稱回 404（不能因為超出 SMALLINT 回 500）；`/groups/100` 帶空白名稱回 400。由 Task 2 的 `TestRenameFavoriteGroup_GroupNotFound` 與 `TestRenameFavoriteGroup_InvalidRequest`（`blank name on missing group`）負責。
5. **`group_no` 寫成 `+1`、`01`**：與 POST 一致，視為群組一。由 Task 2 的 `TestRenameFavoriteGroup_GroupNoVariants` 負責。

---

## File Structure

| File | 動作 | 職責 |
|---|---|---|
| `routers/favorites_config.go` | Modify | 新增 `maxGroupNameLen`、`normalizeGroupName` |
| `routers/favorites_config_test.go` | Modify | 新增 `TestNormalizeGroupName` |
| `db/user_favorites.sql` | Modify | 新增 `user_favorite_groups` |
| `routers/favorites.go` | Modify | 新增 PUT handler 與型別；GET 套用自訂名稱（`applyCustomGroupNames`） |
| `routers/favorites_rename_test.go` | Create | 自訂名稱的 fixture 與整合測試（PUT、GET） |
| `main.go` | Modify | CORS 加上 `PUT` |
| `CLAUDE.md`、`README.md`、`docs/frontend-favorites-integration.md`、`docs/frontend-api-auth-integration.md` | Modify | 文件同步 |
| `<scratchpad>/e2e_favorite_groups.sh`、`<scratchpad>/docjs/rename_check.mjs` | Modify / Create（不進 repo） | 端對端測試、前端文件範例的檢查 |

`<scratchpad>` = `/private/tmp/claude-501/-Users-mussina-Repo-TWStockAPI/a79a9cae-d31c-4627-a3c9-de8369024c3a/scratchpad`

---

### Task 1: 名稱檢查 `normalizeGroupName`

**Files:**
- Modify: `routers/favorites_config.go`
- Test: `routers/favorites_config_test.go`

**Interfaces:**
- Consumes: 無
- Produces:
  - `const maxGroupNameLen = 10`
  - `func normalizeGroupName(raw string) (string, bool)`：合法時回傳去掉前後空白後的名稱與 `true`；不合法時回傳 `""`, `false`

- [ ] **Step 1: Write the failing test**

在 `routers/favorites_config_test.go` 最後加上：

```go
func TestNormalizeGroupName(t *testing.T) {
	valid := map[string]string{
		"科技股":              "科技股",
		"  科技股  ":          "科技股",
		"　科技股　":  "科技股",
		"\t科技股\n":          "科技股",
		"科技 股":             "科技 股",
		"一二三四五六七八九十":     "一二三四五六七八九十",
		"abcdefghij":       "abcdefghij",
		"  一二三四五六七八九十  ": "一二三四五六七八九十",
		"群組三":              "群組三",
		"📈":                "📈",
	}
	for raw, want := range valid {
		got, ok := normalizeGroupName(raw)
		assert.True(t, ok, "normalizeGroupName(%q) should be valid", raw)
		assert.Equal(t, want, got, "normalizeGroupName(%q)", raw)
	}

	invalid := []string{
		"一二三四五六七八九十一",
		"abcdefghijk",
		"",
		"   ",
		"　",
		"科技\n股",
		"科技\t股",
		"科技\u0000股",
		"\xff",
	}
	for _, raw := range invalid {
		got, ok := normalizeGroupName(raw)
		assert.False(t, ok, "normalizeGroupName(%q) should be invalid", raw)
		assert.Equal(t, "", got, "normalizeGroupName(%q)", raw)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `gofmt -w routers && go test ./routers/ -run 'NormalizeGroupName' -v -count=1`
Expected: 編譯失敗，`undefined: normalizeGroupName`

- [ ] **Step 3: Write minimal implementation**

`routers/favorites_config.go` 的 import 改成：

```go
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)
```

在檔案最後（`groupName` 之後）加上：

```go
// maxGroupNameLen 是自訂群組名稱的最大字元數（以 Unicode code point 計），
// 與 user_favorite_groups.name 的 VARCHAR(10) 一致。
const maxGroupNameLen = 10

// normalizeGroupName 去掉名稱前後的空白（包括全形空白）後檢查：必須是合法的 UTF-8、
// 有 1 到 maxGroupNameLen 個字元、不含控制字元。合法時回傳去掉空白後的名稱與 true。
func normalizeGroupName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxGroupNameLen {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `gofmt -w routers && go vet ./routers/ && go test ./routers/ -run 'NormalizeGroupName|ParseFavoriteGroups|GroupName' -v -count=1`
Expected: `TestNormalizeGroupName`、`TestParseFavoriteGroups`、`TestParseFavoriteGroupsErrors`、`TestGroupName` 都 PASS；`ok  main/routers`

- [ ] **Step 5: Commit**

```bash
git add routers/favorites_config.go routers/favorites_config_test.go
git commit -m "feat: 新增自訂群組名稱的檢查 normalizeGroupName"
```

---

### Task 2: `user_favorite_groups` 與 `PUT /api/favorites/groups/:group_no`

**Files:**
- Modify: `db/user_favorites.sql`
- Modify: `routers/favorites.go`
- Create: `routers/favorites_rename_test.go`

**Interfaces:**
- Consumes:
  - Task 1：`normalizeGroupName(raw string) (string, bool)`
  - 既有（`routers/favorites.go`）：`loadUserRule(ctx, q querier, levelSQL, userID string) (groupRule, error)`、`selectMemberLevel`、`respondRuleError(c, err)`、`parseGroupNo(c) (int, bool)`、`favoritesServerError(c, err)`、`auth.CurrentUser(c) (auth.User, bool)`
  - 既有測試 fixture（`routers/favorites_test.go`）：`newFavoritesUser(t, level int) string`、`newFavoritesRouter(userID string) *gin.Engine`、`groupRuleFor(t, level int) groupRule`、`groupPath(groupNo int) string`、`doJSONBody(t, r, method, path, body string) (int, any)`；`routers/setup_test.go`：`doJSON(t, r, method, path string) (int, any)`
- Produces:
  - 資料表 `user_favorite_groups`（見 Global Constraints）
  - 路由 `PUT /api/favorites/groups/:group_no` → `renameFavoriteGroup(c *gin.Context)`
  - `type renameFavoriteGroupRequest struct { Name string \`json:"name"\` }`
  - `type favoriteGroupName struct { GroupNo int \`json:"group_no"\`; Name string \`json:"name"\` }`
  - 測試 helper（`routers/favorites_rename_test.go`）：`setGroupNameRow(t, userID string, groupNo int, name string)`、`storedGroupName(t, userID string, groupNo int) (string, bool)`、`groupNameRows(t, userID string) int`、`nameBody(t, name string) string`

- [ ] **Step 1: 更新 `db/user_favorites.sql`**

第一行註解改成：

```sql
-- 我的最愛（TWStockAPI 擁有）；可重複執行
```

`user_favorite_stocks` 上方的註解：

```sql
-- user 的我的最愛個股；group_no 從 1 開始，群組名稱由後端依 group_no 產生
```

改成：

```sql
-- user 的我的最愛個股；group_no 從 1 開始。預設群組名稱由後端依 group_no 產生，自訂名稱存在 user_favorite_groups
```

在檔案最後加上（前面空一行）：

```sql
-- user 自訂的群組名稱；只存改過名的群組，沒有資料的群組使用預設名稱（群組一、群組二……）
CREATE TABLE IF NOT EXISTS user_favorite_groups (
    user_id      UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_no     SMALLINT    NOT NULL CHECK (group_no >= 1),
    name         VARCHAR(10) NOT NULL CHECK (name <> ''),
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, group_no)
);
```

- [ ] **Step 2: 在 staging 建立新表（先向使用者確認）**

**STOP：先用 AskUserQuestion 向使用者確認**要在 `.env` 的 staging DB 執行 `db/user_favorites.sql`（只新增 `user_favorite_groups`，不刪資料）。使用者同意後才執行：

Run:

```bash
(set -a && . ./.env && set +a && \
 psql "$DATABASE_URL" -Atc "SELECT 'before', (SELECT count(*) FROM user_favorite_stocks)::text, (SELECT count(*) FROM user_config)::text" && \
 psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites.sql && \
 psql "$DATABASE_URL" -c '\d user_favorite_groups' && \
 psql "$DATABASE_URL" -Atc "SELECT 'after', (SELECT count(*) FROM user_favorite_stocks)::text, (SELECT count(*) FROM user_config)::text")
```

Expected：`CREATE TABLE`（既有的兩張表會出現 `already exists, skipping` 的 NOTICE）、`INSERT 0 0`；`\d` 顯示 5 個欄位（`user_id uuid`、`group_no smallint`、`name character varying(10)`、`created_time`、`updated_time`）、PK `(user_id, group_no)`、兩個 CHECK、FK 到 `users(id)` `ON DELETE CASCADE`；`before` 與 `after` 的數字相同。

- [ ] **Step 3: Write the failing tests**

建立 `routers/favorites_rename_test.go`：

```go
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

	"main/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Group name fixtures ──

// setGroupNameRow writes a custom group name directly, bypassing the PUT checks.
func setGroupNameRow(t *testing.T, userID string, groupNo int, name string) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		"INSERT INTO user_favorite_groups (user_id, group_no, name) VALUES ($1::uuid, $2, $3)",
		userID, groupNo, name)
	require.NoError(t, err)
}

// storedGroupName returns the stored custom name of a group and whether one exists.
func storedGroupName(t *testing.T, userID string, groupNo int) (string, bool) {
	t.Helper()
	var name string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT name FROM user_favorite_groups WHERE user_id = $1::uuid AND group_no = $2",
		userID, groupNo).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return name, true
}

func groupNameRows(t *testing.T, userID string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM user_favorite_groups WHERE user_id = $1::uuid", userID).Scan(&n)
	require.NoError(t, err)
	return n
}

// nameBody returns the PUT body {"name": name}.
func nameBody(t *testing.T, name string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"name": name})
	require.NoError(t, err)
	return string(b)
}

// ── PUT /api/favorites/groups/:group_no ──

func TestRenameFavoriteGroup_OK(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"科技股"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, map[string]any{"group_no": float64(1), "name": "科技股"}, body)
	name, ok := storedGroupName(t, userID, 1)
	require.True(t, ok)
	assert.Equal(t, "科技股", name)
}

func TestRenameFavoriteGroup_TrimsName(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"  科技股　"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, "科技股", body.(map[string]any)["name"])
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, "科技股", name)
}

func TestRenameFavoriteGroup_Overwrites(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, name := range []string{"科技股", "金融股", "金融股"} {
		status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, name))
		require.Equal(t, 200, status, name)
		assert.Equal(t, name, body.(map[string]any)["name"])
	}
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, "金融股", name)
	assert.Equal(t, 1, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_MaxLength(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	ten := "一二三四五六七八九十"
	status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, ten))
	require.Equal(t, 200, status, "10 個中文字應該存得下：%v", body)
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, ten, name)

	status, body = doJSONBody(t, r, "PUT", groupPath(2), nameBody(t, ten+"一"))
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "invalid request"}, body)
	_, ok := storedGroupName(t, userID, 2)
	assert.False(t, ok)
}

func TestRenameFavoriteGroup_Emoji(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	ten := strings.Repeat("📈", 10)
	status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, ten))
	require.Equal(t, 200, status, "10 個 emoji 應該存得下：%v", body)
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, ten, name)

	status, _ = doJSONBody(t, r, "PUT", groupPath(2), nameBody(t, ten+"📈"))
	assert.Equal(t, 400, status)
	_, ok := storedGroupName(t, userID, 2)
	assert.False(t, ok)
}

func TestRenameFavoriteGroup_HTMLCharacters(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	html := `<b>&"'</b>`

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), nameBody(t, html))
	require.Equal(t, 200, status)
	assert.Equal(t, html, body.(map[string]any)["name"])
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, html, name)
}

func TestRenameFavoriteGroup_InvalidRequest(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	cases := []struct{ name, path, body string }{
		{"group_no abc", "/api/favorites/groups/abc", `{"name":"科技股"}`},
		{"group_no zero", groupPath(0), `{"name":"科技股"}`},
		{"group_no negative", groupPath(-1), `{"name":"科技股"}`},
		{"invalid json", groupPath(1), `{"name":`},
		{"empty body", groupPath(1), ``},
		{"no name", groupPath(1), `{}`},
		{"number name", groupPath(1), `{"name":123}`},
		{"null name", groupPath(1), `{"name":null}`},
		{"empty name", groupPath(1), `{"name":""}`},
		{"blank name", groupPath(1), `{"name":"   "}`},
		{"newline inside", groupPath(1), `{"name":"科技\n股"}`},
		{"nul", groupPath(1), `{"name":"\u0000"}`},
		{"blank name on missing group", groupPath(maxFavoriteGroups + 1), `{"name":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "PUT", tc.path, tc.body)
			assert.Equal(t, 400, status)
			assert.Equal(t, map[string]any{"error": "invalid request"}, body)
		})
	}
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_GroupNoVariants(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, p := range []string{"+1", "01"} {
		status, body := doJSONBody(t, r, "PUT", "/api/favorites/groups/"+p, `{"name":"科技股"}`)
		require.Equal(t, 200, status, p)
		assert.Equal(t, float64(1), body.(map[string]any)["group_no"], p)
	}
	assert.Equal(t, 1, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_GroupNotFound(t *testing.T) {
	rule := groupRuleFor(t, 0)
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, groupNo := range []int{rule.Groups + 1, 99999} {
		status, body := doJSONBody(t, r, "PUT", groupPath(groupNo), `{"name":"科技股"}`)
		assert.Equal(t, 404, status, "group %d", groupNo)
		assert.Equal(t, map[string]any{"error": "group not found"}, body, "group %d", groupNo)
	}
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_Downgraded(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups >= 8 {
		t.Skipf("level 0 has %d groups; this test needs fewer than 8", rule.Groups)
	}
	userID := newFavoritesUser(t, 0)
	setGroupNameRow(t, userID, 8, "舊名稱")

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(8), `{"name":"新名稱"}`)
	assert.Equal(t, 404, status)
	assert.Equal(t, map[string]any{"error": "group not found"}, body)
	name, _ := storedGroupName(t, userID, 8)
	assert.Equal(t, "舊名稱", name)
}

func TestRenameFavoriteGroup_DefaultNameStored(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, _ := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"群組三"}`)
	require.Equal(t, 200, status)
	name, ok := storedGroupName(t, userID, 1)
	require.True(t, ok)
	assert.Equal(t, "群組三", name)
}

func TestRenameFavoriteGroup_DuplicateNamesAllowed(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, g := range []int{1, 2} {
		status, _ := doJSONBody(t, r, "PUT", groupPath(g), `{"name":"科技股"}`)
		assert.Equal(t, 200, status, "group %d", g)
	}
	assert.Equal(t, 2, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_OnlyOwn(t *testing.T) {
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	setGroupNameRow(t, other, 1, "別人的")

	status, _ := doJSONBody(t, newFavoritesRouter(owner), "PUT", groupPath(1), `{"name":"科技股"}`)
	require.Equal(t, 200, status)
	name, _ := storedGroupName(t, other, 1)
	assert.Equal(t, "別人的", name)
}

func TestRenameFavoriteGroup_ConcurrentRequests(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	const n = 10
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("PUT", groupPath(1), strings.NewReader(fmt.Sprintf(`{"name":"名稱%d"}`, i)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
		}()
	}
	wg.Wait()

	for i, code := range codes {
		assert.Equal(t, 200, code, "request %d", i)
	}
	assert.Equal(t, 1, groupNameRows(t, userID))
	name, _ := storedGroupName(t, userID, 1)
	assert.Regexp(t, `^名稱\d$`, name)
}

func TestRenameFavoriteGroup_UnknownLevel(t *testing.T) {
	userID := newFavoritesUser(t, 99)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"科技股"}`)
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_UserGone(t *testing.T) {
	status, body := doJSONBody(t, newFavoritesRouter(uuid.NewString()), "PUT", groupPath(1), `{"name":"科技股"}`)
	assert.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `gofmt -w routers && go test ./routers/ -run 'RenameFavoriteGroup' -count=1 2>&1 | grep -E '^\s*--- |^(ok|FAIL)' | head -40`
Expected: 所有 `TestRenameFavoriteGroup_*` 都 FAIL（路由還不存在，gin 回 `404 page not found`，`doJSONBody` 解碼失敗或狀態碼不符）；最後一行 `FAIL	main/routers`

- [ ] **Step 5: Write the implementation**

`routers/favorites.go` 的 `RegisterFavorites` 改成：

```go
// RegisterFavorites mounts /favorites behind requireLogin; every handler needs
// the authenticated user.
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.POST("/groups/:group_no", addFavorite)
	g.PUT("/groups/:group_no", renameFavoriteGroup)
	g.DELETE("/groups/:group_no/:symbol", deleteFavorite)
}
```

在 `favoriteGroupItem` 型別之後加上：

```go
type renameFavoriteGroupRequest struct {
	Name string `json:"name"`
}

// favoriteGroupName is the PUT response: a group and its custom name.
type favoriteGroupName struct {
	GroupNo int    `json:"group_no"`
	Name    string `json:"name"`
}
```

在 `deleteFavorite` 之後、`favoritesServerError` 之前加上：

```go
// renameFavoriteGroup 設定群組的自訂名稱；再改一次會取代原本的名稱。
// 改名沒有數量上限要維護，所以不開 transaction、不鎖 user 的 row，同時改名以後寫入的為準。
func renameFavoriteGroup(c *gin.Context) {
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
	var req renameFavoriteGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	name, ok := normalizeGroupName(req.Name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
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
		`INSERT INTO user_favorite_groups (user_id, group_no, name)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()`,
		user.ID, groupNo, name)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: rename group: %w", err))
		return
	}
	c.JSON(http.StatusOK, favoriteGroupName{GroupNo: groupNo, Name: name})
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `gofmt -w routers && go vet ./routers/ && go test ./routers/ -run 'RenameFavoriteGroup' -v -count=1 2>&1 | grep -E '^--- |^(ok|FAIL)|SKIP'`
Expected: 所有 `TestRenameFavoriteGroup_*` 都 PASS（`TestRenameFavoriteGroup_Downgraded` 只有在 level 0 群組數 ≥ 8 時才會 SKIP，目前是 5，不會 SKIP）；`ok  main/routers`

- [ ] **Step 7: 確認併發測試抓得到問題（mutation check）**

暫時把 upsert 的 `ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()` 這一行刪掉（變成單純的 INSERT）。

Run: `go test ./routers/ -run 'RenameFavoriteGroup_(ConcurrentRequests|Overwrites)' -count=1 2>&1 | grep -E '^\s*--- |^(ok|FAIL)'`
Expected: `TestRenameFavoriteGroup_ConcurrentRequests` 與 `TestRenameFavoriteGroup_Overwrites` 都 FAIL（第二次以後的 INSERT 因為 PK 衝突回 500）。

把那一行加回去，再跑：

Run: `go test ./routers/ -run 'RenameFavoriteGroup' -race -count=3 2>&1 | tail -3`
Expected: `ok  main/routers`；`git diff routers/favorites.go` 裡的 SQL 與 Step 5 相同。

- [ ] **Step 8: 跑全部我的最愛測試**

Run: `go test ./routers/ -run 'Favorite|GroupName' -count=1 2>&1 | tail -3`
Expected: `ok  main/routers`

- [ ] **Step 9: Commit**

```bash
git add db/user_favorites.sql routers/favorites.go routers/favorites_rename_test.go
git commit -m "feat: 新增 PUT /api/favorites/groups/:group_no 自訂群組名稱"
```

---

### Task 3: GET 回傳自訂名稱

**Files:**
- Modify: `routers/favorites.go`
- Test: `routers/favorites_rename_test.go`

**Interfaces:**
- Consumes:
  - Task 2：資料表 `user_favorite_groups`、PUT 路由、`setGroupNameRow`
  - 既有：`favoriteGroup`、`groupName(n int) string`、`addFavoriteRow(t, userID string, groupNo int, symbol string, created time.Time)`、`enabledSymbols(t, n int) []string`、`groupsOf(t, body any) []map[string]any`
- Produces:
  - `func applyCustomGroupNames(ctx context.Context, userID string, groups []favoriteGroup) error`

- [ ] **Step 1: Write the failing tests**

`routers/favorites_rename_test.go` 的 import 加上 `"time"`（放在 `"testing"` 之後）。檔案最後加上：

```go
// ── GET /api/favorites with custom names ──

func TestListFavorites_CustomNames(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups < 3 {
		t.Skipf("level 0 has %d groups; this test needs at least 3", rule.Groups)
	}
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, 1, symbols[0], time.Now())
	setGroupNameRow(t, userID, 1, "科技股")
	setGroupNameRow(t, userID, 3, "群組一")

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	for i, g := range groups {
		want := groupName(i + 1)
		switch i + 1 {
		case 1:
			want = "科技股"
		case 3:
			want = "群組一"
		}
		assert.Equal(t, want, g["name"], "group %d", i+1)
		assert.Equal(t, float64(i+1), g["group_no"])
	}
	assert.Equal(t, float64(1), groups[0]["count"])
	items := groups[0]["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, symbols[0], items[0].(map[string]any)["symbol"])
}

func TestListFavorites_RenamedViaPut(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups < 3 {
		t.Skipf("level 0 has %d groups; this test needs at least 3", rule.Groups)
	}
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, _ := doJSONBody(t, r, "PUT", groupPath(1), `{"name":"群組三"}`)
	require.Equal(t, 200, status)

	status, body := doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	assert.Equal(t, "群組三", groups[0]["name"])
	assert.Equal(t, groupName(2), groups[1]["name"])
	assert.Equal(t, "群組三", groups[2]["name"])
}

func TestListFavorites_CustomNamesOnlyOwn(t *testing.T) {
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	setGroupNameRow(t, owner, 1, "科技股")

	status, body := doJSON(t, newFavoritesRouter(other), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	assert.Equal(t, groupName(1), groupsOf(t, body)[0]["name"])
}

func TestListFavorites_CustomNameHiddenAfterDowngrade(t *testing.T) {
	rule0 := groupRuleFor(t, 0)
	rule1 := groupRuleFor(t, 1)
	if rule0.Groups >= 8 || rule1.Groups < 8 {
		t.Skipf("needs level 0 < 8 groups and level 1 >= 8 groups, got %d and %d", rule0.Groups, rule1.Groups)
	}
	userID := newFavoritesUser(t, 0)
	setGroupNameRow(t, userID, 8, "隱藏的")
	r := newFavoritesRouter(userID)

	status, body := doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule0.Groups)
	for _, g := range groups {
		assert.NotEqual(t, "隱藏的", g["name"])
	}

	_, err := db.Pool().Exec(context.Background(),
		"UPDATE users SET member_level = 1 WHERE id = $1::uuid", userID)
	require.NoError(t, err)

	status, body = doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups = groupsOf(t, body)
	require.Len(t, groups, rule1.Groups)
	assert.Equal(t, "隱藏的", groups[7]["name"])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `gofmt -w routers && go test ./routers/ -run 'ListFavorites_(CustomNames|RenamedViaPut|CustomNamesOnlyOwn|CustomNameHiddenAfterDowngrade)' -count=1 2>&1 | grep -E '^\s*--- |^(ok|FAIL)|expected|actual' | head -30`
Expected: `TestListFavorites_CustomNames`、`TestListFavorites_RenamedViaPut`、`TestListFavorites_CustomNameHiddenAfterDowngrade` FAIL（GET 還是回 `群組一` 等預設名稱）；`TestListFavorites_CustomNamesOnlyOwn` PASS（預設名稱本來就對）；最後一行 `FAIL	main/routers`

- [ ] **Step 3: Write the implementation**

`routers/favorites.go` 的 `listFavorites` 裡，建立預設群組的迴圈之後（`rows, err := db.Pool().Query(...)` 之前）加上：

```go
	if err := applyCustomGroupNames(ctx, user.ID, groups); err != nil {
		favoritesServerError(c, err)
		return
	}
```

`listFavorites` 的註解改成：

```go
// listFavorites 回傳目前等級的所有群組（包括空的群組）與群組底下的個股；有自訂名稱的
// 群組回傳自訂名稱。group_no 超過目前群組數的資料與名稱（降級後）保留在 DB，但不回傳。
```

在 `listFavorites` 之後加上：

```go
// applyCustomGroupNames 用 user_favorite_groups 的自訂名稱取代 groups 的預設名稱；
// 只查 groups 範圍內（group_no ≤ len(groups)）的群組。
func applyCustomGroupNames(ctx context.Context, userID string, groups []favoriteGroup) error {
	rows, err := db.Pool().Query(ctx,
		`SELECT group_no, name FROM user_favorite_groups
		WHERE user_id = $1::uuid AND group_no <= $2`, userID, len(groups))
	if err != nil {
		return fmt.Errorf("favorites: list group names: %w", err)
	}
	var groupNo int
	var name string
	_, err = pgx.ForEachRow(rows, []any{&groupNo, &name}, func() error {
		groups[groupNo-1].Name = name
		return nil
	})
	if err != nil {
		return fmt.Errorf("favorites: list group names: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `gofmt -w routers && go vet ./routers/ && go test ./routers/ -run 'Favorite|GroupName' -v -count=1 2>&1 | grep -E '^--- (FAIL|SKIP)|^(ok|FAIL)'`
Expected: 沒有 `--- FAIL`、沒有 `--- SKIP`；`ok  main/routers`

- [ ] **Step 5: Commit**

```bash
git add routers/favorites.go routers/favorites_rename_test.go
git commit -m "feat: GET /api/favorites 回傳自訂群組名稱"
```

---

### Task 4: CORS、端對端測試與文件

**Files:**
- Modify: `main.go:44`
- Modify: `CLAUDE.md`、`README.md`、`docs/frontend-favorites-integration.md`、`docs/frontend-api-auth-integration.md`
- Modify（不進 repo）：`<scratchpad>/e2e_favorite_groups.sh`
- Create（不進 repo）：`<scratchpad>/docjs/rename_check.mjs`

**Interfaces:**
- Consumes：Task 2 的 PUT 路由與回應；Task 3 的 GET 自訂名稱
- Produces：最終文件；CORS 允許 `PUT`

- [ ] **Step 1: 在 e2e script 加上 PUT 的檢查**

`<scratchpad>/e2e_favorite_groups.sh` 的 `cleanup` 裡的 `echo` 改成：

```bash
  echo "cleanup: leftover test users=$(q "SELECT count(*) FROM users WHERE google_sub LIKE 'test-favorites-%'"), favorites rows=$(q "SELECT count(*) FROM user_favorite_stocks WHERE user_id = '$USER_ID'"), group name rows=$(q "SELECT count(*) FROM user_favorite_groups WHERE user_id = '$USER_ID'")"
```

在 `check "CORS preflight DELETE" ...` 這一行之後、`echo "RESULT: ..."` 之前加上：

```bash
check "PUT g1 rename (trimmed)"          200 '{"group_no":1,"name":"科技股"}' "${AUTH[@]}" "${JSON[@]}" -X PUT -d '{"name":"  科技股  "}' "$G/1"
check "GET g1 shows custom name"         200 '"group_no":1,"name":"科技股"' "${AUTH[@]}" "$BASE/api/favorites"
check "GET g2 keeps default name"        200 '"group_no":2,"name":"群組二"' "${AUTH[@]}" "$BASE/api/favorites"
check "PUT g1 blank name"                400 '"invalid request"' "${AUTH[@]}" "${JSON[@]}" -X PUT -d '{"name":"   "}' "$G/1"
check "PUT g1 11 chars"                  400 '"invalid request"' "${AUTH[@]}" "${JSON[@]}" -X PUT -d '{"name":"一二三四五六七八九十一"}' "$G/1"
check "PUT g6 (no such group)"           404 '"group not found"' "${AUTH[@]}" "${JSON[@]}" -X PUT -d '{"name":"科技股"}' "$G/6"
check "PUT no token"                     401 '"unauthorized"'   "${JSON[@]}" -X PUT -d '{"name":"科技股"}' "$G/1"
check "CORS preflight PUT"               204 'Access-Control-Allow-Methods: GET,POST,PUT,DELETE' -D - -X OPTIONS -H 'Origin: https://darvishkzone.com' -H 'Access-Control-Request-Method: PUT' -H 'Access-Control-Request-Headers: authorization,content-type' "$G/1"
```

（`-D -` 把 response header 印到 stdout，`check` 會把它當成 body 比對。）

- [ ] **Step 2: Run e2e to verify the CORS check fails**

Run: `<scratchpad>/e2e_favorite_groups.sh 2>&1 | tail -14`
Expected: 新加的 PUT / GET 檢查都 PASS；`CORS preflight PUT` FAIL（header 是 `Access-Control-Allow-Methods: GET,POST,DELETE`）；`RESULT: 35 passed, 1 failed`；`server log errors: 0`；cleanup 那一行三個數字都是 0。

- [ ] **Step 3: CORS 加上 `PUT`**

`main.go`：

```go
		AllowMethods: []string{"GET", "POST", "DELETE"},
```

改成：

```go
		AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
```

- [ ] **Step 4: Run e2e to verify it passes**

Run: `gofmt -l . ; <scratchpad>/e2e_favorite_groups.sh 2>&1 | tail -6`
Expected: `gofmt -l` 沒有輸出；`RESULT: 36 passed, 0 failed`；`server log errors: 0`；`cleanup: leftover test users=0, favorites rows=0, group name rows=0`

- [ ] **Step 5: Commit**

```bash
git add main.go
git commit -m "feat: CORS 允許 PUT"
```

- [ ] **Step 6: 寫前端文件範例的檢查**

建立 `<scratchpad>/docjs/rename_check.mjs`：

```js
// Runs §4.3's normalizeGroupName / onRename from docs/frontend-favorites-integration.md
// against a fake API, using the same name table as the Go TestNormalizeGroupName.
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const doc = fs.readFileSync("/Users/mussina/Repo/TWStockAPI/docs/frontend-favorites-integration.md", "utf8");
const blocks = [...doc.matchAll(/```js\n([\s\S]*?)\n```/g)].map((m) => m[1]);
const usage = blocks.find((b) => b.includes("export async function onRename"));
if (!usage) {
  console.log("FAIL §4.3 has no onRename");
  console.log("RESULT: 0/1");
  process.exit(1);
}
const dir = path.dirname(new URL(import.meta.url).pathname);

const prelude = `
export class AuthError extends Error { constructor(r) { super(r); this.reason = r; } }
export class ApiError extends Error { constructor(s, b) { super(b?.error); this.status = s; this.body = b; } }
globalThis.toasts = []; const toast = (m) => globalThis.toasts.push(m); const redirectToLogin = () => {};
globalThis.calls = [];
async function listFavoriteGroups() {
  return { max_groups: 5, max_stocks_per_group: 5,
    groups: [1, 2, 3, 4, 5].map((n) => ({ group_no: n, name: "群組" + n, count: 0, items: [] })) };
}
async function addToGroup() { throw new Error("not used"); }
async function removeFromGroup() { throw new Error("not used"); }
async function renameGroup(groupNo, name) {
  globalThis.calls.push([groupNo, name]);
  const n = Number(groupNo);
  if (n > 5) throw new ApiError(404, { error: "group not found" });
  return { group_no: n, name };
}
`;
const strip = (code) => code.replace(/^import .*$/gm, "");
const file = path.join(dir, "rename.mjs");
fs.writeFileSync(file, prelude + strip(usage) + "\nexport { state };\n");
const m = await import(pathToFileURL(file).href + "?" + Date.now());

const results = [];
async function check(name, fn) {
  try { await fn(); results.push(["PASS", name]); } catch (e) { results.push(["FAIL", `${name} — ${e.message}`]); }
}
const assert = (c, msg) => { if (!c) throw new Error(msg); };

const valid = {
  "科技股": "科技股", "  科技股  ": "科技股", "　科技股　": "科技股", "\t科技股\n": "科技股",
  "科技 股": "科技 股", "一二三四五六七八九十": "一二三四五六七八九十", "abcdefghij": "abcdefghij",
  "  一二三四五六七八九十  ": "一二三四五六七八九十", "群組三": "群組三", "📈": "📈",
  [`${"📈".repeat(10)}`]: "📈".repeat(10),
};
const invalid = ["一二三四五六七八九十一", "abcdefghijk", "", "   ", "　", "科技\n股", "科技\t股", "科技\u0000股", "📈".repeat(11)];

await check("normalizeGroupName accepts what the backend accepts", () => {
  for (const [raw, want] of Object.entries(valid)) {
    const got = m.normalizeGroupName(raw);
    assert(got === want, `${JSON.stringify(raw)} → ${JSON.stringify(got)}`);
  }
});
await check("normalizeGroupName rejects what the backend rejects", () => {
  for (const raw of invalid) {
    const got = m.normalizeGroupName(raw);
    assert(got === null, `${JSON.stringify(raw)} → ${JSON.stringify(got)}`);
  }
});
await m.loadFavorites();
await check("onRename sends the trimmed name and updates state", async () => {
  const ok = await m.onRename("2", "  科技股 ");
  assert(ok === true, `returned ${ok}`);
  assert(globalThis.calls.at(-1)?.[1] === "科技股", `sent ${JSON.stringify(globalThis.calls.at(-1))}`);
  assert(m.state.groups[1].name === "科技股", `state name ${m.state.groups[1].name}`);
});
await check("onRename with an invalid name does not call the API", async () => {
  const before = globalThis.calls.length;
  const ok = await m.onRename(1, "   ");
  assert(ok === false && globalThis.calls.length === before, `ok=${ok} calls=${globalThis.calls.length - before}`);
});
await check("onRename on a vanished group reloads and tells the user", async () => {
  const ok = await m.onRename(6, "科技股");
  assert(ok === false, `returned ${ok}`);
  assert(globalThis.toasts.at(-1)?.includes("群組已變更"), `toast ${globalThis.toasts.at(-1)}`);
});

for (const [r, n] of results) console.log(r, n);
console.log(`RESULT: ${results.filter((r) => r[0] === "PASS").length}/${results.length}`);
```

- [ ] **Step 7: Run it to verify it fails**

Run: `node <scratchpad>/docjs/rename_check.mjs`
Expected: `FAIL §4.3 has no onRename`、`RESULT: 0/1`

- [ ] **Step 8: 更新 `docs/frontend-favorites-integration.md`**

依序用 Edit 修改（每一項的「原文」都是目前文件裡的完整字串）：

1. 第 3 行：`取得所有群組、把股票加入群組、從群組移除。` → `取得所有群組、把股票加入群組、從群組移除、幫群組改名。`

2. 第 8 行：`> **改版說明（2026-10-12）：** 我的最愛從「單一清單」改成「群組」。` → `> **改版說明（2026-10-13）：** 我的最愛從「單一清單」改成「群組」，群組可以自訂名稱。`（同一行後面的文字不變）

3. §1 表格的「群組」列：

```
| 群組 | 數量固定，依會員等級（`member_level`）而定；名稱固定為「群組一、群組二……」，**不能新增、刪除或改名** |
```

改成：

```
| 群組 | 數量固定，依會員等級（`member_level`）而定，**不能新增或刪除**；預設名稱為「群組一、群組二……」，**可以改名**（§3.4） |
```

4. §2 表格，在 `POST` 那一列之後加上：

```
| `PUT` | `/api/favorites/groups/:group_no` | 幫群組改名 |
```

5. §3.1 欄位表的 `groups[].name` 列：

```
| `groups[].name` | string | 群組名稱（「群組一」……），直接顯示即可 |
```

改成：

```
| `groups[].name` | string | 群組名稱：使用者改過名時為自訂名稱，否則為「群組一」……；直接顯示即可（請當成純文字顯示，見 §3.4） |
```

6. §3.1「注意」的第二點：`超過新群組數的群組不會出現（資料保留，升級回來會再出現）` → `超過新群組數的群組不會出現（資料與自訂名稱都保留，升級回來會再出現）`

7. 在 §3.3 的最後一點（提到「`204` 沒有 body，不要呼叫 `res.json()`」的那一行）之後、`---` 之前，加上（前面空一行）：

````
### 3.4 `PUT /api/favorites/groups/:group_no` — 幫群組改名

**Request：**

```
PUT /api/favorites/groups/1
Content-Type: application/json
Authorization: Bearer <token>

{ "name": "科技股" }
```

名稱規則（**前端請先檢查，見 §4.3 `normalizeGroupName`**）：

- 前後的空白會被去掉（包括全形空白）；去掉後**不能是空的**。
- 最多 **10 個字**，中文、英文、數字、標點都算 1 個字。以 Unicode 字元（code point）計：請用 `[...name].length`，不要用 `name.length`（部分 emoji 會被算成 2）。由多個字元組成的 emoji（例如 👨‍👩‍👧、國旗）會算成好幾個字。
- 不能含換行、tab 等控制字元。
- 不同群組可以取一樣的名字。
- **沒有「還原預設名稱」的 API**：想改回原名，就把名稱設成「群組一」等。

**Responses：**

| HTTP | Body | 意義 |
|------|------|------|
| `200` | `{"group_no":1,"name":"科技股"}` | 改名成功；`name` 是後端實際存下的名稱（去掉前後空白後）。名稱跟原本一樣也回 `200` |
| `400` | `{"error":"invalid request"}` | `group_no` 不是 ≥ 1 的整數；body 不是合法的 JSON、沒有 `name`、`name` 不是字串，或名稱不符合上述規則 |
| `404` | `{"error":"group not found"}` | `group_no` 超過這個使用者的群組數 → 重新 GET |
| `401` | 同 §3.2 | 未登入、token 過期或無效 |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 |

- 重複送出是安全的；同時送出多次時，以最後寫入的為準。
- 改名不影響群組裡的股票。
- 名稱是使用者輸入的內容，可能含 `<`、`&` 等字元，**請當成純文字顯示**（例如 React 的 `{name}`、`textContent`），不要用 `innerHTML`。
````

8. §4.2 的 `favorites.js` 區塊，在 `removeFromGroup` 函式之後（區塊結尾的 ```` ``` ```` 之前）加上：

```js

/**
 * 幫群組改名；回傳後端實際存下的名稱（去掉前後空白後）。
 * @returns {Promise<FavoriteGroupName>}
 */
export function renameGroup(groupNo, name) {
  return apiFetch(`/api/favorites/groups/${groupNo}`, {
    method: "PUT",
    body: JSON.stringify({ name }),
  });
}
```

9. §4.2 的 TypeScript 區塊：`  name: string; // "群組一"……` → `  name: string; // 自訂名稱，或預設的 "群組一"……`；在 `FavoriteGroupItem` interface 之後加上：

```ts

export interface FavoriteGroupName {
  group_no: number;
  name: string;
}
```

10. §4.3：import 那一行改成 `import { listFavoriteGroups, addToGroup, removeFromGroup, renameGroup } from "./favorites";`；在 `onMove` 函式之後（區塊結尾的 ```` ``` ```` 之前）加上：

```js

// 名稱規則與後端相同：去掉前後空白後 1–10 個字（以 code point 計），不含控制字元。
// 合法時回傳去掉空白後的名稱，不合法時回傳 null。
export const MAX_GROUP_NAME_LENGTH = 10;

export function normalizeGroupName(input) {
  const name = input.trim();
  const length = [...name].length;
  if (length === 0 || length > MAX_GROUP_NAME_LENGTH || /\p{Cc}/u.test(name)) {
    return null;
  }
  return name;
}

// 改名成功回傳 true；名稱不合法或失敗時顯示訊息並回傳 false（讓編輯框保持開啟）
export async function onRename(groupNo, input) {
  const name = normalizeGroupName(input);
  if (name === null) {
    toast(`群組名稱需為 1–${MAX_GROUP_NAME_LENGTH} 個字，且不能含換行`);
    return false;
  }
  try {
    const saved = await renameGroup(groupNo, name);
    const g = groupOf(saved.group_no);
    if (g) g.name = saved.name; // 用後端存下的名稱更新
    return true;
  } catch (err) {
    if (err instanceof AuthError) {
      redirectToLogin();
    } else if (err instanceof ApiError && err.status === 404) {
      await loadFavorites(); // 群組資訊過期（例如等級剛被調整）
      toast("群組已變更，請重新選擇");
    } else if (err instanceof ApiError && err.status === 400) {
      toast(`群組名稱需為 1–${MAX_GROUP_NAME_LENGTH} 個字，且不能含換行`);
    } else {
      toast("改名失敗，請稍後再試");
    }
    return false;
  }
}
```

11. §4.4 axios 區塊，在 `removeFromGroup` 那兩行之後加上：

```js

export const renameGroup = (groupNo, name) =>
  api.put(`/api/favorites/groups/${groupNo}`, { name }).then((r) => r.data); // 200 { group_no, name }
```

12. §5 表格：`| 群組分頁 / 清單 | 直接用 \`groups\` 的順序與 \`name\`；不要自己產生「群組一」等名稱 |` → 句尾加上 `（使用者可能改過名）`；在「移到其他群組」那一列之後加上：

```
| 改名 | 在群組名稱旁放編輯按鈕（或雙擊名稱）原地編輯；Enter 送出、Esc 取消；送出前用 `normalizeGroupName` 檢查（見 §4.3 `onRename`） |
| 名稱字數 | 顯示「`[...value].length` / 10」的計數。`<input maxlength>` 以 UTF-16 計算，部分 emoji 會被提早擋下，建議用計數提示加上送出前檢查 |
| 改名成功 | 用回傳的 `name` 更新畫面（後端會去掉前後空白），不需要重新 GET |
| 顯示群組名稱 | 一律當成純文字顯示，不要用 `innerHTML` |
```

13. §6：`` `GET`、`POST`、`DELETE` 都在允許清單內 `` → `` `GET`、`POST`、`PUT`、`DELETE` 都在允許清單內 ``；`` - `POST` 帶 `Content-Type: application/json` 與 `Authorization` `` → `` - `POST`、`PUT` 帶 `Content-Type: application/json` 與 `Authorization` ``（同一行後面的文字不變）。

14. §7：在 `# 從群組一移除 2330` 之前加上：

```bash
# 把群組一改名為「科技股」
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"科技股"}' -w '\nHTTP %{http_code}\n' $BASE/api/favorites/groups/1

```

15. §8 表格最後加上一列：

```
| 群組名稱固定 | 新增 `PUT /api/favorites/groups/:group_no` 幫群組改名（§3.4）；GET 的 `name` 可能是自訂名稱 |
```

16. §9 表格：`` | `POST` / `DELETE` 回 `404 group not found` | `` → `` | `POST` / `PUT` / `DELETE` 回 `404 group not found` | ``（同一行後面的文字不變）；在 `` | `POST` 回 `409`，但畫面上這個群組還沒滿 | `` 那一列之前加上：

```
| `PUT` 回 `400 invalid request` | 名稱去掉空白後是空的、超過 10 個字、含換行或 tab；body 沒有 `JSON.stringify`；`name` 送成數字或 `null` |
| 輸入框看起來不到 10 個字，`PUT` 卻回 `400` | 部分 emoji（例如 👨‍👩‍👧、國旗）由多個字元組成，後端會算成多個字 |
| 改名後想改回「群組一」 | 沒有還原 API；直接把名稱改成「群組一」 |
```

- [ ] **Step 9: Run the doc checks to verify they pass**

Run: `node <scratchpad>/docjs/rename_check.mjs && node <scratchpad>/docjs/harness.mjs 2>&1 | tail -6`
Expected: `rename_check.mjs` 五項都 PASS、`RESULT: 5/5`；`harness.mjs`（群組版的既有檢查）仍然全部 PASS、`RESULT: 4 passed, 0 failed`

- [ ] **Step 10: 更新 `docs/frontend-api-auth-integration.md`**

§6 CORS 表格：`` | Methods | `GET`、`POST`、`DELETE` | `` → `` | Methods | `GET`、`POST`、`PUT`、`DELETE` | ``

- [ ] **Step 11: 更新 `CLAUDE.md`**

1. `main.go` 那一點：`` CORS allowing `GET` / `POST` / `DELETE` and the `Authorization` header `` → `` CORS allowing `GET` / `POST` / `PUT` / `DELETE` and the `Authorization` header ``

2. `db/*.sql` 那一點：`` `user_favorites.sql`（`user_config`、`user_favorite_stocks` 與 `FAVORITE_GROUPS` 初始值；部署新版程式前先跑） `` → `` `user_favorites.sql`（`user_config`、`user_favorite_stocks`、`user_favorite_groups` 與 `FAVORITE_GROUPS` 初始值；部署新版程式前先跑；已套用群組版 schema 的環境重跑一次即可加上 `user_favorite_groups`） ``

3. `favorites.go` 那一點，依序替換三段：
   - `` 使用者的我的最愛（`user_favorite_stocks`），一律需要登入。分成固定數量的群組（群組一、群組二……；`group_no` 從 1 開始，名稱由 `groupName` 產生；不建群組表），同一檔可以放在多個群組。 `` → `` 使用者的我的最愛（`user_favorite_stocks`、`user_favorite_groups`），一律需要登入。分成固定數量的群組（`group_no` 從 1 開始；預設名稱「群組一、群組二……」由 `groupName` 產生；不建群組表，`user_favorite_groups` 只存改過名的群組），同一檔可以放在多個群組。 ``
   - `` （包括空的群組；群組內 `created_time DESC`）； `` → `` （包括空的群組；`name` 有自訂名稱時回傳自訂名稱；群組內 `created_time DESC`）； ``
   - `` 409 `favorite group full`；`DELETE `` → `` 409 `favorite group full`；`PUT /api/favorites/groups/:group_no` `{"name"}`：200 `{group_no,name}`、400（名稱由 `normalizeGroupName` 檢查：去掉前後空白後 1–10 個字元、不含控制字元）、404 `group not found`；不同群組可重名、沒有還原預設名稱的方式；用 `INSERT ... ON CONFLICT DO UPDATE` 寫入，不加鎖；`DELETE ``
   - `降級時超過目前群組數的資料保留但不顯示。` → `降級時超過目前群組數的資料與自訂名稱保留但不顯示。`

- [ ] **Step 12: 更新 `README.md`**

1. 開頭說明：`由本服務寫入 \`user_favorite_stocks\`。` → `由本服務寫入 \`user_favorite_stocks\` 與 \`user_favorite_groups\`。`

2. Favorites 段落：`同一檔股票可以放在多個群組。` → `同一檔股票可以放在多個群組。群組可以自訂名稱（存在 \`user_favorite_groups\`，只存改過名的群組）。`

3. Favorites 表格，在 `POST` 那一列之後加上：

```
| PUT | `/api/favorites/groups/:group_no` | 自訂群組名稱 |
```

4. `找不到設定、找不到該 level 或格式錯誤時，三支 API 都回 \`500\`。會員等級調降時，超過新群組數的資料會保留但不顯示；` → `找不到設定、找不到該 level 或格式錯誤時，四支 API 都回 \`500\`。會員等級調降時，超過新群組數的資料與自訂名稱會保留但不顯示；`

5. 資料表那一段整段換成：

```
**資料表（部署新版程式之前先執行）：** 全新環境（例如 production）執行 `db/user_favorites.sql`，可重複執行；已套用群組版 schema 的環境（例如 staging）重跑一次即可加上 `user_favorite_groups`，不會刪資料。套用過舊版（單一清單）的環境改執行**一次** `db/user_favorites_v2_migration.sql`（**會刪除所有我的最愛資料，不可重複執行**；staging 已執行過）。程式已部署但 SQL 還沒執行時，API 會回 `500`。
```

6. `GET /api/favorites` 的說明：在 `- 群組內依 \`created_time\` 由新到舊排序；...` 那一點之前加上：

```
- `name` 為使用者自訂的名稱；沒改過名的群組為預設名稱（群組一、群組二……）。
```

7. 在 `#### \`DELETE /api/favorites/groups/:group_no/:symbol\`` 之前加上（後面空一行）：

````
#### `PUT /api/favorites/groups/:group_no`

自訂群組名稱；再改一次會取代原本的名稱。

**Path Parameters:** `group_no` — 群組編號（從 1 開始）

**Request Body:**

```json
{ "name": "科技股" }
```

`name` 會先去掉前後空白（包括全形空白），去掉後必須有 1–10 個字元（以 Unicode 字元計，中英文都算 1 個），不能含換行等控制字元。不同群組可以取一樣的名字。沒有還原預設名稱的 API；想改回原名就把名稱設成「群組一」等。

| HTTP | Body | 意義 |
|------|------|------|
| `200` | `{"group_no":1,"name":"科技股"}` | 改名成功（`name` 為去掉空白後的值；名稱跟原本一樣也回 `200`） |
| `400` | `{"error":"invalid request"}` | `group_no` 不是 ≥ 1 的整數、body 不是合法的 JSON、`name` 不是字串，或名稱不符合上述規則 |
| `404` | `{"error":"group not found"}` | `group_no` 超過目前等級的群組數 |
````

- [ ] **Step 13: 全部測試**

Run: `go build ./... && go vet ./... && go test ./... -count=1 2>&1 | tail -6`
Expected: `ok  main/auth`、`ok  main/routers`，沒有 FAIL

- [ ] **Step 14: Commit**

```bash
git add CLAUDE.md README.md docs/frontend-favorites-integration.md docs/frontend-api-auth-integration.md
git commit -m "docs: 自訂群組名稱的 README、CLAUDE.md 與前端串接文件"
```
