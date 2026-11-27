# 前端串接：大盤資料（`/api/market`）新增欄位

本文件給**前端**參考，說明 `/api/market` 與 `/api/market/:date` 新增的欄位：

- 台指期未平倉、台指 VIX、當沖賣出金額佔比（commit `6d55f8f`）
- 大盤底部 / 頭部訊號（commit `6118378`）

舊欄位的格式、排序、參數都沒有變，既有程式不用改。

- 登入與帶 token 的方式：見 [frontend-api-auth-integration.md](frontend-api-auth-integration.md)
- 完整 API 規格：見 TWStockAPI `README.md` 的 Market Daily 段落

---

## 1. 新增欄位一覽

| 欄位 | 型別 | 說明 | 可能為 `null` |
|------|------|------|:---:|
| `txf_foreign_oi_net` | `number`（整數） | 台指期**外資**未平倉淨口數，單位：口 | 是 |
| `txf_total_oi_net` | `number`（整數） | 台指期**三大法人合計**未平倉淨口數，單位：口 | 是 |
| `tw_vix_close` | `number` | 台指 VIX 收盤 | 是 |
| `day_trade_sell_amount_pct` | `number` | 當沖賣出金額佔比，**比率**（`0.2804` = 28.04%） | 是 |
| `sig_bottom` | `boolean` | 大盤底部訊號 | 否 |
| `sig_top` | `boolean` | 大盤頭部訊號 | 否 |

---

## 2. API 一覽

| Method | Path | 新欄位 |
|--------|------|:---:|
| `GET` | `/api/market?limit=N` | 有 |
| `GET` | `/api/market/:date` | 有 |
| `GET` | `/api/market/dates` | 不變（只回日期陣列） |

