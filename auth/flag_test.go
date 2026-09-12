package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLoader records calls and returns whatever its fields hold.
type fakeLoader struct {
	value string
	found bool
	err   error
	calls int
}

func (l *fakeLoader) load(context.Context) (string, bool, error) {
	l.calls++
	return l.value, l.found, l.err
}

// newTestFlagCache returns a cache whose clock reads *now.
func newTestFlagCache(l *fakeLoader, now *time.Time) *FlagCache {
	f := NewFlagCache(l.load, time.Minute)
	f.now = func() time.Time { return *now }
	return f
}

var t0 = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)

func TestFlagCacheUsesCachedValueWithinTTL(t *testing.T) {
	l := &fakeLoader{value: "true", found: true}
	now := t0
	f := newTestFlagCache(l, &now)

	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on)

	l.value = "false"
	now = t0.Add(59 * time.Second)
	on, err = f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on, "value should still come from the cache")
	assert.Equal(t, 1, l.calls)
}

func TestFlagCacheReloadsAfterTTL(t *testing.T) {
	l := &fakeLoader{value: "true", found: true}
	now := t0
	f := newTestFlagCache(l, &now)

	_, err := f.Enabled(context.Background())
	require.NoError(t, err)

	l.value = "false"
	now = t0.Add(time.Minute)
	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.False(t, on)
	assert.Equal(t, 2, l.calls)
}

func TestFlagCacheDoesNotCacheErrors(t *testing.T) {
	l := &fakeLoader{err: errors.New("db down")}
	now := t0
	f := newTestFlagCache(l, &now)

	_, err := f.Enabled(context.Background())
	require.Error(t, err)

	l.err = nil
	l.value, l.found = "true", true
	on, err := f.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on)
	assert.Equal(t, 2, l.calls)
}

func TestFlagCacheValueParsing(t *testing.T) {
	cases := []struct {
		name  string
		value string
		found bool
		want  bool
	}{
		{"missing key", "", false, false},
		{"true", "true", true, true},
		{"upper case", "TRUE", true, true},
		{"padded", " true ", true, true},
		{"false", "false", true, false},
		{"yes", "yes", true, false},
		{"empty", "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := &fakeLoader{value: tc.value, found: tc.found}
			now := t0
			on, err := newTestFlagCache(l, &now).Enabled(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tc.want, on)
		})
	}
}
