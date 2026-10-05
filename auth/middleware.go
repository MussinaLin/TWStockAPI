package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ContextUserKey is the gin.Context key holding the authenticated User.
const ContextUserKey = "authUser"

// RequireAuth returns middleware that, while the JWT_TOKEN_ENABLE flag is on,
// requires a valid AccountService Bearer JWT whose subject exists in users.
// While the flag is off, requests pass through without reading Authorization.
func RequireAuth(flags FlagSource, users UserStore, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		enabled, err := flags.Enabled(c.Request.Context())
		if err != nil {
			abortServerError(c, fmt.Errorf("auth: load %s: %w", FlagKey, err))
			return
		}
		if !enabled {
			c.Next()
			return
		}
		if authenticate(c, users, secret) {
			c.Next()
		}
	}
}

// RequireLogin returns middleware that always requires a valid AccountService
// Bearer JWT, regardless of JWT_TOKEN_ENABLE. If RequireAuth already
// authenticated the request, it passes through without looking the user up again.
func RequireLogin(users UserStore, secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := CurrentUser(c); ok {
			c.Next()
			return
		}
		if authenticate(c, users, secret) {
			c.Next()
		}
	}
}

// CurrentUser returns the User stored by RequireAuth or RequireLogin.
func CurrentUser(c *gin.Context) (User, bool) {
	v, ok := c.Get(ContextUserKey)
	if !ok {
		return User{}, false
	}
	u, ok := v.(User)
	return u, ok
}

// authenticate verifies the Bearer JWT and stores the matching User in the
// context. On failure it aborts with the error response and returns false.
func authenticate(c *gin.Context, users UserStore, secret []byte) bool {
	if len(secret) == 0 {
		abortServerError(c, errors.New("auth: JWT_SECRET is not set"))
		return false
	}

	tokenStr, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		abortUnauthorized(c, errors.New("auth: missing bearer token"))
		return false
	}

	sub, err := ParseToken(secret, tokenStr)
	if errors.Is(err, ErrTokenExpired) {
		_ = c.Error(fmt.Errorf("auth: %w", err))
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token expired"})
		return false
	}
	if err != nil {
		abortUnauthorized(c, fmt.Errorf("auth: %w", err))
		return false
	}

	id, err := uuid.Parse(sub)
	if err != nil {
		abortUnauthorized(c, errors.New("auth: subject is not a uuid"))
		return false
	}

	user, err := users.GetByID(c.Request.Context(), id.String())
	if errors.Is(err, ErrUserNotFound) {
		abortUnauthorized(c, fmt.Errorf("auth: %w", err))
		return false
	}
	if err != nil {
		abortServerError(c, fmt.Errorf("auth: lookup user: %w", err))
		return false
	}

	c.Set(ContextUserKey, user)
	return true
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(header string) (string, bool) {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

// abortUnauthorized records the reason for slog-gin and responds 401.
func abortUnauthorized(c *gin.Context, reason error) {
	_ = c.Error(reason)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// abortServerError records the reason for slog-gin and responds 500.
func abortServerError(c *gin.Context, reason error) {
	_ = c.Error(reason)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"detail": "Internal server error"})
}
