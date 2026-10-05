# 前端串接：呼叫 TWStockAPI（帶 JWT token）

本文件給**前端**參考，說明登入之後，如何把 token 帶進 TWStockAPI 的每一個 request。

- 登入、取得 token：見 AccountService repo 的 `docs/frontend-login-integration.md`
- 本文件：拿到 token 之後，怎麼呼叫 TWStockAPI

---

## 1. 流程總覽

```
┌─────────┐   1. POST /api/auth/google              ┌────────────────┐
│  前端    │ ──────────────────────────────────────▶ │ AccountService │
│ (SPA)   │   2. 回傳 { token, user }                └────────────────┘
│         │ ◀──────────────────────────────────────
│         │      localStorage.setItem("token", token)
│         │
│         │   3. GET /api/...                        ┌────────────────┐
│         │      Authorization: Bearer <token>        │   TWStockAPI   │
│         │ ──────────────────────────────────────▶ │                │
│         │   4. 200 資料                             │                │
│         │      或 401 → 清掉 token、重新登入         │                │
│         │ ◀────────────────────────────────────── └────────────────┘
└─────────┘
```

重點：
- TWStockAPI 用的 token 就是 AccountService 登入後回傳的 `token`，不需要另外申請。
- **每一個** `/api/*` request 都要帶 `Authorization` header。
- `/health`、`/health/db` 不需要 token。

---

## 2. 環境

| 環境 | TWStockAPI Base URL |
|------|---------------------|
| Staging（dev） | `https://twstockapi-staging.up.railway.app` |
| Production | 向後端確認 |

Token 不能跨環境使用：staging 的 TWStockAPI 要搭配 staging 的 AccountService 登入拿到的 token。

---

## 3. Header 格式

```
Authorization: Bearer <token>
```

- `Bearer` 和 token 之間**一個空格**。
- Token 前後不要加引號，中間不能有換行或空白。
- **沒有 token（未登入）時不要送這個 header**，避免送出 `Bearer null` 或 `Bearer undefined`。
- Token 是登入憑證：不要放進 URL query、不要印到 log。

| 實際送出的 header | 結果 |
|-------------------|------|
| `Authorization: Bearer eyJhbGci...` | ✅ 200 |
| `Authorization: eyJhbGci...`（少了 `Bearer`） | ❌ 401 `unauthorized` |
| `Authorization: Bearer Bearer eyJhbGci...`（多了一個 `Bearer`） | ❌ 401 `unauthorized` |
| `Authorization: Bearer "eyJhbGci..."`（多了引號） | ❌ 401 `unauthorized` |
| `Authorization: Bearer null` | ❌ 401 `unauthorized` |

`Bearer` 不分大小寫，但建議統一寫成 `Bearer`。

---

## 4. 程式範例

以下範例假設 token 存在 `localStorage` 的 `token` key（與 AccountService 串接文件相同）。

### 4.1 fetch 封裝

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

export async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers);
  const token = localStorage.getItem("token");
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  const res = await fetch(`${API_BASE}${path}`, { ...options, headers });

  if (res.status === 401) {
    const body = await res.json().catch(() => ({}));
    localStorage.removeItem("token");
    throw new AuthError(body.error ?? "unauthorized");
  }
  if (!res.ok) {
    throw new Error(`TWStockAPI ${res.status}`);
  }
  return res.json();
}
```

使用方式：

```js
try {
  const stocks = await apiFetch("/api/stocks");
  const picks = await apiFetch("/api/alpha/pick/latest?mode=alpha");
} catch (err) {
  if (err instanceof AuthError) {
    if (err.reason === "token expired") {
      alert("登入已過期，請重新登入");
    }
    redirectToLogin(); // 你們自己的導回登入頁邏輯
    return;
  }
  throw err;
}
```

### 4.2 axios

```js
import axios from "axios";

export const api = axios.create({
  baseURL: "https://twstockapi-staging.up.railway.app",
});

