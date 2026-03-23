package routers

import (
	"main/db"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterDaily(rg *gin.RouterGroup) {
	g := rg.Group("/daily")
	g.GET("/dates", listDailyDates)
	g.GET("/:date", getDailyByDate)
	g.GET("/stock/:symbol", getStockHistory)
}

func listDailyDates(c *gin.Context) {
	limit := parseLimit(c, 30, 365)

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT DISTINCT trade_date FROM stock_daily_raw
		 ORDER BY trade_date DESC LIMIT $1`, limit)
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

const dailyColumns = `r.symbol, r.name, r.open, r.close, r.high, r.low, r.volume,
	r.turnover_rate, r.foreign_net, r.trust_net, r.dealer_net,
	r.institutional_investors_net,
	r.margin_balance, r.short_balance, r.short_margin_ratio,
	r.foreign_holding_pct, r.insti_holding_pct,
	st.vol_ma5, st.vol_ma10, st.vol_ma20, st.turnover_ma20,
	st.foreign_net_5d_avg, st.foreign_net_10d_avg,
	st.foreign_net_15d_avg, st.foreign_net_30d_avg,
	i.rsi_9, i.rsi_14,
	i.macd, i.macd_signal, i.macd_hist,
	i.bb_upper, i.bb_middle, i.bb_lower,
	i.bb_percent_b, i.bb_bandwidth`

const dailyJoins = `FROM stock_daily_raw r
	LEFT JOIN stock_daily_indicators i USING (symbol, trade_date)
	LEFT JOIN stock_daily_statistics st USING (symbol, trade_date)`

func getDailyByDate(c *gin.Context) {
	date := c.Param("date")

	query := "SELECT " + dailyColumns + " " + dailyJoins + `
		WHERE r.trade_date = $1 ORDER BY r.symbol`

	rows, err := db.Pool().Query(c.Request.Context(), query, date)
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

func getStockHistory(c *gin.Context) {
	symbol := c.Param("symbol")
	limit := parseLimit(c, 60, 365)

	historyColumns := `r.trade_date, r.open, r.close, r.high, r.low, r.volume,
		r.turnover_rate, r.foreign_net, r.trust_net, r.dealer_net,
		r.institutional_investors_net,
		r.margin_balance, r.short_balance, r.short_margin_ratio,
		st.vol_ma5, st.vol_ma10, st.vol_ma20, st.turnover_ma20,
		st.foreign_net_5d_avg, st.foreign_net_10d_avg,
		st.foreign_net_15d_avg, st.foreign_net_30d_avg,
		i.rsi_9, i.rsi_14,
		i.macd, i.macd_signal, i.macd_hist,
		i.bb_upper, i.bb_middle, i.bb_lower,
		i.bb_percent_b, i.bb_bandwidth`

	query := "SELECT " + historyColumns + " " + dailyJoins + `
		WHERE r.symbol = $1 ORDER BY r.trade_date DESC LIMIT $2`

	rows, err := db.Pool().Query(c.Request.Context(), query, symbol, limit)
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

func parseLimit(c *gin.Context, defaultVal, maxVal int) int {
	limitStr := c.DefaultQuery("limit", strconv.Itoa(defaultVal))
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		return defaultVal
	}
	if limit > maxVal {
		return maxVal
	}
	return limit
}
