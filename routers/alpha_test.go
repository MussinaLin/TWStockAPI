package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Pick endpoints ──

func TestGetLatestPick_OK(t *testing.T) {
	_ = anyAlphaPickDate(t, "alpha") // skip if alpha_pick is empty

	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/latest")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "picks")
	requireDateString(t, m["trade_date"])

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(picks), int(count))
	require.NotEmpty(t, picks, "expected non-empty picks since trade_date exists")

	first, ok := picks[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume",
		"vol_ma5", "vol_ma10", "vol_ma20",
		"rsi_14", "macd", "macd_signal", "macd_hist",
		"bb_upper", "bb_bandwidth", "bb_percent_b",
		"insti_net_5d_sum", "insti_net_5d_avg",
		"insti_net_10d_sum", "insti_net_10d_avg",
		"insti_net_15d_sum", "insti_net_15d_avg",
		"insti_net_30d_sum", "insti_net_30d_avg",
		"bb_bw_5d_avg", "bb_bw_10d_avg", "bb_bw_15d_avg", "bb_bw_30d_avg",
		"cond_insti", "cond_insti_bullish", "cond_rsi", "cond_macd",
		"cond_vol_ma10", "cond_vol_ma20",
		"cond_bb_narrow", "cond_bb_near_upper", "cond_turnover_surge",
	)
	requireNoKey(t, first, "reasons")
	requireDateString(t, first["trade_date"])
}

func TestListPickDates_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/dates")
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

func TestListPickDates_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/dates?limit=2")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 2)
}

func TestGetPickSummary_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/summary")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	if len(arr) > 0 {
		first, ok := arr[0].(map[string]any)
		require.True(t, ok)
		requireKeys(t, first, "symbol", "name", "pick_count", "first_date", "last_date")
	}
}

func TestGetPickBySymbol_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/stock/"+symbol)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "count", "records")
	assert.Equal(t, symbol, m["symbol"])

	records, ok := m["records"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(records), int(count))

	for i, rec := range records {
		recm, ok := rec.(map[string]any)
		require.True(t, ok, "record %d not an object", i)
		requireKeys(t, recm, "trade_date", "symbol", "name")
		requireNoKey(t, recm, "reasons")
		requireDateString(t, recm["trade_date"])
	}
}

func TestGetPickByDate_OK(t *testing.T) {
	date := anyAlphaPickDate(t, "alpha")
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/"+date)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "count", "picks")
	assert.Equal(t, date, m["trade_date"])

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	count, ok := m["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, len(picks), int(count))
	require.NotEmpty(t, picks)

	first, ok := picks[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first,
		"symbol", "trade_date", "name", "close", "volume",
		"rsi_14", "macd_hist", "bb_percent_b",
		"insti_net_5d_sum", "insti_net_5d_avg",
		"insti_net_10d_sum", "insti_net_10d_avg",
		"insti_net_15d_sum", "insti_net_15d_avg",
		"insti_net_30d_sum", "insti_net_30d_avg",
		"cond_insti", "cond_insti_bullish", "cond_rsi", "cond_macd",
		"cond_vol_ma10", "cond_vol_ma20",
		"cond_bb_narrow", "cond_bb_near_upper", "cond_turnover_surge",
	)
	requireNoKey(t, first, "reasons")
}

// ── Negative paths (unconditional) ──

func TestGetPickByDate_NoData(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/1900-01-01")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "trade_date", "picks")

	picks, ok := m["picks"].([]any)
	require.True(t, ok)
	assert.Empty(t, picks)
}

func TestGetPickBySymbol_Unknown(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/alpha/pick/stock/__INVALID_XX__")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "count", "records")
	assert.Equal(t, "__INVALID_XX__", m["symbol"])

	records, ok := m["records"].([]any)
	require.True(t, ok)
	assert.Empty(t, records)
	assert.Equal(t, float64(0), m["count"])
}
