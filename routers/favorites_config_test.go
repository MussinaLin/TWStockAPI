package routers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFavoriteGroups(t *testing.T) {
	raw := []byte(`{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`)

	got, err := parseFavoriteGroups(raw, 0)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 5, StocksPerGroup: 5}, got)

	got, err = parseFavoriteGroups(raw, 1)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 10, StocksPerGroup: 10}, got)

	got, err = parseFavoriteGroups([]byte(`{"0": {"groups": 99, "stocks_per_group": 0}}`), 0)
	require.NoError(t, err)
	assert.Equal(t, groupRule{Groups: 99, StocksPerGroup: 0}, got)
}

func TestParseFavoriteGroupsErrors(t *testing.T) {
	cases := map[string]struct {
		raw   string
		level int
	}{
		"level missing":            {`{"0": {"groups": 5, "stocks_per_group": 5}, "1": {"groups": 10, "stocks_per_group": 10}}`, 2},
		"groups missing":           {`{"0": {"stocks_per_group": 5}}`, 0},
		"stocks_per_group missing": {`{"0": {"groups": 5}}`, 0},
		"level null":               {`{"0": null}`, 0},
		"groups zero":              {`{"0": {"groups": 0, "stocks_per_group": 5}}`, 0},
		"groups over 99":           {`{"0": {"groups": 100, "stocks_per_group": 5}}`, 0},
		"negative stocks":          {`{"0": {"groups": 5, "stocks_per_group": -1}}`, 0},
		"fractional":               {`{"0": {"groups": 5.5, "stocks_per_group": 5}}`, 0},
		"string value":             {`{"0": {"groups": "5", "stocks_per_group": 5}}`, 0},
		"old flat format":          {`{"0": 10}`, 0},
		"invalid json":             {`{`, 0},
		"null":                     {`null`, 0},
		"array":                    {`[5]`, 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFavoriteGroups([]byte(tc.raw), tc.level)
			assert.Error(t, err)
		})
	}
}

func TestGroupName(t *testing.T) {
	cases := map[int]string{
		1:  "群組一",
		9:  "群組九",
		10: "群組十",
		11: "群組十一",
		19: "群組十九",
		20: "群組二十",
		21: "群組二十一",
		99: "群組九十九",
	}
	for n, want := range cases {
		assert.Equal(t, want, groupName(n), "groupName(%d)", n)
	}
}
