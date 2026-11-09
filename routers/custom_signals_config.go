package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// customSignalLimitsKey is the user_config key holding the per-member_level
// custom signal limits, e.g. {"0": {"stocks": 5, "conditions_per_side": 5}}.
const customSignalLimitsKey = "CUSTOM_SIGNAL_LIMITS"

// signalRule is one member_level's custom signal limits.
type signalRule struct {
	Stocks            int
	ConditionsPerSide int
}

// loadCustomSignalLimits 讀取 user_config 的 CUSTOM_SIGNAL_LIMITS，回傳 level 對應的規則。
func loadCustomSignalLimits(ctx context.Context, q querier, level int) (signalRule, error) {
	var raw string
	err := q.QueryRow(ctx,
		"SELECT value::text FROM user_config WHERE key = $1", customSignalLimitsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalRule{}, fmt.Errorf("custom signals: %s is not set in user_config", customSignalLimitsKey)
	}
	if err != nil {
		return signalRule{}, fmt.Errorf("custom signals: load %s: %w", customSignalLimitsKey, err)
	}
	return parseCustomSignalLimits([]byte(raw), level)
}

// parseCustomSignalLimits 解析 CUSTOM_SIGNAL_LIMITS 的 JSONB value，回傳該 member_level 的規則。
// JSON 格式錯誤、找不到該 level、缺少欄位、值不是整數、stocks < 0、
// conditions_per_side < 1 都回傳 error。
func parseCustomSignalLimits(raw []byte, level int) (signalRule, error) {
	var rules map[string]*struct {
		Stocks            *int `json:"stocks"`
		ConditionsPerSide *int `json:"conditions_per_side"`
	}
	if err := json.Unmarshal(raw, &rules); err != nil {
		return signalRule{}, fmt.Errorf("custom signals: parse %s: %w", customSignalLimitsKey, err)
	}
	r := rules[strconv.Itoa(level)]
	if r == nil {
		return signalRule{}, fmt.Errorf("custom signals: %s has no rule for member_level %d", customSignalLimitsKey, level)
	}
	if r.Stocks == nil || r.ConditionsPerSide == nil {
		return signalRule{}, fmt.Errorf("custom signals: %s rule for member_level %d needs stocks and conditions_per_side", customSignalLimitsKey, level)
	}
	if *r.Stocks < 0 {
		return signalRule{}, fmt.Errorf("custom signals: %s stocks for member_level %d is negative: %d", customSignalLimitsKey, level, *r.Stocks)
	}
	if *r.ConditionsPerSide < 1 {
		return signalRule{}, fmt.Errorf("custom signals: %s conditions_per_side for member_level %d must be >= 1, got %d", customSignalLimitsKey, level, *r.ConditionsPerSide)
	}
	return signalRule{Stocks: *r.Stocks, ConditionsPerSide: *r.ConditionsPerSide}, nil
}

// loadSignalRule 讀取 user 的 member_level 與對應的自訂訊號規則。levelSQL 為
// selectMemberLevel 或 selectMemberLevelForUpdate；user 不存在時回傳 errUserGone。
func loadSignalRule(ctx context.Context, q querier, levelSQL, userID string) (signalRule, error) {
	var level int
	err := q.QueryRow(ctx, levelSQL, userID).Scan(&level)
	if errors.Is(err, pgx.ErrNoRows) {
		return signalRule{}, errUserGone
	}
	if err != nil {
		return signalRule{}, fmt.Errorf("custom signals: load member_level: %w", err)
	}
	return loadCustomSignalLimits(ctx, q, level)
}
