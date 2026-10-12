package routers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// RegisterFavorites mounts /favorites behind requireLogin; every handler needs
// the authenticated user.
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.DELETE("/groups/:group_no/:symbol", deleteFavorite)
}

type favoriteItem struct {
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

type favoriteGroup struct {
	GroupNo int            `json:"group_no"`
	Name    string         `json:"name"`
	Count   int            `json:"count"`
	Items   []favoriteItem `json:"items"`
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// errUserGone means the authenticated user's users row no longer exists.
var errUserGone = errors.New("favorites: user no longer exists")

// selectMemberLevel 讀取 user 的 member_level；selectMemberLevelForUpdate 同時鎖住
// user 的 row，讓同一個 user 的 POST 排隊處理。
const (
	selectMemberLevel          = "SELECT member_level FROM users WHERE id = $1::uuid"
	selectMemberLevelForUpdate = selectMemberLevel + " FOR UPDATE"
)

// loadUserRule 讀取 user 的 member_level 與對應的群組規則。levelSQL 為
// selectMemberLevel 或 selectMemberLevelForUpdate；user 不存在時回傳 errUserGone。
func loadUserRule(ctx context.Context, q querier, levelSQL, userID string) (groupRule, error) {
	var level int
	err := q.QueryRow(ctx, levelSQL, userID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return groupRule{}, errUserGone
	}
	if err != nil {
		return groupRule{}, fmt.Errorf("favorites: load member_level: %w", err)
	}
	return loadFavoriteGroups(ctx, q, level)
}

// respondRuleError 依 loadUserRule 的錯誤回 401（user 已不存在）或 500。
func respondRuleError(c *gin.Context, err error) {
	if errors.Is(err, errUserGone) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	favoritesServerError(c, err)
}

// parseGroupNo 解析 :group_no；只接受 >= 1 的整數。
func parseGroupNo(c *gin.Context) (int, bool) {
	n, err := strconv.Atoi(c.Param("group_no"))
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// invalidSymbol 回報 PostgreSQL 無法當成字串參數的 symbol（含 NUL 或不是合法的 UTF-8）；
// 這種 symbol 不可能存在於 stocks。
func invalidSymbol(s string) bool {
	return strings.ContainsRune(s, 0) || !utf8.ValidString(s)
}

// listFavorites 回傳目前等級的所有群組（包括空的群組）與群組底下的個股。
// group_no 超過目前群組數的資料（降級後）保留在 DB，但不回傳。
func listFavorites(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	ctx := c.Request.Context()

	rule, err := loadUserRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}

	groups := make([]favoriteGroup, rule.Groups)
	for i := range groups {
		groups[i] = favoriteGroup{GroupNo: i + 1, Name: groupName(i + 1), Items: []favoriteItem{}}
	}

	rows, err := db.Pool().Query(ctx,
		`SELECT f.group_no, f.symbol, s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid AND f.group_no <= $2
		ORDER BY f.group_no, f.created_time DESC, f.symbol`, user.ID, rule.Groups)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	defer rows.Close()
	for rows.Next() {
		var groupNo int
		var item favoriteItem
		if err := rows.Scan(&groupNo, &item.Symbol, &item.Name, &item.CreatedTime); err != nil {
			favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
			return
		}
		g := &groups[groupNo-1]
		g.Items = append(g.Items, item)
		g.Count++
	}
	if err := rows.Err(); err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"max_groups":           rule.Groups,
		"max_stocks_per_group": rule.StocksPerGroup,
		"groups":               groups,
	})
}

// deleteFavorite 從指定群組移除一檔股票；不論原本是否存在都回 204。
func deleteFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	groupNo, ok := parseGroupNo(c)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	symbol := c.Param("symbol")
	if invalidSymbol(symbol) {
		c.Status(http.StatusNoContent)
		return
	}
	ctx := c.Request.Context()

	rule, err := loadUserRule(ctx, db.Pool(), selectMemberLevel, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}
	if groupNo > rule.Groups {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	_, err = db.Pool().Exec(ctx,
		"DELETE FROM user_favorite_stocks WHERE user_id = $1::uuid AND group_no = $2 AND symbol = $3",
		user.ID, groupNo, symbol)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: delete: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}

// favoritesServerError records err for slog-gin and responds 500.
func favoritesServerError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
