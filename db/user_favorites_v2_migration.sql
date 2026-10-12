-- 我的最愛 v1（單一清單）→ v2（群組）遷移
-- ⚠ 會刪除所有我的最愛資料。只在套用過 v1 的環境執行一次（目前只有 staging）。
-- 執行方式（在 repo 根目錄）：psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f db/user_favorites_v2_migration.sql
BEGIN;
DROP TABLE IF EXISTS user_favorite_stocks;
DELETE FROM user_config WHERE key = 'FAVORITE_STOCKS_LIMIT';
\ir user_favorites.sql
COMMIT;
