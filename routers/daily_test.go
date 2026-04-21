package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListDailyDates_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/dates")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	var prev string
	for i, d := range arr {
		requireDateString(t, d)
		s := d.(string)
		if i > 0 {
			assert.LessOrEqual(t, s, prev, "dates should be descending at index %d", i)
		}
		prev = s
	}
}

func TestListDailyDates_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/dates?limit=3")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 3)
}

func TestGetDailyByDate_OK(t *testing.T) {
	date := anyDailyDate(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/"+date)
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	require.NotEmpty(t, arr, "anyDailyDate returned %s but /api/daily/%s is empty", date, date)

	first, ok := arr[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "name",
		"open", "close", "high", "low", "volume",
		"turnover_rate",
		"foreign_net", "trust_net", "dealer_net", "institutional_investors_net",
		"margin_balance", "short_balance", "short_margin_ratio",
		"foreign_holding_pct", "insti_holding_pct",
		"vol_ma5", "vol_ma10", "vol_ma20", "turnover_ma20",
		"foreign_net_5d_avg", "foreign_net_10d_avg",
		"foreign_net_15d_avg", "foreign_net_30d_avg",
		"rsi_9", "rsi_14",
		"macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_middle", "bb_lower",
		"bb_percent_b", "bb_bandwidth",
	)
}

func TestGetDailyByDate_NoData(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/1900-01-01")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.Empty(t, arr)
}

func TestGetStockHistory_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/stock/"+symbol)
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	if len(arr) == 0 {
		t.Skipf("no daily rows for symbol %s", symbol)
	}

	first, ok := arr[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"trade_date",
		"open", "close", "high", "low", "volume",
		"turnover_rate",
		"foreign_net", "trust_net", "dealer_net", "institutional_investors_net",
		"margin_balance", "short_balance", "short_margin_ratio",
		"vol_ma5", "vol_ma10", "vol_ma20", "turnover_ma20",
		"foreign_net_5d_avg", "foreign_net_10d_avg",
		"foreign_net_15d_avg", "foreign_net_30d_avg",
		"rsi_9", "rsi_14",
		"macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_middle", "bb_lower",
		"bb_percent_b", "bb_bandwidth",
	)

	var prev string
	for i, item := range arr {
		m := item.(map[string]any)
		d, ok := m["trade_date"].(string)
		require.True(t, ok, "row %d: trade_date is not a string", i)
		if i > 0 {
			assert.LessOrEqual(t, d, prev, "trade_date should be desc at row %d", i)
		}
		prev = d
	}
}

func TestGetStockHistory_LimitHonored(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/daily/stock/"+symbol+"?limit=5")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 5)
}
