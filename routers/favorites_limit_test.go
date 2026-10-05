package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFavoriteLimit(t *testing.T) {
	raw := []byte(`{"0": 10, "1": 15, "2": 20}`)

	got, err := parseFavoriteLimit(raw, 1)
	require.NoError(t, err)
	assert.Equal(t, 15, got)

	got, err = parseFavoriteLimit([]byte(`{"0": 0}`), 0)
	require.NoError(t, err)
	assert.Equal(t, 0, got)
}

func TestParseFavoriteLimitErrors(t *testing.T) {
	cases := map[string]struct {
		raw   string
		level int
	}{
		"level missing": {`{"0": 10, "1": 15, "2": 20}`, 3},
		"invalid json":  {`{`, 0},
		"fractional":    {`{"0": 10.5}`, 0},
		"string value":  {`{"0": "10"}`, 0},
		"negative":      {`{"0": -1}`, 0},
		"null":          {`null`, 0},
		"array":         {`[10]`, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFavoriteLimit([]byte(tc.raw), tc.level)
			assert.Error(t, err)
		})
	}
}
