# 我的最愛群組 — Design

**Date:** 2026-10-12
**Status:** Draft（待使用者審閱）
**Scope:** 把我的最愛從單一清單改成固定數量的群組（群組一、群組二……）。群組數與每組上限依 `member_level` 而定。GET 回傳所有群組與每個群組底下的個股。這次直接改版，取代 2026-10-05 的單一清單 API（`docs/superpowers/specs/2026-10-05-user-favorite-stocks-design.md`）。

## Goal

- `member_level 0`：5 個群組，每組最多 5 檔；`member_level 1`：10 個群組，每組最多 10 檔。
- 群組名稱固定為「群組一、群組二、群組三……」，使用者不能新增、刪除或改名。
- 同一檔股票可以同時放在多個群組；同一個群組裡不重複。
- `GET /api/favorites` 回傳目前等級的所有群組（包括空的群組）與群組底下的個股。
- 新增與刪除都針對特定群組。
- 同時送出多個新增請求時，仍然保證每個群組不超過上限。
- 完成後提供給前端的串接文件（改寫 `docs/frontend-favorites-integration.md`）。

## Background（現況）

- 單一清單版已經上線到 **staging**（`.env` 指向的 Railway DB），**production 還沒部署**。
- staging 的 `user_favorite_stocks` 目前有 5 筆資料，屬於 2 個 level 0 的 user（前端串接時加入的）。`user_config` 只有 `FAVORITE_STOCKS_LIMIT = {"0":10,"1":15,"2":20}`。
- 現有程式：`routers/favorites.go`（三支 handler，POST 用 `SELECT member_level ... FOR UPDATE` 讓同一個 user 的請求排隊處理）、`routers/favorites_limit_test.go`、`routers/favorites_test.go`；`auth.RequireLogin` / `auth.CurrentUser` 不需要改。
- 前端已經依照 `docs/frontend-favorites-integration.md` 串接單一清單版。
- 上次 final review 留下的 M1：symbol 含 NUL 字元時，PostgreSQL 會拒絕參數（`invalid byte sequence for encoding "UTF8": 0x00`），導致 500。M4：額滿時重複加入回 200 的測試沒有檢查回傳的 body。

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | 同一檔股票可以放在多個群組；唯一性改成 `(user_id, group_no, symbol)` | 使用者指定；符合「依主題分類」的使用方式 |
| D2 | 不建群組表；在 `user_favorite_stocks` 加 `group_no SMALLINT`；群組是「虛擬」的，數量由設定決定，名稱由後端依 `group_no` 產生 | 群組沒有可編輯的屬性，不需要存；降級時資料不受影響；以後要讓使用者改名，再加 `user_favorite_groups(user_id, group_no, name)` 就好，不用搬資料 |
| D3 | 設定 key 改成 `FAVORITE_GROUPS`，值為 `{"0":{"groups":5,"stocks_per_group":5},"1":{"groups":10,"stocks_per_group":10}}`；刪除 `FAVORITE_STOCKS_LIMIT` | 新規則需要兩個數字；level 2 先不設定（使用者指定） |
| D4 | 以下情況回 500：設定不存在、JSON 格式錯誤、找不到該 level、缺少 `groups` 或 `stocks_per_group`、`groups` 不在 1–99、`stocks_per_group` < 0、值不是整數 | 設定漏設或寫錯要立刻發現；99 是群組名稱產生的上限 |
| D5 | `is_og_member` 不影響群組數與上限 | 與前版一致 |
| D6 | 既有的 5 筆資料直接清掉，不搬移 | 使用者指定（前端串接時的測試資料） |
| D7 | 降級時保留資料，依目前等級呈現：GET 只回傳 `group_no ≤ groups` 的群組；群組內數量超過上限時照樣全部列出，但不能再加入；POST / DELETE 超過目前群組數的 `group_no` 回 404 `group not found` | 使用者指定；資料不會消失，升級回來會再出現 |
| D8 | 直接改版，不保留舊 API：移除 `POST /api/favorites`、`DELETE /api/favorites/:symbol`；GET 的回應格式改變 | 使用者指定；前端還在串接階段 |
| D9 | 新路徑：`POST /api/favorites/groups/:group_no`、`DELETE /api/favorites/groups/:group_no/:symbol`；`group_no` 從 1 開始 | REST 風格，群組是路徑的一部分 |
| D10 | GET 回應欄位：`max_groups`、`max_stocks_per_group`、`groups[{group_no, name, count, items[{symbol, name, created_time}]}]` | 使用者確認 |
| D11 | 群組已滿回 409 `{"error":"favorite group full","limit":<stocks_per_group>}` | 使用者確認 |
| D12 | 重複 POST（同群組、同股票）回 200 並附上既有資料（即使群組已滿）；DELETE 不論那筆是否存在都回 204 | 沿用前版的 idempotent 設計 |
| D13 | POST 繼續在 transaction 裡用 `SELECT member_level FROM users WHERE id = $1 FOR UPDATE` 鎖住該 user 的 row | 同一個 user 的所有 POST 排隊處理（跨群組也一樣），保證每個群組的數量檢查正確 |
| D14 | `group_no` 用 `strconv.Atoi` 解析，結果 < 1 或解析失敗回 400；`+1`、`01` 視為 1 | 簡單、可預期；超過 `groups`（≤ 99）的值在任何 SQL 之前就回 404，所以不會超出 SMALLINT |
| D15 | 修正 M1：POST 的 symbol（去掉空白後）含 NUL 字元時回 400 `invalid request`；DELETE 的 symbol 含 NUL 時直接回 204，不查 DB | 使用者指定；不合法的輸入不應該產生 500 |
| D16 | 遷移分成兩支 SQL：`db/user_favorites.sql`（新 schema 的最終狀態，可重複執行，不含 DROP）與 `db/user_favorites_v2_migration.sql`（只在套用過舊版的環境跑一次：DROP 舊表、刪除舊設定，再 `\ir user_favorites.sql`） | 可重複執行的檔案不能放 DROP，否則每跑一次就清一次資料；目前只有 staging 需要跑 migration |
| D17 | `count` 為該群組實際列出的個股數，降級時可能大於 `max_stocks_per_group` | 與前版「上限調低時 count 可能大於 limit」一致 |