// 每個 request 自動帶 token
api.interceptors.request.use((config) => {
  const token = localStorage.getItem("token");
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// 401 統一處理
api.interceptors.response.use(
  (res) => res,
  (err) => {
    if (err.response?.status === 401) {
      const reason = err.response.data?.error; // "token expired" | "unauthorized"
      localStorage.removeItem("token");
      if (reason === "token expired") {
        alert("登入已過期，請重新登入");
      }
      redirectToLogin(); // 你們自己的導回登入頁邏輯
    }
    return Promise.reject(err);
  },
);
```

使用方式：

```js
const { data: stocks } = await api.get("/api/stocks");
```

---

## 5. 錯誤回應與處理

| HTTP | Body | 意義 | 前端處理 |
|------|------|------|----------|
| `401` | `{"error":"token expired"}` | Token 已過期（效期依 AccountService 設定，預設 7 天） | 清掉 token，提示「登入已過期」，導回登入 |
| `401` | `{"error":"unauthorized"}` | 沒帶 token、格式錯誤、token 無效，或帳號不存在 | 清掉 token，導回登入 |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 | 顯示錯誤訊息，通知後端 |

- 目前**沒有 refresh token**：過期後只能重新走一次 Google 登入。
- 其他狀態碼（例如 `404 {"error":"not found"}`）與原本相同，詳見 TWStockAPI `README.md`。

---

## 6. CORS

TWStockAPI 目前允許的跨域設定：

| 項目 | 值 |
|------|----|
| Origins | `https://darvishkzone.com`、`https://dev.darvishkzone.com` |
| Methods | `GET`、`POST`、`DELETE` |
| Headers | `Origin`、`Content-Type`、`Authorization` |

- 帶 `Authorization` header 時，瀏覽器會先送一個 `OPTIONS` preflight，後端回 `204` 屬正常現象。
- **`localhost` 不在允許清單內。** 本機開發請用 dev server proxy 轉發到 TWStockAPI，或請後端把你的 origin 加進清單。

Vite proxy 範例（用 `/twstock` 前綴，避免和 AccountService 的 `/api/auth` 路徑衝突）：

```js
// vite.config.js
export default {
  server: {
    proxy: {
      "/twstock": {
        target: "https://twstockapi-staging.up.railway.app",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/twstock/, ""),
      },
    },
  },
};
```

本機開發時把 `API_BASE`（或 axios 的 `baseURL`）設為 `"/twstock"`；proxy 會原樣轉發 `Authorization` header。

---

## 7. 測試方式

### curl

```bash
curl -H "Authorization: Bearer <token>" https://twstockapi-staging.up.railway.app/api/stocks
```

### Postman

1. Authorization 分頁 → Type 選 **Bearer Token**（不要選 JWT Bearer，那是讓 Postman 自己簽 token）。
2. Token 欄位**只貼 token 本身**，不要加 `Bearer ` 前綴。
3. 想確認實際送出的 header：開 Postman Console（左下角），看 Request Headers 的 `Authorization`。

### 取得測試用 token

- 在網站上用 Google 登入後，打開瀏覽器 DevTools → Application → Local Storage，複製 `token` 的值。
- 或向後端索取測試用 token。

---

## 8. 常見問題排查

| 症狀 | 可能原因 |
|------|----------|
| 有帶 token 但一直 `401 unauthorized` | Header 格式錯（見第 3 節）、token 沒複製完整、送成 `Bearer null`、用了其他環境的 token |
| `401 token expired` | Token 超過效期 → 重新登入 |
| 瀏覽器 console 出現 CORS 錯誤 | 前端 origin 不在允許清單（見第 6 節） |
| 未登入時所有 `/api/*` 都 401 | 後端已啟用驗證，屬預期行為；請先登入 |

---

## 9. 後端開關（參考）

後端可以用 DB `config` 表的 `JWT_TOKEN_ENABLE` 開關驗證；關閉時不檢查 token。前端**一律照常帶 token** 即可，開關切換不需要改前端。目前 staging 已啟用驗證。

**例外：`/api/favorites/*`（我的最愛）一律需要 token**，即使開關關閉也一樣；沒帶或 token 無效時回 `401`，處理方式同 §5。API 規格見 TWStockAPI `README.md` 的 Favorites 段落。
