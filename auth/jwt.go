package auth

import (
	"errors"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrTokenExpired means the token is correctly signed but past its exp.
	ErrTokenExpired = errors.New("token expired")
	// ErrInvalidToken covers every other verification failure.
	ErrInvalidToken = errors.New("invalid token")
)

// ParseToken validates an HS256 JWT issued by AccountService and returns its
// subject (users.id). golang-jwt verifies the signature before the claims, so
// ErrTokenExpired is only returned for tokens signed with our secret.
func ParseToken(secret []byte, tokenStr string) (string, error) {
	// golang-jwt accepts an empty HMAC key; refuse it so a missing secret can
	// never validate a token signed with an empty key.
	if len(secret) == 0 {
		return "", ErrInvalidToken
	}

	claims := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims,
		func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if errors.Is(err, jwt.ErrTokenExpired) {
		return "", ErrTokenExpired
	}
	if err != nil || claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
