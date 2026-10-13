# 我的最愛群組自訂名稱 — Design

**Date:** 2026-10-13
**Status:** Draft（待使用者審閱）
**Scope:** 讓 user 自訂我的最愛群組的名稱。群組數與每組上限不變，仍依 `member_level` 而定；沒改過名的群組照舊顯示「群組N」。建立在 `docs/superpowers/specs/2026-10-12-favorite-groups-design.md`（群組版）之上，該版還沒 merge，也還沒部署到任何 server。

## Goal

- 新增改名 API：`PUT /api/favorites/groups/:group_no`。
- `GET /api/favorites` 的回應格式不變；群組的 `name` 有自訂名稱時回傳自訂名稱，否則回傳預設的「群組N」。
- 完成後更新給前端的串接文件 `docs/frontend-favorites-integration.md`。

## Background（現況）

- 群組版在 `feat/favorite-groups` branch（5 個 commit，還沒 merge 到 `develop`）。staging DB 已經套用群組版的 schema；staging server 還在跑單一清單版的程式。production 兩版都還沒部署。
- 群組是虛擬的：沒有群組表，`user_favorite_stocks` 用 `group_no` 欄位分組；名稱由 `groupName(group_no)` 產生（`routers/favorites_config.go`）。
- 前端串接文件（群組版）還沒交給前端。
- API 只使用 `GET`、`POST`、`PUT`、`DELETE`（使用者指定，不用 `PATCH`）。

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | 新增 `user_favorite_groups(user_id, group_no, name, created_time, updated_time)`，PK `(user_id, group_no)`；只存改過名的群組，沒有資料的群組用預設名稱。群組本身維持虛擬，`user_favorite_stocks` 不變、兩張表之間沒有 FK | `users` 表屬於 AccountService，不能加欄位；名稱存在 `user_favorite_stocks` 的話，空的群組沒地方存；以後群組要加其他屬性（排序、顏色）也放這張表 |
| D2 | 名稱規則：先用 `strings.TrimSpace` 去掉前後空白（包括全形空白 U+3000）；去掉後要有 1–10 個字元（以 Unicode code point 計，中英文都算 1 個字）；不能含控制字元（`unicode.IsControl`，例如 `\n`、`\t`、`\u0000`）；必須是合法的 UTF-8。中間的空白保留 | 使用者選 10 個字；PostgreSQL 的 `VARCHAR(10)` 也是以字元計，與 Go 的 `utf8.RuneCountInString` 一致 |
| D3 | 不同群組可以取一樣的名字 | 前端以 `group_no` 辨識群組；禁止重名只會多一個錯誤狀態 |
| D4 | 不提供還原預設名稱的方式。名稱是空的（去掉空白後）回 400；想改回原名就輸入「群組三」，也會存成自訂名稱 | 使用者指定 |
| D5 | `PUT /api/favorites/groups/:group_no`，body `{"name": "科技股"}`，成功回 **200** `{"group_no": 1, "name": "科技股"}`（`name` 為去掉空白後的值）。名稱跟原本一樣也回 200 | 使用者指定用 PUT；群組目前只有名稱可以改；路徑與 POST 一致 |
| D6 | 所有 400 一律回 `{"error":"invalid request"}`，不區分原因 | 與現有 POST / DELETE 一致；前端依文件規則先在畫面上擋 |
| D7 | 寫入用 `INSERT ... ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()`；不開 transaction、不鎖 user 的 row；同時改名以後寫入的為準 | 改名沒有數量上限要維護，不需要排隊 |
| D8 | 降級時，超過目前群組數的群組名稱保留在 DB，但 GET 不回傳；`PUT` 超過目前群組數的 `group_no` 回 404 `group not found`；升級回來會再出現 | 與群組版 D7（個股資料）一致 |
| D9 | GET 多查一次 `user_favorite_groups`（只查 `group_no <= max_groups`），用自訂名稱取代預設名稱；回應格式不變 | 前端不用改程式就能顯示自訂名稱 |
| D10 | CORS 的 `AllowMethods` 加上 `PUT`，變成 `GET`、`POST`、`PUT`、`DELETE` | 瀏覽器的 PUT 需要 preflight 允許 |
| D11 | 名稱檢查寫成獨立的函式 `normalizeGroupName`，放在 `routers/favorites_config.go`（與 `groupName` 放一起） | 可以單獨做單元測試 |
| D12 | 接著 commit 在 `feat/favorite-groups`，與群組版一起 merge、一起部署 | 使用者指定；前端只要串一次 |
| D13 | 新表加進 `db/user_favorites.sql`（`CREATE TABLE IF NOT EXISTS`，可重複執行，不刪資料）。staging 重跑一次這份檔案（**執行前先向使用者確認**）；production 照原本流程，部署前先跑這份檔案 | 不需要新的 migration 檔 |

## Data Model

### `db/user_favorites.sql`（加在 `user_favorite_stocks` 之後）

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

