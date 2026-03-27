package routers

import (
	"main/db"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterTrade(rg *gin.RouterGroup) {
	g := rg.Group("/trade")

	g.GET("/trade-records", getTradeRecords)
}

func getTradeRecords(c *gin.Context) {
	now := time.Now()
	fromStr := c.DefaultQuery("from", now.AddDate(0, 0, -90).Format(time.DateOnly))
	toStr := c.DefaultQuery("to", now.Format(time.DateOnly))

	query := `SELECT symbol, name, type, trade_date, price, performance
		FROM trade_records
		WHERE trade_date BETWEEN $1 AND $2
		ORDER BY trade_date ASC, symbol`

	rows, err := db.Pool().Query(c.Request.Context(), query, fromStr, toStr)
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
		"count":   len(records),
		"records": records,
	})
}
