-- 自訂訊號設定（TWStockAPI 擁有）；可重複執行。需先套用 db/user_favorites.sql（user_config）
INSERT INTO user_config (key, value, description) VALUES
  ('CUSTOM_SIGNAL_LIMITS',
   '{"0": {"stocks": 5, "conditions_per_side": 5}, "1": {"stocks": 20, "conditions_per_side": 10}}',
   '各 member_level 的自訂訊號個股數與每方條件數上限')
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS user_stock_signals (
    user_id      UUID        NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    symbol       VARCHAR(10) NOT NULL REFERENCES stocks(symbol) ON DELETE CASCADE,
    enabled      BOOLEAN     NOT NULL DEFAULT false,
    conditions   JSONB       NOT NULL DEFAULT '[]',
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, symbol)
);