## Data Model

### `db/user_favorites.sql`（改寫；可重複執行）

```sql
-- 我的最愛個股（TWStockAPI 擁有）；可重複執行
-- 全新環境：psql "$DATABASE_URL" -f db/user_favorites.sql
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

PK `(user_id, group_no, symbol)` 同時支援 `WHERE user_id = $1` 與 `WHERE user_id = $1 AND group_no = $2` 的查詢。

### `db/user_favorites_v2_migration.sql`（新增；只跑一次）

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

## API

三支 API 都在 `/api/favorites`，一律需要 `Authorization: Bearer <jwt>`，不受 `JWT_TOKEN_ENABLE` 影響（沿用 `auth.RequireLogin`）。錯誤格式：一般錯誤回 `{"error": "..."}`，500 回 `{"detail": "Internal server error"}`。

### `GET /api/favorites`

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
        {"symbol": "2330", "name": "台積電", "created_time": "2026-10-12T09:12:00Z"},
        {"symbol": "2317", "name": "鴻海",   "created_time": "2026-10-11T15:00:00Z"}
      ]
    },
    {"group_no": 2, "name": "群組二", "count": 0, "items": []}
  ]
}
```

- `groups` 固定有 `max_groups` 個，依 `group_no` 由小到大排列；空的群組 `items` 為 `[]`。
- 群組內依 `created_time DESC, symbol` 排序；被停用的股票照樣列出。
- `group_no > max_groups` 的資料不回傳（D7）。
- user 的 row 不存在回 401 `unauthorized`；設定錯誤（D4）回 500。

### `POST /api/favorites/groups/:group_no`

Request body：`{"symbol": "2330"}`；symbol 會先去掉前後空白。

| 情況 | 回應 |
|---|---|
| 新增成功 | **201** `{"group_no":1,"symbol":"2330","name":"台積電","created_time":"..."}` |
| 這個群組裡本來就有 | **200** 回傳既有的那筆資料（即使群組已滿） |
| `group_no` 不是 ≥ 1 的整數；body 不是合法的 JSON；symbol 是空的或含 NUL | 400 `{"error":"invalid request"}` |
| `group_no` 超過目前等級的群組數 | 404 `{"error":"group not found"}` |
| 股票不存在或 `enabled = false` | 404 `{"error":"stock not found"}` |
| 群組已滿 | 409 `{"error":"favorite group full","limit":5}` |
| 鎖定時 user 的 row 已經不存在 | 401 `{"error":"unauthorized"}` |
| 設定錯誤（D4）或其他 DB 錯誤 | 500 |

