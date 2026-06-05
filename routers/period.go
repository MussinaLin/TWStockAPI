package routers

import (
	"main/db"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func RegisterPeriod(rg *gin.RouterGroup) {
	g := rg.Group("/period")
	g.GET("/dahu/:symbol", getDahuBySymbol)
}

// getDahuBySymbol 查詢特定 symbol 的集保大戶持股 (stock_major_holder)。
// 一律以 trade_date 由舊到新 (ASC) 回傳；無 ?limit 時回傳全部，
// 有合法 ?limit=N (N>=1) 時取「最新 N 筆」再以 ASC 排序呈現。
func getDahuBySymbol(c *gin.Context) {
	symbol := c.Param("symbol")

	args := []any{symbol}
	var query string
	if limit, ok := optionalLimit(c); ok {
		// 內層 DESC LIMIT 取最新 N 筆，外層再 ASC 由舊到新呈現。
		query = `SELECT symbol, trade_date, name, holding_ratio FROM (
				SELECT symbol, trade_date, name, holding_ratio
				FROM stock_major_holder
				WHERE symbol = $1
				ORDER BY trade_date DESC LIMIT $2
			) t ORDER BY trade_date ASC`
		args = append(args, limit)
	} else {
		query = `SELECT symbol, trade_date, name, holding_ratio
			FROM stock_major_holder
			WHERE symbol = $1
			ORDER BY trade_date ASC`
	}

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

// optionalLimit 回傳 ?limit 查詢參數，僅在有提供且為合法正整數時 ok 為 true。
func optionalLimit(c *gin.Context) (int, bool) {
	limitStr := c.Query("limit")
	if limitStr == "" {
		return 0, false
	}
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		return 0, false
	}
	return limit, true
}
