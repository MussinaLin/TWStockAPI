# 前端串接：自訂訊號（`/api/custom-signals`）

本文件給**前端**參考，說明如何串接 TWStockAPI 的「自訂訊號」設定：讀取、儲存、刪除每檔股票的多空條件，以及圖上標記的判斷規則。通知由另一個服務（SignalNotifier）用**同一套規則**發送。

- 登入與帶 token 的方式：見 [frontend-api-auth-integration.md](frontend-api-auth-integration.md)
- 完整 API 規格：見 TWStockAPI `README.md` 的 Custom Signals 段落
- Telegram 綁定：見 AccountService `docs/frontend-telegram-integration.md`

---

## 1. 功能概覽

| 項目 | 說明 |
|------|------|
| 用途 | 使用者針對個股設定多方 / 空方條件，條件成立時發 Telegram 通知，圖上也畫出標記 |
| 歸屬 | 每個使用者各自一份，每檔股票一筆設定 |
| 登入 | **一律需要登入**：不論 `JWT_TOKEN_ENABLE` 怎麼設，都必須帶 `Authorization: Bearer <token>` |
| 上限 | 個股數 `max_stocks`、每方條件數 `max_conditions_per_side` 依會員等級而定；**請一律用 GET 回傳的值，不要寫死在前端** |
| 儲存位置 | 後端（不再存 localStorage） |

---

## 2. 條件格式

### 2.1 單一條件

```json
{ "id": "uuid", "side": "long", "kind": "rsi", "period": 9, "op": "gt", "value": 50 }
```

- `id`：前端產生，1–64 字元字串，同一陣列內唯一。
- `side`：`long`（多方）/ `short`（空方）。
- `op`：`gt`（>）/ `lt`（<），**嚴格比較，相等不成立**。
- `id`、`side`、`kind` 所有 kind 都必填。`value` 必須是有限數字（不限範圍），`period` 必須是整數。
- 後端只保留下表列出的欄位，多餘的欄位會被忽略（不會存、也不會回傳）。

### 2.2 各 kind 的欄位

| kind | 意義 | 必填欄位 |
|---|---|---|
| `vol_ma` | 今日成交量 op N 日均量 × value 倍 | `period` ∈ {5,10,20}、`op`、`value` |
| `insti_avg` | 外資 / 投信 N 日平均買賣超 op value 張（賣超用負數） | `entity` ∈ {`foreign`,`trust`}、`period` ∈ {5,10,15,20,30}、`op`、`value` |
| `margin_balance` | 融資餘額 op value 張 | `op`、`value` |
| `short_balance` | 融券餘額 op value 張 | `op`、`value` |
| `short_margin_ratio` | 券資比 op value % | `op`、`value` |
| `rsi` | RSI N op value | `period` ∈ {9,14}、`op`、`value` |
| `macd_hist` | MACD 柱狀 op value | `op`、`value` |
| `bb_percent_b` | 布林 %b op value | `op`、`value` |
| `kd_k` | KD 的 K op value | `op`、`value` |
| `kd_cross` | KD 黃金 / 死亡交叉 | `cross` ∈ {`golden`,`death`} |
| `price_ma` | 收盤價在 N 日均線之上（`gt`）/ 之下（`lt`） | `period` ∈ {5,10,20}、`op` |

---

## 3. API 一覽

| Method | Path | 說明 |
|--------|------|------|
| `GET` | `/api/custom-signals` | 取得自己所有的設定與上限 |
| `GET` | `/api/custom-signals/:symbol` | 取得單一股票的設定 |
| `PUT` | `/api/custom-signals/:symbol` | 建立或覆寫單一股票的設定 |
| `DELETE` | `/api/custom-signals/:symbol` | 刪除單一股票的設定 |

