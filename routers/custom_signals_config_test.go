package routers

import (
	"context"
	"testing"

	"main/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const customSignalLimitsSample = `{"0": {"stocks": 5, "conditions_per_side": 5}, "1": {"stocks": 20, "conditions_per_side": 10}}`

func TestParseCustomSignalLimits_OK(t *testing.T) {
	got, err := parseCustomSignalLimits([]byte(customSignalLimitsSample), 0)
	require.NoError(t, err)
	assert.Equal(t, signalRule{Stocks: 5, ConditionsPerSide: 5}, got)

	got, err = parseCustomSignalLimits([]byte(customSignalLimitsSample), 1)
	require.NoError(t, err)
	assert.Equal(t, signalRule{Stocks: 20, ConditionsPerSide: 10}, got)

	got, err = parseCustomSignalLimits([]byte(`{"0": {"stocks": 0, "conditions_per_side": 1}}`), 0)
	require.NoError(t, err)
	assert.Equal(t, signalRule{Stocks: 0, ConditionsPerSide: 1}, got)
}

func TestParseCustomSignalLimits_Errors(t *testing.T) {
	cases := map[string]struct {
		raw   string
		level int
	}{
		"invalid json":       {`{`, 0},
		"level missing":      {customSignalLimitsSample, 2},
		"level null":         {`{"0": null}`, 0},
		"stocks missing":     {`{"0": {"conditions_per_side": 5}}`, 0},
		"conditions missing": {`{"0": {"stocks": 5}}`, 0},
		"stocks fractional":  {`{"0": {"stocks": 1.5, "conditions_per_side": 5}}`, 0},
		"stocks negative":    {`{"0": {"stocks": -1, "conditions_per_side": 5}}`, 0},
		"conditions zero":    {`{"0": {"stocks": 5, "conditions_per_side": 0}}`, 0},
		"string value":       {`{"0": {"stocks": "5", "conditions_per_side": 5}}`, 0},
		"null":               {`null`, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseCustomSignalLimits([]byte(tc.raw), tc.level)
			assert.Error(t, err)
		})
	}
}

func TestLoadCustomSignalLimits_FromDB(t *testing.T) {
	got, err := loadCustomSignalLimits(context.Background(), db.Pool(), 0)
	require.NoError(t, err, "apply db/user_custom_signals.sql to the DB first")
	assert.Equal(t, signalRule{Stocks: 5, ConditionsPerSide: 5}, got)
}
