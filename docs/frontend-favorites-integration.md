# 前端串接：我的最愛群組（`/api/favorites`）

本文件給**前端**參考，說明如何串接 TWStockAPI 的「我的最愛」群組功能：取得所有群組、把股票加入群組、從群組移除、幫群組改名。

- 登入與帶 token 的方式：見 [frontend-api-auth-integration.md](frontend-api-auth-integration.md)
- 完整 API 規格：見 TWStockAPI `README.md` 的 Favorites 段落

> **改版說明（2026-10-12）：** 我的最愛從「單一清單」改成「群組」。舊的 `POST /api/favorites`、`DELETE /api/favorites/:symbol` 已移除，GET 的回應格式也改了。舊版留下的資料已清除。從舊版改過來時要調整的地方見第 8 節。
>
> **更新（2026-10-13）：** 群組可以自訂名稱：新增 `PUT /api/favorites/groups/:group_no`（§3.4）。GET 的回應格式不變，`groups[].name` 改成回傳自訂名稱（沒改過名的群組仍是「群組一」等）。

---

## 1. 功能概覽

| 項目 | 說明 |
|------|------|
| 用途 | 使用者把個股分組收藏，方便快速選取 |
| 歸屬 | 每個使用者各自一份，只看得到、也只改得到自己的 |
| 登入 | **一律需要登入**：不論後端 `JWT_TOKEN_ENABLE` 開關怎麼設，都必須帶 `Authorization: Bearer <token>` |
| 群組 | 數量固定，依會員等級（`member_level`）而定，**不能新增或刪除**；預設名稱為「群組一、群組二……」，**可以改名**（§3.4） |
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
| `PUT` | `/api/favorites/groups/:group_no` | 幫群組改名 |
| `DELETE` | `/api/favorites/groups/:group_no/:symbol` | 從指定群組移除一檔股票 |

- `group_no` 是群組編號，**從 1 開始**（群組一 = `1`）。回應裡的 `group_no` 一律是 number；前端比對時記得把 `<select>` 等來源的字串轉成 number（見 §4.3 `groupOf`）。
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
| `groups[].name` | string | 群組名稱：使用者改過名時為自訂名稱，否則為「群組一」……；直接顯示即可（請當成純文字顯示，見 §3.4） |
| `groups[].count` | number | 這個群組目前的數量（等於 `items.length`） |
| `groups[].items` | array | 群組內的個股，依 `created_time` 由新到舊；**空的群組是 `[]`，不會是 `null`** |
| `items[].symbol` | string | 股票代號 |
| `items[].name` | string | 股票名稱 |
| `items[].created_time` | string | 加入這個群組的時間，**含時區的 RFC 3339 字串**；可能是 `...Z` 或 `...+08:00`，請用 `new Date(created_time)` 解析 |

注意：

- 股票加入之後如果被後端停用，**仍然會出現在群組中**，可以正常移除。
- 會員等級被調降時，超過新群組數的群組不會出現（資料與自訂名稱都保留，升級回來會再出現）；群組內的數量可能大於 `max_stocks_per_group`（例如 `9/5`），這時可以移除，但不能再加入。

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

TypeScript 型別（參考）：

```ts
export interface FavoriteItem {
  symbol: string;
  name: string;
  created_time: string; // RFC 3339
}

export interface FavoriteGroup {
  group_no: number; // 從 1 開始
  name: string; // 自訂名稱，或預設的 "群組一"……
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

export interface FavoriteGroupName {
  group_no: number;
  name: string;
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
import { listFavoriteGroups, addToGroup, removeFromGroup, renameGroup } from "./favorites";

const state = { maxGroups: 0, maxPerGroup: 0, groups: [] };

// 頁面載入、登入後：取一次所有群組
export async function loadFavorites() {
  const data = await listFavoriteGroups();
  state.maxGroups = data.max_groups;
  state.maxPerGroup = data.max_stocks_per_group;
  state.groups = data.groups;
}

// groupNo 可以是 number 或字串（例如 <select> 的 value、URL 參數），一律轉成 number 比對
function groupOf(groupNo) {
  return state.groups.find((g) => g.group_no === Number(groupNo));
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
    // 201（新加入）放到最前面；200（本來就在群組裡）不用動，維持與後端相同的順序
    if (!g.items.some((i) => i.symbol === item.symbol)) {
      g.items = [item, ...g.items];
      g.count = g.items.length;
    }
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
  if (Number(fromGroupNo) === Number(toGroupNo)) return; // 同一個群組：什麼都不做（否則會把它刪掉）
  await onAdd(toGroupNo, symbol);
  if (groupOf(toGroupNo)?.items.some((i) => i.symbol === symbol)) {
    await onRemove(fromGroupNo, symbol);
  }
}

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

### 4.4 axios 版

沿用 [auth 文件 §4.2](frontend-api-auth-integration.md#42-axios) 的 `api` instance（已經會自動帶 token、統一處理 401）：

```js
import { api } from "./api";

export const listFavoriteGroups = () => api.get("/api/favorites").then((r) => r.data);

export const addToGroup = (groupNo, symbol) =>
  api.post(`/api/favorites/groups/${groupNo}`, { symbol }).then((r) => r.data); // 201 或 200

export const removeFromGroup = (groupNo, symbol) =>
  api.delete(`/api/favorites/groups/${groupNo}/${encodeURIComponent(symbol)}`); // 204，r.data 為 ""

export const renameGroup = (groupNo, name) =>
  api.put(`/api/favorites/groups/${groupNo}`, { name }).then((r) => r.data); // 200 { group_no, name }

