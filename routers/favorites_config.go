package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// favoriteGroupsKey is the user_config key holding the per-member_level group
// rules, e.g. {"0": {"groups": 5, "stocks_per_group": 5}}.
const favoriteGroupsKey = "FAVORITE_GROUPS"

// maxFavoriteGroups is the most groups groupName can name (群組九十九).
const maxFavoriteGroups = 99

// groupRule is one member_level's favorites layout.
type groupRule struct {
	Groups         int
	StocksPerGroup int
}

// loadFavoriteGroups 讀取 user_config 的 FAVORITE_GROUPS，回傳 level 對應的規則。
func loadFavoriteGroups(ctx context.Context, q querier, level int) (groupRule, error) {
	var raw string
	err := q.QueryRow(ctx,
		"SELECT value::text FROM user_config WHERE key = $1", favoriteGroupsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return groupRule{}, fmt.Errorf("favorites: %s is not set in user_config", favoriteGroupsKey)
	}
	if err != nil {
		return groupRule{}, fmt.Errorf("favorites: load %s: %w", favoriteGroupsKey, err)
	}
	return parseFavoriteGroups([]byte(raw), level)
}

// parseFavoriteGroups 解析 FAVORITE_GROUPS 的 JSONB value，回傳該 member_level 的規則。
// JSON 格式錯誤、找不到該 level、缺少欄位、groups 不在 1–99、stocks_per_group < 0，
// 或值不是整數，都回傳 error。
func parseFavoriteGroups(raw []byte, level int) (groupRule, error) {
	var rules map[string]*struct {
		Groups         *int `json:"groups"`
		StocksPerGroup *int `json:"stocks_per_group"`
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return groupRule{}, fmt.Errorf("favorites: parse %s: %w", favoriteGroupsKey, err)
	}
	r := rules[strconv.Itoa(level)]
	if r == nil {
		return groupRule{}, fmt.Errorf("favorites: %s has no rule for member_level %d", favoriteGroupsKey, level)
	}
	if r.Groups == nil || r.StocksPerGroup == nil {
		return groupRule{}, fmt.Errorf("favorites: %s rule for member_level %d needs groups and stocks_per_group", favoriteGroupsKey, level)
	}
	if *r.Groups < 1 || *r.Groups > maxFavoriteGroups {
		return groupRule{}, fmt.Errorf("favorites: %s groups for member_level %d must be 1-%d, got %d", favoriteGroupsKey, level, maxFavoriteGroups, *r.Groups)
	}
	if *r.StocksPerGroup < 0 {
		return groupRule{}, fmt.Errorf("favorites: %s stocks_per_group for member_level %d is negative: %d", favoriteGroupsKey, level, *r.StocksPerGroup)
	}
	return groupRule{Groups: *r.Groups, StocksPerGroup: *r.StocksPerGroup}, nil
}

var chineseDigits = [...]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// groupName 回傳群組名稱，例如 1 → 群組一、10 → 群組十、21 → 群組二十一。
// n 必須在 1 到 maxFavoriteGroups 之間（parseFavoriteGroups 已保證）。
func groupName(n int) string {
	tens, ones := n/10, n%10
	s := ""
	if tens > 1 {
		s = chineseDigits[tens]
	}
	if tens > 0 {
		s += "十"
	}
	return "群組" + s + chineseDigits[ones]
}

// maxGroupNameLen 是自訂群組名稱的最大字元數（以 Unicode code point 計），
// 與 user_favorite_groups.name 的 VARCHAR(10) 一致。
const maxGroupNameLen = 10

// normalizeGroupName 去掉名稱前後的空白（包括全形空白）後檢查：必須是合法的 UTF-8、
// 有 1 到 maxGroupNameLen 個字元、不含控制字元。合法時回傳去掉空白後的名稱與 true。
func normalizeGroupName(raw string) (string, bool) {
	name := strings.TrimSpace(raw)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxGroupNameLen {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}
