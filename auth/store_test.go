package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"main/db"

	"github.com/jackc/pgx/v5"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain opens the DB pool only when DATABASE_URL is set. Unit tests in
// this package always run; DB tests skip themselves via requireDB.
func TestMain(m *testing.M) {
	_ = godotenv.Load("../.env")
	if os.Getenv("DATABASE_URL") != "" {
		if err := db.InitPool(); err != nil {
			fmt.Fprintln(os.Stderr, "db init failed:", err)
			os.Exit(1)
		}
	}
	code := m.Run()
	db.ClosePool()
	os.Exit(code)
}

func requireDB(t *testing.T) {
	t.Helper()
	if db.Pool() == nil {
		t.Skip("DATABASE_URL not set, skipping DB test")
	}
}

func TestPgxUserStoreGetByID(t *testing.T) {
	requireDB(t)
	ctx := context.Background()

	var want User
	err := db.Pool().QueryRow(ctx, "SELECT id::text, email FROM users LIMIT 1").Scan(&want.ID, &want.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("no rows in users")
	}
	require.NoError(t, err)

	got, err := PgxUserStore{}.GetByID(ctx, want.ID)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestPgxUserStoreGetByIDNotFound(t *testing.T) {
	requireDB(t)

	_, err := PgxUserStore{}.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestLoadConfigValueMissingKey(t *testing.T) {
	requireDB(t)

	v, found, err := loadConfigValue(context.Background(), "__no_such_key__")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, v)
}

func TestLoadJWTFlag(t *testing.T) {
	requireDB(t)

	_, _, err := LoadJWTFlag(context.Background())
	assert.NoError(t, err)
}
