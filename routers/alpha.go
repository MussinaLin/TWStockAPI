package routers

import (
	"main/db"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterAlpha(rg *gin.RouterGroup) {
	g := rg.Group("/alpha")

	g.GET("/pick/latest", getLatestPick)
	g.GET("/pick/dates", listPickDates)
	g.GET("/pick/summary", getPickSummary)
	g.GET("/pick/stock/:symbol", getPickBySymbol)
	g.GET("/pick/:date", getPickByDate)

	g.GET("/sell/latest", getLatestSell)
	g.GET("/sell/summary", getSellSummary)
	g.GET("/sell/stock/:symbol", getSellBySymbol)
	g.GET("/sell/:date", getSellByDate)
}

// ── Pick endpoints ──

func getLatestPick(c *gin.Context) {
	mode := c.DefaultQuery("mode", "alpha")
	ctx := c.Request.Context()

	var tradeDate *string
	err := db.Pool().QueryRow(ctx,
		`SELECT MAX(trade_date)::text FROM alpha_pick WHERE mode = $1`, mode).Scan(&tradeDate)
	if err != nil || tradeDate == nil {
		c.JSON(http.StatusOK, gin.H{"trade_date": nil, "picks": []any{}})
		return
	}

	query := `SELECT p.symbol, p.trade_date, p.name, p.close, p.volume,
		p.vol_ma5, p.vol_ma10, p.vol_ma20,
		p.rsi_14, p.macd, p.macd_signal, p.macd_hist,
		p.bb_upper, p.bb_bandwidth, p.bb_percent_b,
		st.insti_net_5d_sum, st.insti_net_5d_avg,
		st.insti_net_10d_sum, st.insti_net_10d_avg,
		st.insti_net_15d_sum, st.insti_net_15d_avg,
		st.insti_net_30d_sum, st.insti_net_30d_avg,
		st.bb_bw_5d_avg, st.bb_bw_10d_avg,
		st.bb_bw_15d_avg, st.bb_bw_30d_avg,
		p.cond_insti, p.cond_insti_buy AS cond_insti_bullish, p.cond_rsi, p.cond_macd,
		p.cond_vol_ma10, p.cond_vol_ma20,
		p.cond_bb_narrow, p.cond_bb_near_upper, p.cond_turnover_surge,
		p.reasons
	FROM alpha_pick p
	LEFT JOIN stock_daily_statistics st
		ON st.symbol = p.symbol AND st.trade_date = p.trade_date
	WHERE p.trade_date = $1 AND p.mode = $2
	ORDER BY p.symbol`

	rows, err := db.Pool().Query(ctx, query, *tradeDate, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	picks, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if picks == nil {
		picks = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"trade_date": *tradeDate,
		"count":      len(picks),
		"picks":      picks,
	})
}

func listPickDates(c *gin.Context) {
	mode := c.DefaultQuery("mode", "alpha")
	limit := parseLimit(c, 30, 365)

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT DISTINCT trade_date FROM alpha_pick
		 WHERE mode = $1 ORDER BY trade_date DESC LIMIT $2`, mode, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
			return
		}
		dates = append(dates, d.Format(time.DateOnly))
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if dates == nil {
		dates = []string{}
	}
	c.JSON(http.StatusOK, dates)
}

func getPickSummary(c *gin.Context) {
	mode := c.DefaultQuery("mode", "alpha")

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT symbol,
		        (ARRAY_AGG(name ORDER BY trade_date DESC))[1] AS name,
		        COUNT(*)::int AS pick_count,
		        MIN(trade_date) AS first_date, MAX(trade_date) AS last_date
		 FROM alpha_pick WHERE mode = $1
		 GROUP BY symbol
		 ORDER BY pick_count DESC`, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	result, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if result == nil {
		result = []map[string]any{}
	}
	c.JSON(http.StatusOK, result)
}

func getPickBySymbol(c *gin.Context) {
	symbol := c.Param("symbol")
	mode := c.DefaultQuery("mode", "alpha")

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT trade_date, symbol, name, reasons
		 FROM alpha_pick
		 WHERE symbol = $1 AND mode = $2
		 ORDER BY trade_date DESC`, symbol, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	records, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if records == nil {
		records = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"symbol":  symbol,
		"count":   len(records),
		"records": records,
	})
}

func getPickByDate(c *gin.Context) {
	date := c.Param("date")
	mode := c.DefaultQuery("mode", "alpha")

	query := `SELECT p.symbol, p.trade_date, p.name, p.close, p.volume,
		p.rsi_14, p.macd_hist, p.bb_percent_b,
		st.insti_net_5d_sum, st.insti_net_5d_avg,
		st.insti_net_10d_sum, st.insti_net_10d_avg,
		st.insti_net_15d_sum, st.insti_net_15d_avg,
		st.insti_net_30d_sum, st.insti_net_30d_avg,
		p.cond_insti, p.cond_insti_buy AS cond_insti_bullish, p.cond_rsi, p.cond_macd,
		p.cond_vol_ma10, p.cond_vol_ma20,
		p.cond_bb_narrow, p.cond_bb_near_upper, p.cond_turnover_surge,
		p.reasons
	FROM alpha_pick p
	LEFT JOIN stock_daily_statistics st
		ON st.symbol = p.symbol AND st.trade_date = p.trade_date
	WHERE p.trade_date = $1 AND p.mode = $2
	ORDER BY p.symbol`

	rows, err := db.Pool().Query(c.Request.Context(), query, date, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	picks, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if picks == nil {
		picks = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"trade_date": date,
		"count":      len(picks),
		"picks":      picks,
	})
}

// ── Sell endpoints ──

func getLatestSell(c *gin.Context) {
	mode := c.DefaultQuery("mode", "sell")
	ctx := c.Request.Context()

	var tradeDate *string
	err := db.Pool().QueryRow(ctx,
		`SELECT MAX(trade_date)::text FROM alpha_sell WHERE mode = $1`, mode).Scan(&tradeDate)
	if err != nil || tradeDate == nil {
		c.JSON(http.StatusOK, gin.H{"trade_date": nil, "sells": []any{}})
		return
	}

	query := `SELECT sl.symbol, sl.trade_date, sl.name, sl.close, sl.volume,
		sl.vol_ma10,
		sl.rsi_14, sl.macd_hist, sl.bb_percent_b,
		st.foreign_net_5d_sum, st.foreign_net_5d_avg,
		st.foreign_net_10d_sum, st.foreign_net_10d_avg,
		st.foreign_net_15d_sum, st.foreign_net_15d_avg,
		st.foreign_net_30d_sum, st.foreign_net_30d_avg,
		st.trust_net_5d_sum, st.trust_net_5d_avg,
		st.trust_net_10d_sum, st.trust_net_10d_avg,
		st.trust_net_15d_sum, st.trust_net_15d_avg,
		st.trust_net_30d_sum, st.trust_net_30d_avg,
		sl.cond_foreign_sell, sl.cond_foreign_accel,
		sl.cond_trust_sell, sl.cond_trust_accel,
		sl.cond_high_black, sl.cond_price_up_vol_down,
		sl.cond_rsi_overbought, sl.cond_rsi_divergence,
		sl.cond_macd_turn_neg, sl.cond_macd_divergence,
		sl.cond_bb_below, sl.cond_macd_death_cross,
		sl.cond_margin_surge, sl.cond_turnover_surge,
		sl.cond_vol_surge_flat,
		sl.conditions_met, sl.reasons
	FROM alpha_sell sl
	LEFT JOIN stock_daily_statistics st
		ON st.symbol = sl.symbol AND st.trade_date = sl.trade_date
	WHERE sl.trade_date = $1 AND sl.mode = $2
	ORDER BY sl.conditions_met DESC, sl.symbol`

	rows, err := db.Pool().Query(ctx, query, *tradeDate, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	sells, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if sells == nil {
		sells = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"trade_date": *tradeDate,
		"count":      len(sells),
		"sells":      sells,
	})
}

func getSellSummary(c *gin.Context) {
	mode := c.DefaultQuery("mode", "sell")

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT symbol,
		        (ARRAY_AGG(name ORDER BY trade_date DESC))[1] AS name,
		        COUNT(*)::int AS sell_count,
		        MIN(trade_date) AS first_date, MAX(trade_date) AS last_date
		 FROM alpha_sell WHERE mode = $1
		 GROUP BY symbol
		 ORDER BY sell_count DESC`, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	result, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if result == nil {
		result = []map[string]any{}
	}
	c.JSON(http.StatusOK, result)
}

func getSellBySymbol(c *gin.Context) {
	symbol := c.Param("symbol")
	mode := c.DefaultQuery("mode", "sell")

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT trade_date, symbol, name, reasons
		 FROM alpha_sell
		 WHERE symbol = $1 AND mode = $2
		 ORDER BY trade_date DESC`, symbol, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	records, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if records == nil {
		records = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"symbol":  symbol,
		"count":   len(records),
		"records": records,
	})
}

func getSellByDate(c *gin.Context) {
	date := c.Param("date")
	mode := c.DefaultQuery("mode", "sell")

	query := `SELECT symbol, trade_date, name, close, volume, vol_ma10,
		rsi_14, macd_hist, bb_percent_b,
		conditions_met, reasons
	FROM alpha_sell
	WHERE trade_date = $1 AND mode = $2
	ORDER BY conditions_met DESC, symbol`

	rows, err := db.Pool().Query(c.Request.Context(), query, date, mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	defer rows.Close()

	sells, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
		return
	}
	if sells == nil {
		sells = []map[string]any{}
	}
	c.JSON(http.StatusOK, gin.H{
		"trade_date": date,
		"count":      len(sells),
		"sells":      sells,
	})
}
