package routers

import (
	"context"
	"main/db"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func anyMarketDate(t *testing.T) string {
	t.Helper()
	var d *string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT MAX(trade_date)::text FROM market_daily").Scan(&d)
	require.NoError(t, err)
	if d == nil {
		t.Skip("no rows in market_daily")
	}
	return *d
}

var marketKeys = []string{
	"trade_date",
	"taiex_open", "taiex_high", "taiex_low", "taiex_close",
	"total_volume",
	"margin_balance", "margin_balance_change",
	"foreign_net",
	"txf_foreign_oi_net", "txf_total_oi_net",
	"tw_vix_close",
	"day_trade_sell_amount_pct",
}

var marketExtraKeys = []string{
	"txf_foreign_oi_net", "txf_total_oi_net",
	"tw_vix_close",
	"day_trade_sell_amount_pct",
}

func TestListMarketDaily_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	if len(arr) == 0 {
		t.Skip("no rows in market_daily")
	}

	first, ok := arr[0].(map[string]any)
	require.True(t, ok)
	requireKeys(t, first, marketKeys...)
	requireNoKey(t, first, "created_time")
	requireNoKey(t, first, "updated_time")

	var prev string
	for i, item := range arr {
		m := item.(map[string]any)
		requireDateString(t, m["trade_date"])
		d := m["trade_date"].(string)
		if i > 0 {
			assert.LessOrEqual(t, d, prev, "trade_date should be desc at row %d", i)
		}
		prev = d
	}
}

func TestListMarketDaily_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market?limit=5")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 5)
}

func TestListMarketDaily_LimitClampedToMax(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market?limit=99999")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 1500)
}

func TestListMarketDaily_LimitInvalidFallsBackToDefault(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market?limit=abc")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 500)
}

func TestListMarketDates_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/dates")
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

func TestListMarketDates_LimitHonored(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/dates?limit=3")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)
	assert.LessOrEqual(t, len(arr), 3)
}

func TestGetMarketByDate_OK(t *testing.T) {
	date := anyMarketDate(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/"+date)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok, "expected single object, got %T", body)

	requireKeys(t, m, marketKeys...)
	requireNoKey(t, m, "created_time")
	requireNoKey(t, m, "updated_time")
	require.Equal(t, date, m["trade_date"])
}

func TestGetMarketByDate_NotFound(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/1900-01-01")
	require.Equal(t, 404, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "not found", m["error"])
}

func TestGetMarketByDate_ExtraFieldsMatchDB(t *testing.T) {
	date := anyMarketDate(t)

	var dbVals [4]*float64
	err := db.Pool().QueryRow(context.Background(),
		`SELECT txf_foreign_oi_net::float8, txf_total_oi_net::float8,
		        tw_vix_close::float8, day_trade_sell_amount_pct::float8
		 FROM market_daily WHERE trade_date = $1`, date).
		Scan(&dbVals[0], &dbVals[1], &dbVals[2], &dbVals[3])
	require.NoError(t, err)

	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/"+date)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok, "expected single object, got %T", body)
	requireKeys(t, m, marketExtraKeys...)

	for i, key := range marketExtraKeys {
		if dbVals[i] == nil {
			assert.Nil(t, m[key], "%s should be null", key)
			continue
		}
		got, ok := m[key].(float64)
		require.True(t, ok, "%s should be a number, got %T", key, m[key])
		assert.InDelta(t, *dbVals[i], got, 1e-9, "%s should equal DB value", key)
	}
}

func TestGetMarketByDate_NullDayTradePct(t *testing.T) {
	var d *string
	err := db.Pool().QueryRow(context.Background(),
		`SELECT MAX(trade_date)::text FROM market_daily
		 WHERE day_trade_sell_amount_pct IS NULL`).Scan(&d)
	require.NoError(t, err)
	if d == nil {
		t.Skip("no market_daily row with NULL day_trade_sell_amount_pct")
	}

	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/market/"+*d)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok, "expected single object, got %T", body)
	requireKeys(t, m, "day_trade_sell_amount_pct")
	assert.Nil(t, m["day_trade_sell_amount_pct"])
}
