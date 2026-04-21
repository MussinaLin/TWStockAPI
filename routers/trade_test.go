package routers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTradeRecords_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "count", "profit_count", "loss_count", "avg_performance", "win_rate", "records")

	records, ok := m["records"].([]any)
	require.True(t, ok)

	count := int(m["count"].(float64))
	assert.Equal(t, len(records), count)

	profit := int(m["profit_count"].(float64))
	loss := int(m["loss_count"].(float64))
	assert.LessOrEqual(t, profit+loss, count, "profit+loss must not exceed total records")

	if wr, present := m["win_rate"]; present && wr != nil {
		f, ok := wr.(float64)
		require.True(t, ok, "win_rate should be a number, got %T", wr)
		assert.GreaterOrEqual(t, f, 0.0)
		assert.LessOrEqual(t, f, 1.0)
	}
	if avg, present := m["avg_performance"]; present && avg != nil {
		_, ok := avg.(float64)
		require.True(t, ok, "avg_performance should be a number, got %T", avg)
	}

	for i, rec := range records {
		recm, ok := rec.(map[string]any)
		require.True(t, ok, "record %d not an object", i)
		requireKeys(t, recm, "symbol", "name", "type", "trade_date", "price", "performance")
		requireDateString(t, recm["trade_date"])
	}
}

func TestGetTradeRecords_DateRange(t *testing.T) {
	from := "2026-01-01"
	to := "2026-04-21"
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records?from="+from+"&to="+to)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	records, ok := m["records"].([]any)
	require.True(t, ok)

	var prev string
	for i, rec := range records {
		recm := rec.(map[string]any)
		d, ok := recm["trade_date"].(string)
		require.True(t, ok, "row %d: trade_date not string", i)
		assert.GreaterOrEqual(t, d, from, "row %d: trade_date %s before from %s", i, d, from)
		assert.LessOrEqual(t, d, to, "row %d: trade_date %s after to %s", i, d, to)
		if i > 0 {
			assert.LessOrEqual(t, d, prev, "row %d: trade_date should be DESC", i)
		}
		prev = d
	}

	// sanity: the parse worked end-to-end
	_, err := time.Parse(time.DateOnly, from)
	require.NoError(t, err)
}

func TestGetTradeRecords_EmptyRange(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/trade/trade-records?from=1900-01-01&to=1900-01-02")
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "count", "profit_count", "loss_count", "avg_performance", "win_rate", "records")

	records, ok := m["records"].([]any)
	require.True(t, ok)
	assert.Empty(t, records)
	assert.Equal(t, float64(0), m["count"])
	assert.Equal(t, float64(0), m["profit_count"])
	assert.Equal(t, float64(0), m["loss_count"])
	assert.Nil(t, m["avg_performance"])
	assert.Nil(t, m["win_rate"])
}
