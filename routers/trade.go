package routers

import (
	"main/db"
	"math"
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

	var profitCount, lossCount int
	var perfSum float64
	var perfValidCount int
	for _, r := range records {
		perf, ok := r["performance"].(float64)
		if !ok {
			continue
		}
		perfValidCount++
		perfSum += perf
		if perf > 0 {
			profitCount++
		} else if perf < 0 {
			lossCount++
		}
	}

	var avgPerf any
	if perfValidCount > 0 {
		avgPerf = math.Round(perfSum/float64(perfValidCount)*10000) / 10000
	}

	var winRate any
	if total := profitCount + lossCount; total > 0 {
		winRate = math.Round(float64(profitCount)/float64(total)*100*10000) / 10000
	}

	c.JSON(http.StatusOK, gin.H{
		"count":           len(records),
		"profit_count":    profitCount,
		"loss_count":      lossCount,
		"avg_performance": avgPerf,
		"win_rate":        winRate,
		"records":         records,
	})
}