處理流程：

```
0. 解析 group_no、body、symbol（trim、空字串、NUL）→ 400；這一步不開 transaction
1. BEGIN
2. SELECT member_level FROM users WHERE id = $1 FOR UPDATE      → 沒有這一列：401
3. loadFavoriteGroups(tx, level)                                 → 錯誤：500
4. group_no > rule.Groups                                        → 404 group not found
5. SELECT 這個群組裡的這一檔（JOIN stocks 取 name）               → 有：200 + 既有資料
6. SELECT name FROM stocks WHERE symbol = $1 AND enabled         → 沒有：404 stock not found
7. SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1 AND group_no = $2
                                                                 → count >= rule.StocksPerGroup：409
8. INSERT ... RETURNING created_time
9. COMMIT → 201
```

### `DELETE /api/favorites/groups/:group_no/:symbol`

```
1. 解析 group_no                                   → 400 invalid request
2. symbol 含 NUL                                   → 204（不查 DB）
3. SELECT member_level（不加鎖）                    → 沒有這一列：401
4. loadFavoriteGroups                              → 錯誤：500
5. group_no > rule.Groups                          → 404 group not found
6. DELETE FROM user_favorite_stocks WHERE user_id = $1 AND group_no = $2 AND symbol = $3
7. 204（不論影響幾列）
```

只從指定的群組移除；其他群組裡的同一檔不受影響。

## Components

### `routers/favorites_config.go`（新增）

```go
// favoriteGroupsKey is the user_config key holding the per-member_level group rules,
// e.g. {"0": {"groups": 5, "stocks_per_group": 5}}.
const favoriteGroupsKey = "FAVORITE_GROUPS"

// maxFavoriteGroups is the most groups groupName can name (群組九十九).
const maxFavoriteGroups = 99

type groupRule struct {
    Groups         int
    StocksPerGroup int
}

// parseFavoriteGroups 解析 FAVORITE_GROUPS 的 JSONB value，回傳該 member_level 的規則（D4）。
func parseFavoriteGroups(raw []byte, level int) (groupRule, error)

// loadFavoriteGroups 讀取 user_config 的 FAVORITE_GROUPS，回傳 level 對應的規則。
func loadFavoriteGroups(ctx context.Context, q querier, level int) (groupRule, error)

// groupName 回傳群組名稱，例如 1 → 群組一、10 → 群組十、21 → 群組二十一（1 ≤ n ≤ 99）。
func groupName(n int) string
```

- `parseFavoriteGroups` 解析到 `map[string]struct{ Groups, StocksPerGroup *int }`，用指標分辨「缺少欄位」與「值為 0」。小數或字串會讓 `json.Unmarshal` 失敗，視為設定錯誤。
- 取代 `parseFavoriteLimit`、`loadFavoriteLimit`、`favoriteLimitKey`（刪除）；`routers/favorites_limit_test.go` 改名為 `routers/favorites_config_test.go`。

### `routers/favorites.go`（改寫）

- `RegisterFavorites(rg, requireLogin)`：`GET ""`、`POST "/groups/:group_no"`、`DELETE "/groups/:group_no/:symbol"`。
- `favoriteItem`（`symbol`、`name`、`created_time`）沿用，用在 GET 的 `items`；POST 回應用 `favoriteGroupItem`（多一個 `group_no`）。
- 共用 helper：讀取 user 的 `member_level`（POST 用 `FOR UPDATE` 的版本，GET / DELETE 用不加鎖的版本）再呼叫 `loadFavoriteGroups`，回傳 `groupRule`。user 不存在時回傳可辨識的 error，handler 轉成 401。
- `parseGroupNo(c *gin.Context) (int, bool)`：D14。
- GET 只查一次：`SELECT f.group_no, f.symbol, s.name, f.created_time FROM user_favorite_stocks f JOIN stocks s ON s.symbol = f.symbol WHERE f.user_id = $1 AND f.group_no <= $2 ORDER BY f.group_no, f.created_time DESC, f.symbol`，在 Go 裡依 `group_no` 分到預先建好的 `rule.Groups` 個群組。
- `querier`、`favoritesServerError` 沿用。

### 不需要修改

`auth/`、`main.go`（路由仍由 `RegisterFavorites` 註冊；CORS 已允許 `GET` / `POST` / `DELETE`）。

## Testing