- `user_favorite_stocks` 上方的註解「群組名稱由後端依 group_no 產生」改成「預設群組名稱由後端依 group_no 產生；自訂名稱存在 user_favorite_groups」。
- `CHECK (name <> '')` 是 DB 端的最後防線；正常情況下程式已經擋掉空名稱。
- `db/user_favorites_v2_migration.sql` 不用改：它最後 `\ir user_favorites.sql`，在還沒套用 v2 的環境會一起建立新表。

## API

### `PUT /api/favorites/groups/:group_no`（新增）

一律需要 `Authorization: Bearer <jwt>`（沿用 `auth.RequireLogin`）。

Request body：`{"name": "科技股"}`

| 情況 | 回應 |
|---|---|
| 成功（包括名稱跟原本一樣） | **200** `{"group_no": 1, "name": "科技股"}` |
| `group_no` 不是 ≥ 1 的整數 | 400 `{"error":"invalid request"}` |
| body 不是合法的 JSON；`name` 不是字串 | 400 `invalid request` |
| 沒帶 `name`、`"name": null`、去掉空白後是空的 | 400 `invalid request` |
| 名稱超過 10 個字元（去掉空白後） | 400 `invalid request` |
| 名稱含控制字元，或不是合法的 UTF-8 | 400 `invalid request` |
| user 的 row 已經不存在 | 401 `{"error":"unauthorized"}` |
| `group_no` 超過目前等級的群組數 | 404 `{"error":"group not found"}` |
| 設定錯誤（群組版 D4）或其他 DB 錯誤 | 500 `{"detail":"Internal server error"}` |

處理流程：

```
0. auth.CurrentUser
1. parseGroupNo                                        → 400
2. ShouldBindJSON 到 struct{ Name string }             → 400
3. normalizeGroupName(req.Name)                        → 不合法：400
4. loadUserRule(pool, selectMemberLevel, user.ID)      → user 不存在：401；設定錯誤：500
5. group_no > rule.Groups                              → 404 group not found
6. INSERT INTO user_favorite_groups (user_id, group_no, name) VALUES ($1::uuid, $2, $3)
   ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()
                                                       → 錯誤：500
7. 200 {"group_no", "name"}
```

- 名稱檢查（步驟 1–3）在查 DB 之前，所以「名稱不合法」且「群組不存在」時回 400，與 POST 一致。
- 步驟 4 到 6 之間 user 被刪除時，INSERT 會因為 FK 失敗而回 500；機率極低，不特別處理。
- Go 的 `encoding/json` 解碼時會把不合法的 UTF-8 換成 U+FFFD，所以透過 JSON 送來的名稱實際上不會是不合法的 UTF-8；`normalizeGroupName` 仍然檢查，作為防線。

### `GET /api/favorites`（修改）

回應格式不變（見群組版 spec）。流程改為：

1. 建立 `rule.Groups` 個群組，名稱先填 `groupName(n)`。
2. **新增**：`SELECT group_no, name FROM user_favorite_groups WHERE user_id = $1::uuid AND group_no <= $2`，有資料的群組改用自訂名稱。
3. 照舊查 `user_favorite_stocks`，依 `group_no` 放進群組。

```json
{
  "max_groups": 5,
  "max_stocks_per_group": 5,
  "groups": [
    {"group_no": 1, "name": "科技股", "count": 1, "items": [{"symbol": "2330", "name": "台積電", "created_time": "2026-10-13T09:12:00Z"}]},
    {"group_no": 2, "name": "群組二", "count": 0, "items": []}
  ]
}
```

### `POST`、`DELETE`

不變。

## Components

### `routers/favorites_config.go`

```go
// maxGroupNameLen 是自訂群組名稱的最大字元數（以 Unicode code point 計）；
// 與 user_favorite_groups.name 的 VARCHAR(10) 一致。
const maxGroupNameLen = 10

// normalizeGroupName 去掉名稱前後的空白後檢查（D2）；合法時回傳去掉空白後的名稱與 true。
func normalizeGroupName(raw string) (string, bool)
```

### `routers/favorites.go`

- `RegisterFavorites` 加上 `g.PUT("/groups/:group_no", renameFavoriteGroup)`。
- 新增 request / response 型別：

```go
type renameFavoriteGroupRequest struct {
    Name string `json:"name"`
}

// favoriteGroupName 是 PUT 的回應。
type favoriteGroupName struct {
    GroupNo int    `json:"group_no"`
    Name    string `json:"name"`
}
```

- 新增 `renameFavoriteGroup(c *gin.Context)`，依上面的處理流程。
- `listFavorites` 加上查詢自訂名稱的步驟。

### `main.go`

- CORS `AllowMethods: []string{"GET", "POST", "PUT", "DELETE"}`。

### 不需要修改

`auth/`、`addFavorite`、`deleteFavorite`、`parseFavoriteGroups`、`groupName`。

## Testing

### 單元測試（`routers/favorites_config_test.go`）

`TestNormalizeGroupName`（table-driven）：

