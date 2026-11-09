package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// RegisterCustomSignals mounts /custom-signals behind requireLogin; every
// handler needs the authenticated user.
func RegisterCustomSignals(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/custom-signals", requireLogin)
	g.GET("", listCustomSignals)
	g.GET("/:symbol", getCustomSignal)
	g.PUT("/:symbol", putCustomSignal)
	g.DELETE("/:symbol", deleteCustomSignal)
}

type customSignalDetail struct {
	Symbol      string          `json:"symbol"`
	Enabled     bool            `json:"enabled"`
	Active      bool            `json:"active"`
	Conditions  json.RawMessage `json:"conditions"`
	UpdatedTime *time.Time      `json:"updated_time"`
}

type putCustomSignalRequest struct {
	Enabled    *bool                `json:"enabled"`
	Conditions []rawSignalCondition `json:"conditions"`
}

// stockEnabled reports whether symbol exists in stocks with enabled = true.
func stockEnabled(ctx context.Context, q querier, symbol string) (bool, error) {
	if invalidSymbol(symbol) {
		return false, nil
	}
	var one int
	err := q.QueryRow(ctx, "SELECT 1 FROM stocks WHERE symbol = $1 AND enabled", symbol).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("custom signals: find stock: %w", err)
	}
	return true, nil
}

// signalActive 與 SignalNotifier 的規則一致：同 user 全部設定（含 enabled = false）依
// (created_time, symbol) 排序，排在它前面的設定數 < rule.Stocks 才算 active。
func signalActive(ctx context.Context, q querier, userID, symbol string, rule signalRule) (bool, error) {
	var ahead int
	err := q.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_stock_signals o
		JOIN user_stock_signals me ON me.user_id = o.user_id AND me.symbol = $2
		WHERE o.user_id = $1::uuid AND (o.created_time, o.symbol) < (me.created_time, me.symbol)`,
		userID, symbol).Scan(&ahead)
	if err != nil {
		return false, fmt.Errorf("custom signals: rank: %w", err)
	}
	return ahead < rule.Stocks, nil
}

// getCustomSignal 回傳單一股票的自訂訊號設定；沒有設定時回傳預設值。
func getCustomSignal(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("custom signals: no authenticated user"))
		return
	}
	symbol := c.Param("symbol")
	ctx := c.Request.Context()

	exists, err := stockEnabled(ctx, db.Pool(), symbol)
	if err != nil {
		favoritesServerError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock not found"})
		return
	}
	rule, err := loadSignalRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}

	detail := customSignalDetail{Symbol: symbol}
	var updated time.Time
	err = db.Pool().QueryRow(ctx,
		`SELECT enabled, conditions, updated_time FROM user_stock_signals
		WHERE user_id = $1::uuid AND symbol = $2`, user.ID, symbol).
		Scan(&detail.Enabled, &detail.Conditions, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		detail.Active = true
		detail.Conditions = json.RawMessage("[]")
		c.JSON(http.StatusOK, detail)
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: get: %w", err))
		return
	}
	detail.UpdatedTime = &updated
	detail.Active, err = signalActive(ctx, db.Pool(), user.ID, symbol, rule)
	if err != nil {
		favoritesServerError(c, err)
		return
	}
	c.JSON(http.StatusOK, detail)
}

// maxCustomSignalBody 是 PUT body 的大小上限（64 KB）。
const maxCustomSignalBody = 64 << 10

// putCustomSignal 建立或覆寫單一股票的自訂訊號設定。先以 FOR UPDATE 鎖住 user 的 row，
// 讓同一個 user 的請求排隊處理，避免同時新增時超過個股數上限。
func putCustomSignal(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("custom signals: no authenticated user"))
		return
	}
	symbol := c.Param("symbol")

	// 限制 body 大小，避免超大 payload 耗盡記憶體；超過上限視同 JSON 解析失敗。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCustomSignalBody)

	var req putCustomSignalRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	conds, err := normalizeConditions(req.Conditions)
	if err != nil {
		var ce *conditionError
		if errors.As(err, &ce) {
			c.JSON(http.StatusBadRequest, gin.H{"error": ce.Error()})
			return
		}
		favoritesServerError(c, err)
		return
	}
	if len(conds) == 0 && *req.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "enabled requires conditions"})
		return
	}
	condJSON, err := json.Marshal(conds)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: marshal conditions: %w", err))
		return
	}

	ctx := c.Request.Context()
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: begin: %w", err))
		return
	}
	defer tx.Rollback(ctx) // 已 Commit 時為 no-op

	rule, err := loadSignalRule(ctx, tx, selectMemberLevelForUpdate, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}
	long, short := countBySide(conds)
	if long > rule.ConditionsPerSide || short > rule.ConditionsPerSide {
		c.JSON(http.StatusBadRequest, gin.H{"error": "too many conditions"})
		return
	}

	exists, err := stockEnabled(ctx, tx, symbol)
	if err != nil {
		favoritesServerError(c, err)
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock not found"})
		return
	}

	var has bool
	var count int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(BOOL_OR(symbol = $2), false), COUNT(*)
		FROM user_stock_signals WHERE user_id = $1::uuid`, user.ID, symbol).Scan(&has, &count)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: count: %w", err))
		return
	}
	if !has && count >= rule.Stocks {
		c.JSON(http.StatusConflict, gin.H{"error": "custom signal limit reached", "limit": rule.Stocks})
		return
	}

	detail := customSignalDetail{Symbol: symbol}
	var updated time.Time
	err = tx.QueryRow(ctx,
		`INSERT INTO user_stock_signals (user_id, symbol, enabled, conditions)
		VALUES ($1::uuid, $2, $3, $4::jsonb)
		ON CONFLICT (user_id, symbol) DO UPDATE
		SET enabled = EXCLUDED.enabled, conditions = EXCLUDED.conditions, updated_time = NOW()
		RETURNING enabled, conditions, updated_time`,
		user.ID, symbol, *req.Enabled, string(condJSON)).Scan(&detail.Enabled, &detail.Conditions, &updated)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: upsert: %w", err))
		return
	}
	detail.UpdatedTime = &updated
	detail.Active, err = signalActive(ctx, tx, user.ID, symbol, rule)
	if err != nil {
		favoritesServerError(c, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: commit: %w", err))
		return
	}
	c.JSON(http.StatusOK, detail)
}

