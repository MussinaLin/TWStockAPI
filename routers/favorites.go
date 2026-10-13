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
	g.POST("/groups/:group_no", addFavorite)
	g.PUT("/groups/:group_no", renameFavoriteGroup)
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

// favoriteGroupItem is the POST response: one stock in one group.
type favoriteGroupItem struct {
	GroupNo     int       `json:"group_no"`
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

type renameFavoriteGroupRequest struct {
	Name string `json:"name"`
}

// favoriteGroupName is the PUT response: a group and its custom name.
type favoriteGroupName struct {
	GroupNo int    `json:"group_no"`
	Name    string `json:"name"`
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

// listFavorites 回傳目前等級的所有群組（包括空的群組）與群組底下的個股；有自訂名稱的
// 群組回傳自訂名稱。group_no 超過目前群組數的資料與名稱（降級後）保留在 DB，但不回傳。
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
	if err := applyCustomGroupNames(ctx, user.ID, groups); err != nil {
		favoritesServerError(c, err)
		return
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

// applyCustomGroupNames 用 user_favorite_groups 的自訂名稱取代 groups 的預設名稱；
// 只查 groups 範圍內（group_no ≤ len(groups)）的群組。
func applyCustomGroupNames(ctx context.Context, userID string, groups []favoriteGroup) error {
	rows, err := db.Pool().Query(ctx,
		`SELECT group_no, name FROM user_favorite_groups
		WHERE user_id = $1::uuid AND group_no <= $2`, userID, len(groups))
	if err != nil {
		return fmt.Errorf("favorites: list group names: %w", err)
	}
	var groupNo int
	var name string
	_, err = pgx.ForEachRow(rows, []any{&groupNo, &name}, func() error {
		groups[groupNo-1].Name = name
		return nil
	})
	if err != nil {
		return fmt.Errorf("favorites: list group names: %w", err)
	}
	return nil
}

type addFavoriteRequest struct {
	Symbol string `json:"symbol"`
}

// addFavorite 把一檔股票加入指定群組。整個流程在同一個 transaction 裡，先以 FOR UPDATE
// 鎖住 user 的 row，讓同一個 user 的請求排隊處理，避免同時送出時超過群組上限。
func addFavorite(c *gin.Context) {
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
	var req addFavoriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	symbol := strings.TrimSpace(req.Symbol)
	if symbol == "" || invalidSymbol(symbol) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	ctx := c.Request.Context()
	tx, err := db.Pool().Begin(ctx)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: begin: %w", err))
		return
	}
	defer tx.Rollback(ctx) // 已 Commit 時為 no-op

	rule, err := loadUserRule(ctx, tx, selectMemberLevelForUpdate, user.ID)
	if err != nil {
		respondRuleError(c, err)
		return
	}
	if groupNo > rule.Groups {
		c.JSON(http.StatusNotFound, gin.H{"error": "group not found"})
		return
	}

	item := favoriteGroupItem{GroupNo: groupNo, Symbol: symbol}
	err = tx.QueryRow(ctx,
		`SELECT s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid AND f.group_no = $2 AND f.symbol = $3`,
		user.ID, groupNo, symbol).Scan(&item.Name, &item.CreatedTime)
	if err == nil {
		c.JSON(http.StatusOK, item)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		favoritesServerError(c, fmt.Errorf("favorites: find existing: %w", err))
		return
	}

	err = tx.QueryRow(ctx,
		"SELECT name FROM stocks WHERE symbol = $1 AND enabled", symbol).Scan(&item.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "stock not found"})
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: find stock: %w", err))
		return
	}

	var count int
	err = tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_favorite_stocks WHERE user_id = $1::uuid AND group_no = $2",
		user.ID, groupNo).Scan(&count)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: count: %w", err))
		return
	}
	if count >= rule.StocksPerGroup {
		c.JSON(http.StatusConflict, gin.H{"error": "favorite group full", "limit": rule.StocksPerGroup})
		return
	}

	err = tx.QueryRow(ctx,
		`INSERT INTO user_favorite_stocks (user_id, group_no, symbol)
		VALUES ($1::uuid, $2, $3) RETURNING created_time`,
		user.ID, groupNo, symbol).Scan(&item.CreatedTime)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: insert: %w", err))
		return
	}
	if err := tx.Commit(ctx); err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: commit: %w", err))
		return
	}
	c.JSON(http.StatusCreated, item)
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

// renameFavoriteGroup 設定群組的自訂名稱；再改一次會取代原本的名稱。
// 改名沒有數量上限要維護，所以不開 transaction、不鎖 user 的 row，同時改名以後寫入的為準。
func renameFavoriteGroup(c *gin.Context) {
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
	var req renameFavoriteGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	name, ok := normalizeGroupName(req.Name)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
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
		`INSERT INTO user_favorite_groups (user_id, group_no, name)
		VALUES ($1::uuid, $2, $3)
		ON CONFLICT (user_id, group_no) DO UPDATE SET name = EXCLUDED.name, updated_time = NOW()`,
		user.ID, groupNo, name)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: rename group: %w", err))
		return
	}
	c.JSON(http.StatusOK, favoriteGroupName{GroupNo: groupNo, Name: name})
}

// favoritesServerError records err for slog-gin and responds 500.
func favoritesServerError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
