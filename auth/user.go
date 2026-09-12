package auth

import (
	"context"
	"errors"

	"main/db"

	"github.com/jackc/pgx/v5"
)

// User is the authenticated caller, resolved from the JWT subject.
type User struct {
	ID    string
	Email string
}

// ErrUserNotFound means the JWT subject has no row in the users table.
var ErrUserNotFound = errors.New("user not found")

// UserStore looks up users by id.
type UserStore interface {
	GetByID(ctx context.Context, id string) (User, error)
}

// PgxUserStore is a UserStore backed by the shared pgx pool.
// The users table is owned by AccountService.
type PgxUserStore struct{}

// GetByID fetches a user by UUID.
func (PgxUserStore) GetByID(ctx context.Context, id string) (User, error) {
	var u User
	err := db.Pool().QueryRow(ctx,
		"SELECT id::text, email FROM users WHERE id = $1::uuid", id).Scan(&u.ID, &u.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return u, nil
}