Base URL 與環境同 [frontend-api-auth-integration.md §2](frontend-api-auth-integration.md#2-環境)。所有 request 都要帶 `Authorization: Bearer <token>`；`symbol` 放在 URL path，請用 `encodeURIComponent(symbol)` 編碼。

### 3.1 `GET /api/custom-signals`

**Response `200`：**

```json
{
  "max_stocks": 5,
  "max_conditions_per_side": 5,
  "items": [
    {
      "symbol": "2330",
      "name": "台積電",
      "enabled": true,
      "active": true,
      "conditions": [
        { "id": "a1", "side": "long", "kind": "rsi", "period": 9, "op": "gt", "value": 50 }
      ],
      "created_time": "2026-11-09T09:12:00Z",
      "updated_time": "2026-11-09T09:12:00Z"
    }
  ]
}
```

| 欄位 | 型別 | 說明 |
|------|------|------|
| `max_stocks` | number | 最多幾檔 |
| `max_conditions_per_side` | number | 多方、空方各自最多幾個條件 |
| `items` | array | 依 `created_time` 由舊到新，同時間依 `symbol`；沒有設定時是 `[]`，不會是 `null` |
| `items[].enabled` | boolean | 是否啟用 |
| `items[].active` | boolean | 是否在會員上限內（見 §4） |
| `items[].conditions` | array | 條件，格式見 §2 |
| `items[].created_time` / `updated_time` | string | RFC 3339 時間，請用 `new Date(...)` 解析 |

### 3.2 `GET /api/custom-signals/:symbol`

**Response `200`：** `{ "symbol", "enabled", "active", "conditions", "updated_time" }`。

沒有設定時回預設值：`{"symbol":"2330","enabled":false,"active":true,"conditions":[],"updated_time":null}`。

| HTTP | Body | 意義 |
|------|------|------|
| `404` | `{"error":"stock not found"}` | 股票不存在，或後端未啟用 |

### 3.3 `PUT /api/custom-signals/:symbol`

```
PUT /api/custom-signals/2330
Content-Type: application/json
Authorization: Bearer <token>

{ "enabled": true, "conditions": [ { "id": "a1", "side": "long", "kind": "kd_cross", "cross": "golden" } ] }
```

- `enabled` 必填（boolean）；`conditions` 是整份覆寫，不是合併。
- 存空陣列且 `enabled: false` 可以（等同「有設定但關閉」）；要完全移除請用 DELETE。

| HTTP | Body | 意義 |
|------|------|------|
| `200` | 同 §3.2 的格式 | 儲存成功；`conditions` 是後端正規化後的結果，請以它為準 |
| `400` | `{"error":"invalid request"}` | body 不是合法的 JSON、沒有 `enabled`、欄位型別錯誤（例如 `"period":"9"`、`"value":"50"`、`enabled` 不是 boolean）、數字超出範圍（例如 `1e400`），或 body 超過 64 KB |
| `400` | `{"error":"invalid conditions[i].field"}` | 第 `i` 個條件（從 0 開始）的 `field` 不合法；`field` 為 `id` / `side` / `kind` / `entity` / `period` / `op` / `value` / `cross` |
| `400` | `{"error":"enabled requires conditions"}` | `enabled: true` 但沒有任何條件 |
| `400` | `{"error":"too many conditions"}` | 多方或空方的條件數超過 `max_conditions_per_side` |
| `404` | `{"error":"stock not found"}` | 股票不存在，或後端未啟用 |
| `409` | `{"error":"custom signal limit reached","limit":5}` | 新增一檔會超過 `max_stocks`（覆寫既有的設定不受限）；`limit` 是目前上限 |
| `401` | `{"error":"token expired"}` / `{"error":"unauthorized"}` | 未登入、token 過期或無效，處理方式同 [auth 文件 §5](frontend-api-auth-integration.md#5-錯誤回應與處理) |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 |

### 3.4 `DELETE /api/custom-signals/:symbol`

| HTTP | Body | 意義 |
|------|------|------|
| `204` | （沒有 body） | 已刪除；**本來就沒有設定、股票不存在也回 `204`** |
| `401` | 同上 | 未登入、token 過期或無效 |
| `500` | `{"detail":"Internal server error"}` | 後端錯誤 |

**`204` 沒有 body，不要呼叫 `res.json()`。**

---

## 4. `active` 與 `enabled`

- `enabled`：使用者自己的開關。`enabled = false` 的設定**不畫標記**、也不通知。
- `active`：是否在會員上限內。同一個使用者的全部設定（**包括 `enabled = false`**）依 `created_time`、再依 `symbol` 排序，前 `max_stocks` 筆為 `active = true`。會員降級後，超過上限的設定會保留，但 `active = false`。
- `active = false` 時請提示「超過會員上限，未生效」，**且不畫標記**。使用者可以刪除其他設定，後面的設定會依序遞補成 `active = true`。
- **降級注意**：`max_conditions_per_side` 在每一次 PUT 都會檢查，包括只是切換 `enabled: false` 的請求。會員降級後，若某檔設定的條件數已超過新的上限，PUT 會回 `400 too many conditions`；使用者必須先刪減條件，或改用 DELETE 移除該設定。
- 新增、修改後請以 PUT 回應或重新 GET 的 `active` 為準，不要自己推算。

---

## 5. 判斷規則（圖上標記與通知共用）

以下 `T` = 該股某一天（要判斷的那一天），`P` = 該股在 `T` 之前最近一筆交易日（前一根 K 棒）。**任何用到的欄位為 `null` → 該條件不成立。** 對歷史上每一天都用同一套規則畫標記。

| kind | 判斷 |
|---|---|
| `vol_ma` | `volume op vol_ma{N} × value`；`vol_ma{N} ≤ 0` → 不成立 |
| `insti_avg` | `{foreign\|trust}_net_{N}d_avg op value` |
| `margin_balance` | `margin_balance op value` |
| `short_balance` | `short_balance op value` |
| `short_margin_ratio` | `short_margin_ratio × 100 op value`（API 回傳小數，0.2 = 20%，所以要先乘 100） |
| `rsi` | `rsi_{9\|14} op value` |
| `macd_hist` | `macd_hist op value` |
| `bb_percent_b` | `bb_percent_b op value` |
| `kd_k` | `kd_k op value` |
| `kd_cross` golden | `P.kd_k < P.kd_d` 且 `T.kd_k > T.kd_d`；death：`P.kd_k > P.kd_d` 且 `T.kd_k < T.kd_d`。沒有 `P` → 不成立（`kd_cross` 用**前一根 K 棒**） |
| `price_ma` | `close op ma{N}` |

- **嚴格比較**：`gt` / `lt` 相等時不成立。
- **精確求值**：每個運算式都以 IEEE double 照上表寫法求值，運算元順序不可更動——`vol_ma` 一律用 `vol_ma{N} × value`（不可改成 `volume / vol_ma`），`short_margin_ratio` 一律用 `short_margin_ratio × 100`（不可改成對 `value` 除以 100），這樣標記才會和通知在剛好等於門檻時一致。測試向量：`volume 63`、`vol_ma5 90`、`value 0.7`、`gt` → 成立（`63 > 62.99999999999999`）。
- **某一方**（多方或空方）：有至少一個條件**且全部成立** → 該方成立。**沒有條件 → 該方永不成立**（只設多方就不會出現空方訊號）。

### 欄位來源

欄位都來自 `/api/daily/stock/:symbol`：

- 外資 / 投信 N 日均**改用**回應裡的 `foreign_net_{N}d_avg` / `trust_net_{N}d_avg`（N ∈ 5、10、15、20、30），不要再自己累加。
- KD 與均線用新欄位：`kd_k`、`kd_d`、`ma5`、`ma10`、`ma20`。

---

## 6. 已知例外

「通知與圖上標記一致」指的是**發送當下一致**。以下兩種事後才出現的差異屬於已知例外，不會補發或撤回通知：

1. **融資券資料隔天修正**：`margin_balance`、`short_balance`、`short_margin_ratio` 當天寫入的是速報版，隔天會被最終版覆寫。用到這三種條件時，隔天之後圖上該日的標記可能與當初的通知不同。
2. **修改條件**：圖上的歷史標記一律用**目前的條件**重算，使用者改了條件後，過去發過的通知會與圖上的標記對不上。

---

## 7. 未綁定 Telegram 時的引導

通知只會發給已綁定 Telegram 的使用者。設定自訂訊號時若還沒綁定，前端應引導使用者去綁，但**不要擋住儲存**：沒綁定也能儲存、啟用，圖上的標記照畫，只是條件成立時不會收到通知。

### 7.1 判斷是否已綁定

綁定狀態在 **AccountService** 的 `GET /api/auth/me`（`user.telegram`），本服務的 `/api/custom-signals` 回應不含這個資訊。

| `user.telegram` | 意思 |
|---|---|
| `null` | 未綁定 |
| `{ "username": "@alice", "bound_at": "..." }` | 已綁定 |

**進入設定頁時重新呼叫一次 `/api/auth/me`，不要只用登入時存下來的 user。** 使用者在 Telegram 封鎖 bot 時，後端會立刻解除綁定，登入時快取的資料會過時。

```js
const ACCOUNT_API = "https://<account-service>";
// authHeaders、goToLogin 同 AccountService docs/frontend-telegram-integration.md §3.1

// 回傳 null（未綁定）或 { username, bound_at }
async function getTelegramBinding() {
  const res = await fetch(`${ACCOUNT_API}/api/auth/me`, { headers: authHeaders() });
  if (res.status === 401) return goToLogin();
  const { user } = await res.json();
  return user.telegram;
}
```

### 7.2 UI 建議

| 時機 | 未綁定 | 已綁定 |
|---|---|---|
| 進入設定頁 | 頁面上方常駐提示「尚未綁定 Telegram，條件成立時不會收到通知」＋「綁定 Telegram」按鈕 | 顯示「通知會發到 Telegram `@username`」 |
| 儲存成功（`enabled: true`） | 提示「設定已儲存。綁定 Telegram 才會收到通知」＋「立即綁定」/「稍後」 | 不需提示 |
| 按下綁定 | 在當頁開啟 Telegram 連結並輪詢 `/api/auth/me`，`telegram` 不為 `null` 後提示自動消失；**不要導離設定頁**，避免使用者正在編輯的條件遺失 | — |

- 綁定連結（`POST /api/telegram/bind-code`）、開窗方式與輪詢範例見 AccountService `docs/frontend-telegram-integration.md` §2、§3.1。
- `bind-code` 回 `503`（該環境未啟用 Telegram 綁定）時，隱藏綁定按鈕與上述提示。
- 「稍後」只是關掉這次的提示；頁面上方的常駐提示要等綁定完成才消失。

---

## 8. 前端需配合的事項

- 設定改用本文件的 API 讀寫，不再存 localStorage。
- 外資 / 投信 N 日均改用 `foreign_net_{N}d_avg` / `trust_net_{N}d_avg`。
- KD、`price_ma` 判斷用 `kd_k`、`kd_d`、`ma5`、`ma10`、`ma20`。
- `active = false` 提示「超過會員上限，未生效」且不畫標記；`enabled = false` 不畫標記。
- 在說明文字中告知 §6 的兩個已知例外。
- 未綁定 Telegram 時依 §7 引導使用者綁定（不擋儲存）；綁定按鈕與狀態細節見 AccountService 的 `docs/frontend-telegram-integration.md`。