### 單元測試（`routers/favorites_config_test.go`）

routers package 的 `TestMain` 沒設 DB 時會整包 skip，所以這些測試也只有在設定 DB 時才會執行。

- `parseFavoriteGroups`：level 0 / 1 正常取值；`stocks_per_group: 0` 合法；錯誤：level 不存在、缺少 `groups`、缺少 `stocks_per_group`、`groups` 為 0、`groups` 為 100、`stocks_per_group` 為 -1、值為小數、值為字串、JSON 格式錯誤、`null`、陣列。
- `groupName`：1 → 群組一、9 → 群組九、10 → 群組十、11 → 群組十一、19 → 群組十九、20 → 群組二十、21 → 群組二十一、99 → 群組九十九。

### 整合測試（改寫 `routers/favorites_test.go`；寫入 staging，`t.Cleanup` 清除）

沿用前版的 fixture（暫時的測試 user、fake login、`enabledSymbols`、`disabledSymbol`、直接寫入的 helper，改成帶 `group_no`）；規則從 `user_config` 讀取，不寫死。

- GET：level 0 回 `max_groups` 個空群組，名稱與 `group_no` 正確；level 1 回 10 個；群組內新的排前面；被停用的股票照樣列出；只看得到自己的。
- 同一檔股票加入兩個群組 → 兩次都是 201；GET 兩個群組都有；DELETE 其中一組後，另一組還在。
- POST：201，然後同群組重複 POST 回 200，**body 與 201 相同**；symbol 前後空白會被去掉；400：`group_no` 為 `abc`、`0`、`-1`、`1.5`、body 不是合法的 JSON、空 body、缺少 symbol、空白 symbol、數字 symbol、`"\u0000"`；404 `stock not found`：不存在、超過 10 字元、已停用；409：群組加到上限後，下一個回 `{"error":"favorite group full","limit":N}`，但其他群組照樣能加入；群組已滿時加入已存在的那檔回 200，**body 為既有資料**（M4）。
- 降級（D7）：level 0 user，直接寫入群組一 9 筆、群組八 1 筆 → GET 只回 5 個群組，群組一 `count = 9`，沒有群組八；POST 群組一回 409；POST 群組八回 404 `group not found`；DELETE 群組八回 404；DELETE 群組一的一筆回 204。
- `group_no` 超過群組數：POST / DELETE `group_no = max_groups + 1` 回 404 `group not found`。
- DELETE：204，再 DELETE 一次也是 204；`group_no` 為 `abc` 回 400；symbol 為 `%00` 回 204；只刪得到自己的。
- 同時送出：對同一個群組同時送 `max_stocks_per_group + 5` 個不同 symbol 的 POST → `{201: N, 409: 5}`，DB 中該群組剛好 N 筆。
- 等級沒有設定（例如 `member_level = 2`）：GET / POST / DELETE 回 500。
- user 的 row 不存在：GET / POST / DELETE 回 401。

### 端對端

改寫 scratchpad 的 e2e script：建立暫時的測試 user、用 `.env` 的 `JWT_SECRET` 簽 token、起本機 server，用 curl 跑一遍三支 API（包括 preflight），測完刪除測試 user。

### 遷移

在 staging 執行 `db/user_favorites_v2_migration.sql` 前**先向使用者確認**；執行後確認舊表與舊設定已移除、新表有 `group_no` 欄位、`FAVORITE_GROUPS` 已插入。

## Docs

- `CLAUDE.md`：更新 `favorites.go` 的說明（群組、新路徑、`FAVORITE_GROUPS`、降級行為），`db/*.sql` 補上 `user_favorites_v2_migration.sql`。
- `README.md`：改寫 Favorites 段落（新路徑、回應格式、設定方式、遷移步驟）。
- `docs/frontend-favorites-integration.md`：**改寫成群組版**，給前端串接用。內容包括新的路徑與回應格式、從單一清單版改版時要調整的地方、程式範例（fetch / axios）、UI 建議、curl 測試與常見問題。

## Out of Scope

- 自訂群組名稱、新增或刪除群組、調整群組順序。
- 在群組之間移動個股的 API（目前用「在新群組加入，再從舊群組刪除」達成）。
- 一次從所有群組移除某檔股票的 API。
- 上次 review 的 M2（POST transaction 沒有 lock timeout）、M3（`FOR UPDATE` 換成 `FOR NO KEY UPDATE`）。