// 群組資訊過期時重新取得（與 §4.3 的 loadFavorites 相同）
async function reloadFavorites() {
  const data = await listFavoriteGroups();
  // 用 data 更新你們的 state 並重畫
  return data;
}

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
| 群組分頁 / 清單 | 直接用 `groups` 的順序與 `name`；不要自己產生「群組一」等名稱（使用者可能改過名） |
| 加入股票 | 點收藏時讓使用者選群組；用 `groupsContaining(symbol)` 標出已經在哪些群組 |
| 顯示數量 | 每個群組顯示 `items.length` / `max_stocks_per_group`，例如「群組一 2 / 5」 |
| 群組已滿 | `items.length >= max_stocks_per_group` 時，選單裡該群組設成不可選並標示「已滿」；仍然可以移除 |
| 加入 / 移除進行中 | 把按鈕暫時設成不可按，避免連點（後端已保證連點不會出錯，這只是讓畫面比較穩定） |
| 加入成功 | 直接用回傳的那一筆更新該群組，不需要重新 GET |
| 收到 `409` | 用回傳的 `limit` 更新每組上限 |
| 收到 `404 group not found` | 重新 GET（群組數可能因等級調整而改變） |
| 移到其他群組 | 沒有「移動」API：先 POST 到新群組，成功後再 DELETE 舊群組（見 §4.3 `onMove`） |
| 改名 | 在群組名稱旁放編輯按鈕（或雙擊名稱）原地編輯；Enter 送出、Esc 取消；送出前用 `normalizeGroupName` 檢查（見 §4.3 `onRename`）。**注音、倉頡等輸入法組字時按的 Enter 是選字，不能送出**：`keydown` 時 `e.isComposing` 為 `true` 或 `e.keyCode === 229`（Safari 要靠後者）就略過 |
| 名稱字數 | 顯示「`[...value].length` / 10」的計數。`<input maxlength>` 以 UTF-16 計算，部分 emoji 會被提早擋下，建議用計數提示加上送出前檢查 |
| 改名成功 | 用回傳的 `name` 更新畫面（後端會去掉前後空白），不需要重新 GET |
| 顯示群組名稱 | 一律當成純文字顯示，不要用 `innerHTML` |
| 被停用的股票 | 仍然會出現在群組中；點進去沒有行情資料屬正常現象，可以讓使用者移除 |
| 顯示時間 | `new Date(item.created_time).toLocaleString("zh-TW")` |

---

## 6. CORS

`GET`、`POST`、`PUT`、`DELETE` 都在允許清單內（見 [auth 文件 §6](frontend-api-auth-integration.md#6-cors)）。

- `POST`、`PUT` 帶 `Content-Type: application/json` 與 `Authorization`，`DELETE` 帶 `Authorization`，瀏覽器都會先送 `OPTIONS` preflight，後端回 `204` 屬正常現象。
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

# 把群組一改名為「科技股」
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name":"科技股"}' -w '\nHTTP %{http_code}\n' $BASE/api/favorites/groups/1

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
| 群組名稱固定 | 新增 `PUT /api/favorites/groups/:group_no` 幫群組改名（§3.4）；GET 的 `name` 可能是自訂名稱 |

舊版留下的我的最愛資料已清除，使用者需要重新加入。`apiFetch` 的調整（處理 `204`、保留錯誤 body）與上一版相同。

---

## 9. 常見問題排查

| 症狀 | 可能原因 |
|------|----------|
| 其他 API 不帶 token 都正常，只有 `/api/favorites` 回 `401` | 正常：我的最愛**一律需要登入**，不受後端開關影響 |
| 呼叫舊的 `POST /api/favorites` 或 `DELETE /api/favorites/2330` 回 `404` | 舊 API 已移除，改用 `/api/favorites/groups/:group_no/...`（見第 8 節） |
| `DELETE` 成功了但前端拋出 JSON 解析錯誤 | `204` 沒有 body，不要呼叫 `res.json()`（見 §4.1） |
| `POST` 回 `400 invalid request` | `group_no` 傳了 `0`、`abc` 等不是 ≥ 1 的整數；body 沒有 `JSON.stringify`；`symbol` 送成數字（例如 `2330` 而不是 `"2330"`） |
| `POST` / `PUT` / `DELETE` 回 `404 group not found` | `group_no` 超過這個使用者的群組數（例如 level 0 傳 `6`），或前端的群組資訊過期 → 重新 GET |
| `POST` 回 `404 stock not found`，但股票確實存在 | 該股票後端未啟用，或代號大小寫不一致 |
| `PUT` 回 `400 invalid request` | 名稱去掉空白後是空的、超過 10 個字、含換行或 tab；body 沒有 `JSON.stringify`；`name` 送成數字或 `null` |
| 輸入框看起來不到 10 個字，`PUT` 卻回 `400` | 部分 emoji（例如 👨‍👩‍👧、國旗）由多個字元組成，後端會算成多個字 |
| 改名後想改回「群組一」 | 沒有還原 API；直接把名稱改成「群組一」 |
| `POST` 回 `409`，但畫面上這個群組還沒滿 | 前端資料過期（例如在別的裝置加過）→ 重新 GET |
| 某個群組的數量大於每組上限 | 會員等級被調降，屬正常；可以移除，但不能再加入 |
| 等級調降後少了幾個群組 | 屬正常：超過新群組數的群組不會顯示，資料保留，升級後會再出現 |
| `500 Internal server error` | 後端設定或 DB 問題 → 通知後端 |
