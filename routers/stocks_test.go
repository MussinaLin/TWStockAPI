package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListStocks_OK(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok, "expected array, got %T", body)

	if len(arr) > 0 {
		first, ok := arr[0].(map[string]any)
		require.True(t, ok)
		requireKeys(t, first, "symbol", "name", "enabled", "issued_shares")
	}
}

func TestListStocks_EnabledFilter(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks?enabled=true")
	require.Equal(t, 200, status)

	arr, ok := body.([]any)
	require.True(t, ok)

	for i, item := range arr {
		m, ok := item.(map[string]any)
		require.True(t, ok, "row %d not an object", i)
		assert.Equal(t, true, m["enabled"], "row %d: expected enabled=true", i)
	}
}

func TestGetStock_OK(t *testing.T) {
	symbol := anyStockSymbol(t)
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks/"+symbol)
	require.Equal(t, 200, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "symbol", "name", "enabled", "issued_shares")
	assert.Equal(t, symbol, m["symbol"])
}

func TestGetStock_NotFound(t *testing.T) {
	r := newTestRouter()
	status, body := doJSON(t, r, "GET", "/api/stocks/__INVALID_XX__")
	require.Equal(t, 404, status)

	m, ok := body.(map[string]any)
	require.True(t, ok)
	requireKeys(t, m, "error")
}
