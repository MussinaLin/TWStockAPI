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

const marketColumns = `trade_date, taiex_open, taiex_high, taiex_low, taiex_close,
	total_volume, margin_balance, margin_balance_change, foreign_net`

func listMarketDaily(c *gin.Context) {
	limit := parseLimit(c, 500, 1500)

	query := "SELECT " + marketColumns + ` FROM market_daily
		ORDER BY trade_date DESC LIMIT $1`

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

	query := "SELECT " + marketColumns + ` FROM market_daily
		WHERE trade_date = $1`

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
