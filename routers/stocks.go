package routers

import (
	"main/db"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RegisterStocks(rg *gin.RouterGroup) {
	g := rg.Group("/stocks")
	g.GET("", listStocks)
	g.GET("/:symbol", getStock)
}

func listStocks(c *gin.Context) {
	query := `SELECT s.symbol, s.name, s.enabled, sh.issued_shares
		FROM stocks s
		LEFT JOIN tw_stock_shares sh USING (symbol)`

	var args []any

	if v, ok := c.GetQuery("enabled"); ok {
		enabled := v == "true" || v == "1"
		query += " WHERE s.enabled = $1"
		args = append(args, enabled)
	}

	query += " ORDER BY s.symbol"

	rows, err := db.Pool().Query(c.Request.Context(), query, args...)
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

func getStock(c *gin.Context) {
	symbol := c.Param("symbol")

	query := `SELECT s.symbol, s.name, s.enabled, sh.issued_shares
		FROM stocks s
		LEFT JOIN tw_stock_shares sh USING (symbol)
		WHERE s.symbol = $1`

	rows, err := db.Pool().Query(c.Request.Context(), query, symbol)
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
