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

func TestNormalizeGroupName(t *testing.T) {
	valid := map[string]string{
		"科技股":            "科技股",
		"  科技股  ":        "科技股",
		"　科技股　":          "科技股",
		"\t科技股\n":        "科技股",
		"科技 股":           "科技 股",
		"一二三四五六七八九十":     "一二三四五六七八九十",
		"abcdefghij":     "abcdefghij",
		"  一二三四五六七八九十  ": "一二三四五六七八九十",
		"群組三":            "群組三",
		"📈":              "📈",
	}
	for raw, want := range valid {
		got, ok := normalizeGroupName(raw)
		assert.True(t, ok, "normalizeGroupName(%q) should be valid", raw)
		assert.Equal(t, want, got, "normalizeGroupName(%q)", raw)
	}

	invalid := []string{
		"一二三四五六七八九十一",
		"abcdefghijk",
		"",
		"   ",
		"　",
		"科技\n股",
		"科技\t股",
		"科技\u0000股",
		"\xff",
	}
	for _, raw := range invalid {
		got, ok := normalizeGroupName(raw)
		assert.False(t, ok, "normalizeGroupName(%q) should be invalid", raw)
		assert.Equal(t, "", got, "normalizeGroupName(%q)", raw)
	}
}