type customSignalItem struct {
	Symbol      string          `json:"symbol"`
	Name        string          `json:"name"`
	Enabled     bool            `json:"enabled"`
	Active      bool            `json:"active"`
	Conditions  json.RawMessage `json:"conditions"`
	CreatedTime time.Time       `json:"created_time"`
	UpdatedTime time.Time       `json:"updated_time"`
}

// listCustomSignals 列出 user 全部設定，依 (created_time, symbol) 排序；
// 第 i 筆（從 0 開始）active = i < Stocks，enabled = false 的設定也佔名額。
func listCustomSignals(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("custom signals: no authenticated user"))
		return
	}
	ctx := c.Request.Context()

	rule, err := loadSignalRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}

	rows, err := db.Pool().Query(ctx,
		`SELECT u.symbol, s.name, u.enabled, u.conditions, u.created_time, u.updated_time
		FROM user_stock_signals u
		JOIN stocks s ON s.symbol = u.symbol
		WHERE u.user_id = $1::uuid
		ORDER BY u.created_time, u.symbol`, user.ID)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: list: %w", err))
		return
	}
	defer rows.Close()
	items := []customSignalItem{}
	for rows.Next() {
		var it customSignalItem
		if err := rows.Scan(&it.Symbol, &it.Name, &it.Enabled, &it.Conditions, &it.CreatedTime, &it.UpdatedTime); err != nil {
			favoritesServerError(c, fmt.Errorf("custom signals: list: %w", err))
			return
		}
		it.Active = len(items) < rule.Stocks
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: list: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"max_stocks":              rule.Stocks,
		"max_conditions_per_side": rule.ConditionsPerSide,
		"items":                   items,
	})
}

// deleteCustomSignal 刪除單一股票的設定；不存在或 symbol 不合法也回 204。
func deleteCustomSignal(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("custom signals: no authenticated user"))
		return
	}
	symbol := c.Param("symbol")
	if invalidSymbol(symbol) {
		c.Status(http.StatusNoContent)
		return
	}
	_, err := db.Pool().Exec(c.Request.Context(),
		"DELETE FROM user_stock_signals WHERE user_id = $1::uuid AND symbol = $2", user.ID, symbol)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("custom signals: delete: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}