- Base URL 與環境同 [frontend-api-auth-integration.md §2](frontend-api-auth-integration.md#2-環境)。
- 登入規則跟其他 `/api` 一樣：後端開啟 JWT 驗證（`JWT_TOKEN_ENABLE`）時要帶 `Authorization: Bearer <token>`，關閉時不用。和 favorites / custom-signals 不同，這組**不是**一律需要登入。
- `GET /api/market`：依 `trade_date` **由新到舊**排序；`limit` 預設 `500`，最大 `1500`。畫圖時如果需要由舊到新，請在前端反轉陣列。
- `GET /api/market/:date`：`date` 用 `YYYY-MM-DD`；該日沒有資料回 `404 {"error":"not found"}`。

---

## 3. Response 範例

以下是 staging 的實際資料。

### 3.1 `GET /api/market?limit=2`

```json
[
  {
    "trade_date": "2026-11-26",
    "taiex_open": 38435.99,
    "taiex_high": 39459.95,
    "taiex_low": 38435.99,
    "taiex_close": 39151.81,
    "total_volume": 845336632675,
    "margin_balance": 434484476000,
    "margin_balance_change": 346489000,
    "foreign_net": -49181133625,
    "txf_foreign_oi_net": -106275,
    "txf_total_oi_net": -3765,
    "tw_vix_close": 55.18,
    "day_trade_sell_amount_pct": 0.4725,
    "sig_bottom": true,
    "sig_top": false
  },
  {
    "trade_date": "2026-11-25",
    "taiex_open": 39983.91,
    "taiex_high": 39983.91,
    "taiex_low": 36681.9,
    "taiex_close": 38195.43,
    "total_volume": 1229761968894,
    "margin_balance": 434137987000,
    "margin_balance_change": -19367026000,
    "foreign_net": -123654798153,
    "txf_foreign_oi_net": -105709,
    "txf_total_oi_net": -5442,
    "tw_vix_close": 58.38,
    "day_trade_sell_amount_pct": 0.4616,
    "sig_bottom": true,
    "sig_top": false
  }
]
```

### 3.2 `GET /api/market/2026-10-14`（頭部訊號的一天）

```json
{
  "trade_date": "2026-10-14",
  "taiex_open": 48382.64,
  "taiex_high": 49059.12,
  "taiex_low": 48382.64,
  "taiex_close": 48625.83,
  "total_volume": 978137416296,
  "margin_balance": 651556359000,
  "margin_balance_change": 6577734000,
  "foreign_net": -20037340628,
  "txf_foreign_oi_net": -86814,
  "txf_total_oi_net": -6083,
  "tw_vix_close": 26.08,
  "day_trade_sell_amount_pct": 0.3929,
  "sig_bottom": false,
  "sig_top": true
}
```

### 3.3 TypeScript 型別

```ts
interface MarketDaily {
  trade_date: string;                       // "YYYY-MM-DD"
  taiex_open: number | null;
  taiex_high: number | null;
  taiex_low: number | null;
  taiex_close: number | null;
  total_volume: number | null;
  margin_balance: number | null;
  margin_balance_change: number | null;
  foreign_net: number | null;
  // 6d55f8f 新增
  txf_foreign_oi_net: number | null;        // 口
  txf_total_oi_net: number | null;          // 口
  tw_vix_close: number | null;
  day_trade_sell_amount_pct: number | null; // 比率，0.2804 = 28.04%
  // 6118378 新增
  sig_bottom: boolean;
  sig_top: boolean;
}
```

---

## 4. 欄位說明

### 4.1 期貨未平倉 / VIX / 當沖（`6d55f8f`）

- 四個欄位都**照 DB 原值回傳**，後端不換算。
- `txf_foreign_oi_net`、`txf_total_oi_net`：淨口數 = 多單 − 空單，**負數代表淨空單**（例如 `-106275` = 外資淨空 106,275 口）。
- `day_trade_sell_amount_pct` 是**比率**，顯示成百分比時要自己 × 100：`(0.4725 * 100).toFixed(2) + "%"` → `47.25%`。
- 某天資料缺漏時欄位是 `null`，**不是** `0`，也不會少掉 key。請顯示成「—」之類，不要當成 0 畫進圖表。

### 4.2 大盤底部 / 頭部訊號（`6118378`）

| 欄位 | 為 `true` 的條件（來自 `market_daily_signal`） |
|------|------|
| `sig_bottom` | `sig_bottom` **或** `sig_retail_exit` 任一為 true |
| `sig_top` | `sig_top` 為 true |

- 訊號由 TWStockAnalysis 每日批次計算，API 只負責轉出來；前端不需要、也拿不到個別條件。
- 一定是 `true` / `false`，**不會是 `null`**。
- 該日如果還沒有訊號資料，兩個欄位都回 `false`。所以 `false` 可能是「沒有訊號」，也可能是「訊號還沒算出來」，前端無法區分。
- **最新一天可能從 `false` 變成 `true`**：大盤資料和訊號由不同批次寫入，訊號批次還沒跑完時，最新一天會先顯示 `false`，跑完後才更新。如果前端有快取，最新一天不要快取太久。
- 歷史資料裡，同一天 `sig_bottom` 與 `sig_top` 不會同時為 `true`，但後端沒有保證這件事；畫圖時請兩個都處理，不要用 if / else 二選一。
- 訊號是稀疏事件（staging 2022-01-03 ~ 2026-11-26 共 1,187 個交易日中，底部 46 天、頭部 119 天），適合在 K 線圖上用標記點呈現，不適合畫成連續線。

---

## 5. 錯誤回應

| 狀況 | Status | Body |
|------|--------|------|
| `/api/market/:date` 該日沒有資料 | `404` | `{"error":"not found"}` |
| `/api/market/:date` 日期格式錯誤（例如 `abc`） | `500` | `{"detail":"Internal server error"}` |
| JWT 驗證開啟、token 過期 | `401` | `{"error":"token expired"}` |
| JWT 驗證開啟、沒帶 token 或 token 無效 | `401` | `{"error":"unauthorized"}` |

日期格式錯誤目前回 `500` 而不是 `400`（整個專案都是這樣）。請在前端先檢查 `YYYY-MM-DD` 格式再打 API。

---

## 6. 前端需配合的事項

1. `MarketDaily` 型別加上六個新欄位（見 §3.3）。
2. 四個數值欄位要處理 `null`，不要當成 0。
3. `day_trade_sell_amount_pct` 顯示百分比時要 × 100。
4. K 線圖在 `sig_bottom` / `sig_top` 為 `true` 的日期加上標記；兩個可能同時為 `true`。
5. **上線過渡期**：後端某個環境還沒部署新版時，回應裡不會有這些欄位（值是 `undefined`）。建議讀取時加上預設值，例如 `m.sig_bottom ?? false`、`m.tw_vix_close ?? null`，避免前端比後端先上線時出錯。各環境的部署狀態請向後端確認。
