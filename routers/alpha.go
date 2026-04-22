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

	query := `SELECT symbol, trade_date, name, close, volume,
		rsi_14, macd_hist, bb_percent_b
	FROM alpha_pick
	WHERE trade_date = $1 AND mode = $2
	ORDER BY symbol`

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
		`SELECT trade_date, symbol, name
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

	query := `SELECT symbol, trade_date, name, close, volume,
		rsi_14, macd_hist, bb_percent_b
	FROM alpha_pick
	WHERE trade_date = $1 AND mode = $2
	ORDER BY symbol`

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

	query := `SELECT symbol, trade_date, name, close, volume,
		rsi_14, macd_hist, bb_percent_b
	FROM alpha_sell
	WHERE trade_date = $1 AND mode = $2
	ORDER BY conditions_met DESC, symbol`

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
		`SELECT trade_date, symbol, name
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

	query := `SELECT symbol, trade_date, name, close, volume,
		rsi_14, macd_hist, bb_percent_b
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
