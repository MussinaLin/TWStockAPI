package routers

import (
	"main/db"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterMarket(rg *gin.RouterGroup) {
	g := rg.Group("/market")
	g.GET("", listMarketDaily)
	g.GET("/dates", listMarketDates)
	g.GET("/:date", getMarketByDate)
}

// market_daily_signal also has taiex_close, so market_daily columns are
// qualified with m. (trade_date is shared through USING).
const marketColumns = `trade_date, m.taiex_open, m.taiex_high, m.taiex_low, m.taiex_close,
	m.total_volume, m.margin_balance, m.margin_balance_change, m.foreign_net,
	m.txf_foreign_oi_net, m.txf_total_oi_net, m.tw_vix_close, m.day_trade_sell_amount_pct,
	COALESCE(s.sig_bottom OR s.sig_retail_exit, false) AS sig_bottom,
	COALESCE(s.sig_top, false) AS sig_top`

// LEFT JOIN keeps market_daily rows that have no signal row yet; their
// sig_bottom / sig_top fall back to false.
const marketFrom = `market_daily m LEFT JOIN market_daily_signal s USING (trade_date)`

func listMarketDaily(c *gin.Context) {
	limit := parseLimit(c, 500, 1500)

	query := "SELECT " + marketColumns + " FROM " + marketFrom +
		" ORDER BY trade_date DESC LIMIT $1"

	rows, err := db.Pool().Query(c.Request.Context(), query, limit)
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

func listMarketDates(c *gin.Context) {
	limit := parseLimit(c, 500, 1500)

	rows, err := db.Pool().Query(c.Request.Context(),
		`SELECT trade_date FROM market_daily
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

func getMarketByDate(c *gin.Context) {
	date := c.Param("date")

	query := "SELECT " + marketColumns + " FROM " + marketFrom +
		" WHERE trade_date = $1"

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
	if len(result) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.JSON(http.StatusOK, result[0])
}
