package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret")

// signToken signs claims the same way AccountService does.
func signToken(t *testing.T, method jwt.SigningMethod, secret []byte, claims jwt.RegisteredClaims) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	require.NoError(t, err)
	return s
}

// validClaims mirrors AccountService's claims: sub, iat, exp = now + ttl.
func validClaims(sub string, ttl time.Duration) jwt.RegisteredClaims {
	now := time.Now()
	return jwt.RegisteredClaims{
		Subject:   sub,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
}

func TestParseTokenReturnsSubject(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("user-123", time.Hour))

	sub, err := ParseToken(testSecret, token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", sub)
}

func TestParseTokenExpired(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("user-123", -time.Hour))

	_, err := ParseToken(testSecret, token)
	assert.ErrorIs(t, err, ErrTokenExpired)
}

// An expired token signed with another key must not be reported as expired.
func TestParseTokenExpiredWithWrongSecretIsInvalid(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims("user-123", -time.Hour))

	_, err := ParseToken(testSecret, token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestParseTokenRejectsInvalid(t *testing.T) {
	noExp := jwt.RegisteredClaims{Subject: "user-123", IssuedAt: jwt.NewNumericDate(time.Now())}

	cases := map[string]string{
		"wrong secret": signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims("user-123", time.Hour)),
		"HS384":        signToken(t, jwt.SigningMethodHS384, testSecret, validClaims("user-123", time.Hour)),
		"missing exp":  signToken(t, jwt.SigningMethodHS256, testSecret, noExp),
		"missing sub":  signToken(t, jwt.SigningMethodHS256, testSecret, validClaims("", time.Hour)),
		"garbage":      "not-a-jwt",
		"empty":        "",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseToken(testSecret, token)
			assert.ErrorIs(t, err, ErrInvalidToken)
		})
	}
}

// golang-jwt accepts an empty HMAC key, so ParseToken must refuse it itself.
func TestParseTokenRejectsEmptySecret(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, []byte{}, validClaims("user-123", time.Hour))

	_, err := ParseToken(nil, token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}