| 輸入 | 結果 |
|---|---|
| `"科技股"` | `"科技股"`, true |
| `"  科技股  "` | `"科技股"`, true |
| `"　科技股　"`（全形空白） | `"科技股"`, true |
| `"\t科技股\n"`（前後的 tab / 換行會被去掉） | `"科技股"`, true |
| `"科技 股"`（中間的空白保留） | `"科技 股"`, true |
| `"一二三四五六七八九十"`（10 個中文字） | 原值, true |
| `"abcdefghij"`（10 個英文字） | 原值, true |
| `"  一二三四五六七八九十  "`（去掉空白後 10 個字） | `"一二三四五六七八九十"`, true |
| `"群組三"` | `"群組三"`, true |
| `"📈"` | `"📈"`, true |
| `"一二三四五六七八九十一"`（11 個字） | false |
| `"abcdefghijk"`（11 個字） | false |
| `""` | false |
| `"   "` | false |
| `"　"` | false |
| `"科技\n股"` | false |
| `"科技\t股"` | false |
| `"科技\u0000股"` | false |
| `"\xff"`（不合法的 UTF-8） | false |

### 整合測試（新增 `routers/favorites_rename_test.go`；寫入 staging，`t.Cleanup` 清除）

沿用 `routers/favorites_test.go` 的 fixture（`newFavoritesUser`、`newFavoritesRouter`、`groupRuleFor`、`groupPath`、`doJSONBody`）。刪除測試 user 時，`ON DELETE CASCADE` 會一起刪掉自訂名稱。新增直接讀寫 `user_favorite_groups` 的 helper。

- PUT 成功：回 200，body 為 `{"group_no":1,"name":"科技股"}`，DB 有這一列。
- PUT 去掉空白：`"  科技股  "` → body 與 DB 都是 `"科技股"`。
- PUT 兩次：第二次的名稱取代第一次，DB 仍然只有一列。
- PUT 10 個中文字 → 200，DB 存得下（確認 `VARCHAR(10)` 以字元計）；11 個中文字 → 400，DB 沒有資料。
- PUT 400：`group_no` 為 `abc`、`0`；body 不是合法的 JSON；空 body；`{}`；`{"name":123}`；`{"name":null}`；`{"name":""}`；`{"name":"   "}`；`{"name":"科技\n股"}`；`{"name":"\u0000"}`。每一種 DB 都沒有資料。
- PUT 404：`group_no = max_groups + 1` 回 `group not found`，DB 沒有資料。
- PUT 401：user 的 row 不存在。
- PUT 500：等級沒有設定（`member_level = 2`）。
- PUT `"群組三"` 到群組一 → 200，GET 的群組一顯示「群組三」。
- 不同群組可以取一樣的名字：群組一、群組二都改成 `"科技股"` → 兩次都是 200。
- GET：改過名的群組顯示自訂名稱，其他群組維持「群組N」；改名不影響 `count` 與 `items`。
- GET 只看得到自己的：user A 改群組一的名稱，user B 的群組一仍然是「群組一」。
- 降級（D8）：level 0 user，直接寫入群組八的名稱 → GET 只回 5 個群組，沒有群組八；PUT 群組八回 404；把 `member_level` 改成 1 → GET 的群組八顯示自訂名稱。

### 端對端

擴充 scratchpad 的 e2e script（帶真 token、起本機 server）：
- PUT 的 preflight 允許 `PUT`；
- PUT 改名後 GET 看得到；
- PUT 空名稱回 400；
- PUT 超過群組數回 404；
- 沒帶 token 回 401。

### DB

在 staging 執行 `db/user_favorites.sql` 前**先向使用者確認**；執行後確認 `user_favorite_groups` 存在、欄位與 constraint 正確，`user_favorite_stocks` 與 `user_config` 的資料不受影響。

## Docs

- `CLAUDE.md`：`favorites.go` 的說明加上 PUT 與 `user_favorite_groups`；CORS 改成 `GET` / `POST` / `PUT` / `DELETE`；`db/*.sql` 的 `user_favorites.sql` 說明加上 `user_favorite_groups`。
- `README.md`：Favorites 段落加上 PUT（請求、回應、錯誤）與新表。
- `docs/frontend-favorites-integration.md`：加入改名 API 的規格、名稱規則（前端先擋）、fetch 和 axios 範例、UI 建議（原地編輯、10 字計數）、curl 範例；CORS 表格加上 `PUT`；FAQ 補上「怎麼改回預設名稱」（自己輸入「群組N」）。
- `docs/frontend-api-auth-integration.md`：CORS 表格的 Methods 加上 `PUT`。

## Out of Scope

- 還原預設名稱的 API（D4）。
- 禁止重名（D3）。
- 新增或刪除群組、調整群組順序、在群組之間移動個股。
- 以「使用者看到的字數」（grapheme cluster）計算長度：由多個 code point 組成的 emoji（例如 👨‍👩‍👧）會算成多個字。
- 群組版 final review 留下的 minor：#2（`BeginTx` 固定 ReadCommitted）、#6（migration 防呆）、#7（測試缺口）。
