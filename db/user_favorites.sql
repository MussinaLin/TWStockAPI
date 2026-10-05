-- 我的最愛個股（TWStockAPI 擁有）；可重複執行
-- 部署前在 .env 指向的 DB 執行：psql "$DATABASE_URL" -f db/user_favorites.sql

-- user 相關設定（通用 key/value；value 為 JSONB）
CREATE TABLE IF NOT EXISTS user_config (
    key          VARCHAR(50) PRIMARY KEY,
    value        JSONB       NOT NULL,
    description  TEXT        NOT NULL DEFAULT '',
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 各 member_level 的我的最愛上限；JSON 的 key 為 member_level 的字串
INSERT INTO user_config (key, value, description) VALUES
  ('FAVORITE_STOCKS_LIMIT', '{"0": 10, "1": 15, "2": 20}', '各 member_level 的我的最愛個股上限')
ON CONFLICT (key) DO NOTHING;

-- user 的我的最愛個股
CREATE TABLE IF NOT EXISTS user_favorite_stocks (
    user_id      UUID        NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    symbol       VARCHAR(10) NOT NULL REFERENCES stocks(symbol) ON DELETE CASCADE,
    created_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, symbol)
);
