package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"main/auth"
	"main/db"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// favoriteLimitKey is the user_config key holding the per-member_level limits,
// e.g. {"0": 10, "1": 15, "2": 20}.
const favoriteLimitKey = "FAVORITE_STOCKS_LIMIT"

// RegisterFavorites mounts /favorites behind requireLogin; every handler needs
// the authenticated user.
func RegisterFavorites(rg *gin.RouterGroup, requireLogin gin.HandlerFunc) {
	g := rg.Group("/favorites", requireLogin)
	g.GET("", listFavorites)
	g.DELETE("/:symbol", deleteFavorite)
}

type favoriteItem struct {
	Symbol      string    `json:"symbol"`
	Name        string    `json:"name"`
	CreatedTime time.Time `json:"created_time"`
}

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// listFavorites 回傳 user 的我的最愛（最新加入在前），以及該 member_level 的上限。
func listFavorites(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}
	ctx := c.Request.Context()

	var level int
	err := db.Pool().QueryRow(ctx,
		"SELECT member_level FROM users WHERE id = $1::uuid", user.ID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: load member_level: %w", err))
		return
	}

	limit, err := loadFavoriteLimit(ctx, db.Pool(), level)
	if err != nil {
		favoritesServerError(c, err)
		return
	}

	rows, err := db.Pool().Query(ctx,
		`SELECT f.symbol, s.name, f.created_time
		FROM user_favorite_stocks f
		JOIN stocks s ON s.symbol = f.symbol
		WHERE f.user_id = $1::uuid
		ORDER BY f.created_time DESC, f.symbol`, user.ID)
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[favoriteItem])
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: list: %w", err))
		return
	}
	if items == nil {
		items = []favoriteItem{}
	}

	c.JSON(http.StatusOK, gin.H{"limit": limit, "count": len(items), "items": items})
}

// deleteFavorite 移除一檔我的最愛；不論原本是否存在都回 204。
func deleteFavorite(c *gin.Context) {
	user, ok := auth.CurrentUser(c)
	if !ok {
		favoritesServerError(c, errors.New("favorites: no authenticated user"))
		return
	}

	_, err := db.Pool().Exec(c.Request.Context(),
		"DELETE FROM user_favorite_stocks WHERE user_id = $1::uuid AND symbol = $2",
		user.ID, c.Param("symbol"))
	if err != nil {
		favoritesServerError(c, fmt.Errorf("favorites: delete: %w", err))
		return
	}
	c.Status(http.StatusNoContent)
}

// loadFavoriteLimit 讀取 user_config 的 FAVORITE_STOCKS_LIMIT，回傳 level 對應的上限。
func loadFavoriteLimit(ctx context.Context, q querier, level int) (int, error) {
	var raw string
	err := q.QueryRow(ctx,
		"SELECT value::text FROM user_config WHERE key = $1", favoriteLimitKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("favorites: %s is not set in user_config", favoriteLimitKey)
	}
	if err != nil {
		return 0, fmt.Errorf("favorites: load %s: %w", favoriteLimitKey, err)
	}
	return parseFavoriteLimit([]byte(raw), level)
}

// parseFavoriteLimit 解析 FAVORITE_STOCKS_LIMIT 的 JSONB value，回傳該 member_level 的上限。
// JSON 格式錯誤、找不到該 level、值不是 >= 0 的整數，都回傳 error。
func parseFavoriteLimit(raw []byte, level int) (int, error) {
	var limits map[string]int
	if err := json.Unmarshal(raw, &limits); err != nil {
		return 0, fmt.Errorf("favorites: parse %s: %w", favoriteLimitKey, err)
	}
	limit, ok := limits[strconv.Itoa(level)]
	if !ok {
		return 0, fmt.Errorf("favorites: %s has no limit for member_level %d", favoriteLimitKey, level)
	}
	if limit < 0 {
		return 0, fmt.Errorf("favorites: %s limit for member_level %d is negative: %d", favoriteLimitKey, level, limit)
	}
	return limit, nil
}

// favoritesServerError records err for slog-gin and responds 500.
func favoritesServerError(c *gin.Context, err error) {
	_ = c.Error(err)
	c.JSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
