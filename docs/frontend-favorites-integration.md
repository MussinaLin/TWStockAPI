# 前端串接：我的最愛（`/api/favorites`）

本文件給**前端**參考，說明如何串接 TWStockAPI 的「我的最愛個股」功能：查詢清單、加入、移除。

- 登入與帶 token 的方式：見 [frontend-api-auth-integration.md](frontend-api-auth-integration.md)（本文件沿用其中的 `apiFetch` / axios 設定，並在第 4 節補上這次需要的調整）
- 完整 API 規格：見 TWStockAPI `README.md` 的 Favorites 段落

---

## 1. 功能概覽

| 項目 | 說明 |
|------|------|
| 用途 | 使用者把個股加入「我的最愛」，方便快速選取 |
| 歸屬 | 每個使用者各自一份清單，只看得到、也只改得到自己的 |
| 登入 | **一律需要登入**：不論後端 `JWT_TOKEN_ENABLE` 開關怎麼設，都必須帶 `Authorization: Bearer <token>` |
| 數量上限 | 依會員等級（`member_level`）而定，由後端設定；**請一律用 GET 回傳的 `limit`，不要寫死在前端** |
| 排序 | 依加入時間，最新加入的排最前面（目前不支援自訂順序） |
| 可加入的股票 | 只能加入後端已啟用的股票（`stocks.enabled = true`）；不存在或未啟用回 `404` |

目前的上限設定（僅供參考，後端可以隨時調整）：

| `member_level` | 上限 |
|----------------|------|
| `0` | 10 |
| `1` | 15 |
| `2` | 20 |

---

## 2. API 一覽

| Method | Path | 說明 |
|--------|------|------|
| `GET` | `/api/favorites` | 取得自己的清單、上限與目前數量 |
| `POST` | `/api/favorites` | 加入一檔股票 |
| `DELETE` | `/api/favorites/:symbol` | 移除一檔股票 |

