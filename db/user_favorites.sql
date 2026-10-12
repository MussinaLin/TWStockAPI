-- 我的最愛個股（TWStockAPI 擁有）；可重複執行
-- 全新環境：psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites.sql
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
