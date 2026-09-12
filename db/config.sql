-- JWT 驗證開關（config 表由 TWStockAnalysis 擁有）
-- value 為 'true'（不分大小寫）才啟用；其他值或沒有這一列都視為關閉
INSERT INTO config (key, value) VALUES ('JWT_TOKEN_ENABLE', 'false')
ON CONFLICT (key) DO NOTHING;