Base URL 與環境同 [frontend-api-auth-integration.md §2](frontend-api-auth-integration.md#2-環境)（staging：`https://twstockapi-staging.up.railway.app`）。

所有 request 都要帶：

```
Authorization: Bearer <token>
```

---

## 3. API 規格

### 3.1 `GET /api/favorites` — 取得清單

**Response `200`：**

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

| 欄位 | 型別 | 說明 |
|------|------|------|
| `limit` | number | 這個使用者可以加入的最大數量 |
| `count` | number | 目前清單的數量（等於 `items.length`） |
| `items` | array | 清單；依 `created_time` 由新到舊排序；**沒有資料時是 `[]`，不會是 `null`** |
| `items[].symbol` | string | 股票代號 |
| `items[].name` | string | 股票名稱 |
| `items[].created_time` | string | 加入時間，**含時區的 RFC 3339 字串**；可能是 `...Z` 或 `...+08:00`，請用 `new Date(created_time)` 解析，不要自己切字串 |

注意：

- 股票加入之後如果被後端停用，**仍然會出現在清單中**，可以正常移除。
- 後端調低上限時，`count` 可能大於 `limit`（例如 `12/10`）。這時可以移除，但不能再加入，直到數量低於上限。

### 3.2 `POST /api/favorites` — 加入一檔股票

**Request：**

```
POST /api/favorites
Content-Type: application/json
Authorization: Bearer <token>

{ "symbol": "2330" }
```

- `symbol` 前後的空白會被去掉（`" 2330 "` 等同 `"2330"`）。
- 代號要完全一致（例如 `00631L` 不能寫成 `00631l`）；建議直接用 `/api/stocks` 或清單回傳的 `symbol`。

**Responses：**

| HTTP | Body | 意義 |
|------|------|------|
| `201` | `{"symbol":"2330","name":"台積電","created_time":"..."}` | 加入成功，回傳新的那一筆 |
| `200` | `{"symbol":"2330","name":"台積電","created_time":"..."}` | **本來就已經在清單中**，回傳既有的那一筆（即使已經額滿也一樣）。前端當作成功處理即可 |
| `400` | `{"error":"invalid request"}` | body 不是合法的 JSON、沒有 `symbol`、`symbol` 不是字串，或去掉空白後是空字串 |
| `404` | `{"error":"stock not found"}` | 股票不存在，或後端未啟用 |
| `409` | `{"error":"favorite limit reached","limit":10}` | 已達上限；`limit` 是目前的上限 |
| `401` | `{"error":"token expired"}` / `{"error":"unauthorized"}` | 未登入、token 過期或無效，處理方式同 [auth 文件 §5](frontend-api-auth-integration.md#5-錯誤回應與處理) |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤（例如上限設定有誤） |

- **重複送出是安全的**：同一檔連點兩次，第二次回 `200`，不會產生重複資料。
- 同時送出多個加入請求時，後端保證不會超過上限，超過的那幾個會回 `409`。

### 3.3 `DELETE /api/favorites/:symbol` — 移除一檔股票

```
DELETE /api/favorites/2330
Authorization: Bearer <token>
```

| HTTP | Body | 意義 |
|------|------|------|
| `204` | （沒有 body） | 已移除；**本來就不在清單中也回 `204`** |
| `401` | 同上 | 未登入、token 過期或無效 |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 |

- 重複送出是安全的，不會出錯。
- `symbol` 放在 URL path，請用 `encodeURIComponent(symbol)` 編碼。
- **`204` 沒有 body，不要呼叫 `res.json()`**，否則會丟出解析錯誤（見第 4 節）。

---

## 4. 程式範例

### 4.1 調整 `apiFetch`（fetch 版）

[auth 文件 §4.1](frontend-api-auth-integration.md#41-fetch-封裝) 的 `apiFetch` 只處理 `GET`，用在我的最愛會有兩個問題：

1. `DELETE` 回 `204` 沒有 body，`res.json()` 會丟出錯誤。
2. 非 2xx 時只丟出 `TWStockAPI 409`，拿不到 body 裡的 `limit`。

建議改成下面這個版本。它與原本的用法相容（GET 仍然直接回傳 JSON）：

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

/** @returns {Promise<{limit: number, count: number, items: FavoriteItem[]}>} */
export function listFavorites() {
  return apiFetch("/api/favorites");
}

/**
 * 加入一檔股票。201（新加入）與 200（本來就在清單中）都會回傳該筆資料。
 * @returns {Promise<FavoriteItem>}
 */
export function addFavorite(symbol) {
  return apiFetch("/api/favorites", {
    method: "POST",
    body: JSON.stringify({ symbol }),
  });
}

/** 移除一檔股票；本來就不在清單中也算成功。 */
export async function removeFavorite(symbol) {
  await apiFetch(`/api/favorites/${encodeURIComponent(symbol)}`, { method: "DELETE" });
}

/**
 * @typedef {Object} FavoriteItem
 * @property {string} symbol
 * @property {string} name
 * @property {string} created_time  RFC 3339，例如 "2026-10-05T09:12:00Z"
 */
```

TypeScript 型別（參考）：

```ts
export interface FavoriteItem {
  symbol: string;
  name: string;
  created_time: string; // RFC 3339
}

export interface FavoriteList {
  limit: number;
  count: number;
  items: FavoriteItem[];
}

export interface FavoriteLimitError {
  error: "favorite limit reached";
  limit: number;
}
```

### 4.3 使用範例（與框架無關）

```js
import { AuthError, ApiError } from "./api";
import { listFavorites, addFavorite, removeFavorite } from "./favorites";

const state = { limit: 0, items: [] };

// 頁面載入、登入後：取一次清單
export async function loadFavorites() {
  const data = await listFavorites();
  state.limit = data.limit;
  state.items = data.items;
}

export function isFavorite(symbol) {
  return state.items.some((i) => i.symbol === symbol);
}

export function isFull() {
  return state.items.length >= state.limit;
}

export async function onAdd(symbol) {
  try {
    const item = await addFavorite(symbol);
    // 201 或 200 都放到最前面；先過濾掉同代號，避免 200 時重複
    state.items = [item, ...state.items.filter((i) => i.symbol !== item.symbol)];
  } catch (err) {
    if (err instanceof ApiError && err.status === 409) {
      state.limit = err.body.limit; // 以後端回傳的上限為準
      toast(`我的最愛已達上限（${err.body.limit} 檔）`);
    } else if (err instanceof ApiError && err.status === 404) {
      toast("這檔股票無法加入我的最愛");
    } else if (err instanceof AuthError) {
      redirectToLogin(); // 你們自己的導回登入頁邏輯
    } else {
      toast("加入失敗，請稍後再試");
    }
  }
}

export async function onRemove(symbol) {
  const before = state.items;
  state.items = state.items.filter((i) => i.symbol !== symbol); // 樂觀更新
  try {
    await removeFavorite(symbol);
  } catch (err) {
    state.items = before; // 失敗時還原
    if (err instanceof AuthError) {
      redirectToLogin();
    } else {
      toast("移除失敗，請稍後再試");
    }
  }
}
```

### 4.4 axios 版

沿用 [auth 文件 §4.2](frontend-api-auth-integration.md#42-axios) 的 `api` instance（已經會自動帶 token、統一處理 401）：

```js
import { api } from "./api";

export const listFavorites = () => api.get("/api/favorites").then((r) => r.data);

export const addFavorite = (symbol) =>
  api.post("/api/favorites", { symbol }).then((r) => r.data); // 201 或 200

export const removeFavorite = (symbol) =>
  api.delete(`/api/favorites/${encodeURIComponent(symbol)}`); // 204，r.data 為 ""

// 錯誤處理
try {
  await addFavorite("2330");
} catch (err) {
  const { status, data } = err.response ?? {};
  if (status === 409) toast(`我的最愛已達上限（${data.limit} 檔）`);
  else if (status === 404) toast("這檔股票無法加入我的最愛");
  else if (status !== 401) toast("加入失敗，請稍後再試"); // 401 已由 interceptor 處理
}
```

axios 會自動帶 `Content-Type: application/json`，`204` 也不會解析失敗，不需要額外調整。

---

## 5. UI 建議

| 情境 | 建議做法 |
|------|----------|
| 未登入 | 不要呼叫 `/api/favorites`（一定回 `401`）；星號按鈕改成「登入後使用」或點了導去登入 |
| 進入頁面 / 登入後 | 呼叫一次 `GET /api/favorites`，用結果決定每檔股票的星號狀態 |
| 顯示數量 | 用 `items.length` / `limit` 顯示，例如「2 / 10」 |
| 已額滿 | `items.length >= limit` 時把「加入」按鈕設成不可按，並提示上限；仍然可以移除 |
| 加入 / 移除進行中 | 把按鈕暫時設成不可按，避免連點（後端已保證連點不會出錯，這只是讓畫面比較穩定） |
| 加入成功 | 直接用回傳的那一筆更新畫面，不需要重新 GET |
| 收到 `409` | 用回傳的 `limit` 更新畫面上的上限（使用者等級可能剛被調整） |
| 會員等級變更 | 重新 GET 一次，取得新的 `limit` |
| 被停用的股票 | 仍然會出現在清單中；如果點進去沒有行情資料屬正常現象，可以讓使用者移除 |
| 顯示時間 | `new Date(item.created_time).toLocaleString("zh-TW")` |

---

## 6. CORS

`POST` 與 `DELETE` 已加入允許清單（見 [auth 文件 §6](frontend-api-auth-integration.md#6-cors)）。

- `POST` 帶 `Content-Type: application/json` 與 `Authorization`，`DELETE` 帶 `Authorization`，瀏覽器都會先送 `OPTIONS` preflight，後端回 `204` 屬正常現象。
- 本機開發同樣要透過 dev server proxy（`localhost` 不在允許清單內）。

---

## 7. 測試方式

```bash
BASE=https://twstockapi-staging.up.railway.app
TOKEN=<token>

# 取得清單
curl -H "Authorization: Bearer $TOKEN" $BASE/api/favorites

# 加入
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"symbol":"2330"}' -w '\nHTTP %{http_code}\n' $BASE/api/favorites

# 移除
curl -X DELETE -H "Authorization: Bearer $TOKEN" -w 'HTTP %{http_code}\n' $BASE/api/favorites/2330
```

取得測試用 token 的方式見 [auth 文件 §7](frontend-api-auth-integration.md#7-測試方式)。

---

## 8. 常見問題排查

| 症狀 | 可能原因 |
|------|----------|
| 其他 API 不帶 token 都正常，只有 `/api/favorites` 回 `401` | 正常：我的最愛**一律需要登入**，不受後端開關影響 |
| `DELETE` 成功了但前端拋出 JSON 解析錯誤 | `204` 沒有 body，不要呼叫 `res.json()`（見 §4.1） |
| `POST` 回 `400 invalid request` | body 沒有 `JSON.stringify`（送成 `[object Object]`）、沒有 `symbol` 欄位、`symbol` 送成數字（例如 `2330` 而不是 `"2330"`） |
| `POST` 回 `404 stock not found`，但股票確實存在 | 該股票後端未啟用，或代號大小寫不一致 |
| `POST` 回 `409`，但畫面上數量還沒到上限 | 前端的 `limit` 過期了（例如會員等級被調整）→ 重新 GET |
| 清單數量大於上限 | 後端調低了上限，屬正常；使用者可以移除，但不能再加入 |
| `500 Internal server error` | 後端設定或 DB 問題 → 通知後端 |
