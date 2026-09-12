package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"main/db"

	"github.com/jackc/pgx/v5"
)

// FlagKey is the config-table key that turns JWT verification on.
const FlagKey = "JWT_TOKEN_ENABLE"

// FlagSource reports whether JWT verification is enabled.
type FlagSource interface {
	Enabled(ctx context.Context) (bool, error)
}

// FlagLoader returns the raw config value; found is false when the key is absent.
type FlagLoader func(ctx context.Context) (value string, found bool, err error)

// FlagCache is a FlagSource that caches the loaded flag for ttl.
// Load errors are returned to the caller and never cached.
type FlagCache struct {
	load FlagLoader
	ttl  time.Duration
	now  func() time.Time

	mu        sync.Mutex
	value     bool
	expiresAt time.Time
}

func NewFlagCache(load FlagLoader, ttl time.Duration) *FlagCache {
	return &FlagCache{load: load, ttl: ttl, now: time.Now}
}

// Enabled returns the cached flag, reloading it once the TTL has passed.
// The lock is held across the load so only one request queries the DB.
func (f *FlagCache) Enabled(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.now().Before(f.expiresAt) {
		return f.value, nil
	}

	v, found, err := f.load(ctx)
	if err != nil {
		return false, err
	}
	f.value = found && strings.EqualFold(strings.TrimSpace(v), "true")
	f.expiresAt = f.now().Add(f.ttl)
	return f.value, nil
}

// LoadJWTFlag reads JWT_TOKEN_ENABLE from the config table (owned by TWStockAnalysis).
func LoadJWTFlag(ctx context.Context) (string, bool, error) {
	return loadConfigValue(ctx, FlagKey)
}

func loadConfigValue(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := db.Pool().QueryRow(ctx, "SELECT value FROM config WHERE key = $1", key).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}
