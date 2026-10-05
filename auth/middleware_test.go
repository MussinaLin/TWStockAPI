package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testUserID = "6f1c1f5e-7a5e-4d7b-9a3b-2f0e5c1d8a11"

type fakeFlags struct {
	enabled bool
	err     error
}

func (f fakeFlags) Enabled(context.Context) (bool, error) { return f.enabled, f.err }

type fakeUsers struct {
	users map[string]User
	err   error
	calls int
}

func (s *fakeUsers) GetByID(_ context.Context, id string) (User, error) {
	s.calls++
	if s.err != nil {
		return User{}, s.err
	}
	u, ok := s.users[id]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func knownUsers() *fakeUsers {
	return &fakeUsers{users: map[string]User{
		testUserID: {ID: testUserID, Email: "user@example.com"},
	}}
}

// serve sends one GET /api/ping through RequireAuth.
func serve(t *testing.T, flags FlagSource, users UserStore, secret []byte, authHeader string) (int, map[string]any) {
	t.Helper()
	return serveWith(t, authHeader, RequireAuth(flags, users, secret))
}

// serveWith sends one GET /api/ping through the given middleware chain. The
// handler echoes the authenticated user's email so tests can see what reached
// the context.
func serveWith(t *testing.T, authHeader string, chain ...gin.HandlerFunc) (int, map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api", chain...)
	api.GET("/ping", func(c *gin.Context) {
		body := gin.H{"ok": true}
		if u, ok := CurrentUser(c); ok {
			body["email"] = u.Email
		}
		c.JSON(http.StatusOK, body)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
	return w.Code, body
}

func bearer(t *testing.T, sub string, ttl time.Duration) string {
	t.Helper()
	return "Bearer " + signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(sub, ttl))
}

func TestRequireAuthDisabledPassesThrough(t *testing.T) {
	cases := map[string]string{
		"no token":      "",
		"garbage token": "Bearer garbage",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			users := knownUsers()
			code, body := serve(t, fakeFlags{enabled: false}, users, testSecret, header)
			assert.Equal(t, http.StatusOK, code)
			assert.NotContains(t, body, "email")
			assert.Zero(t, users.calls)
		})
	}
}

func TestRequireAuthValidTokenSetsUser(t *testing.T) {
	token := signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(testUserID, time.Hour))
	cases := map[string]string{
		"Bearer": "Bearer " + token,
		"bearer": "bearer " + token,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, header)
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, "user@example.com", body["email"])
		})
	}
}

func TestRequireAuthExpiredToken(t *testing.T) {
	code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, bearer(t, testUserID, -time.Hour))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "token expired"}, body)
}

func TestRequireAuthUnauthorized(t *testing.T) {
	cases := map[string]string{
		"missing header": "",
		"wrong scheme":   "Token " + signToken(t, jwt.SigningMethodHS256, testSecret, validClaims(testUserID, time.Hour)),
		"empty bearer":   "Bearer   ",
		"bad signature":  "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims(testUserID, time.Hour)),
		"unknown user":   bearer(t, "00000000-0000-0000-0000-000000000000", time.Hour),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), testSecret, header)
			assert.Equal(t, http.StatusUnauthorized, code)
			assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
		})
	}
}

func TestRequireAuthNonUUIDSubjectSkipsLookup(t *testing.T) {
	users := knownUsers()
	code, body := serve(t, fakeFlags{enabled: true}, users, testSecret, bearer(t, "not-a-uuid", time.Hour))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
	assert.Zero(t, users.calls)
}

func TestRequireAuthServerErrors(t *testing.T) {
	serverError := map[string]any{"detail": "Internal server error"}

	t.Run("flag load fails", func(t *testing.T) {
		code, body := serve(t, fakeFlags{err: errors.New("db down")}, knownUsers(), testSecret, "")
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("secret not set", func(t *testing.T) {
		code, body := serve(t, fakeFlags{enabled: true}, knownUsers(), nil, bearer(t, testUserID, time.Hour))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		users := &fakeUsers{err: errors.New("db down")}
		code, body := serve(t, fakeFlags{enabled: true}, users, testSecret, bearer(t, testUserID, time.Hour))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})
}

// presetUser simulates RequireAuth having already authenticated the request.
func presetUser(u User) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(ContextUserKey, u)
		c.Next()
	}
}

func TestRequireLoginValidTokenSetsUser(t *testing.T) {
	users := knownUsers()
	code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(users, testSecret))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "user@example.com", body["email"])
	assert.Equal(t, 1, users.calls)
}

func TestRequireLoginExpiredToken(t *testing.T) {
	code, body := serveWith(t, bearer(t, testUserID, -time.Hour), RequireLogin(knownUsers(), testSecret))

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, map[string]any{"error": "token expired"}, body)
}

func TestRequireLoginUnauthorized(t *testing.T) {
	cases := map[string]string{
		"missing header":   "",
		"garbage token":    "Bearer garbage",
		"bad signature":    "Bearer " + signToken(t, jwt.SigningMethodHS256, []byte("other-secret"), validClaims(testUserID, time.Hour)),
		"unknown user":     bearer(t, "00000000-0000-0000-0000-000000000000", time.Hour),
		"non-uuid subject": bearer(t, "not-a-uuid", time.Hour),
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			code, body := serveWith(t, header, RequireLogin(knownUsers(), testSecret))
			assert.Equal(t, http.StatusUnauthorized, code)
			assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
		})
	}
}

func TestRequireLoginSkipsLookupWhenUserPresent(t *testing.T) {
	users := knownUsers()
	preset := User{ID: testUserID, Email: "preset@example.com"}
	code, body := serveWith(t, "", presetUser(preset), RequireLogin(users, testSecret))

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "preset@example.com", body["email"])
	assert.Zero(t, users.calls)
}

func TestRequireLoginServerErrors(t *testing.T) {
	serverError := map[string]any{"detail": "Internal server error"}

	t.Run("secret not set", func(t *testing.T) {
		code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(knownUsers(), nil))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		users := &fakeUsers{err: errors.New("db down")}
		code, body := serveWith(t, bearer(t, testUserID, time.Hour), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusInternalServerError, code)
		assert.Equal(t, serverError, body)
	})
}

// RequireLogin must enforce login even while JWT_TOKEN_ENABLE is off, and must
// not look the user up a second time when RequireAuth already did.
func TestRequireLoginBehindRequireAuth(t *testing.T) {
	t.Run("flag off, no token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, "",
			RequireAuth(fakeFlags{enabled: false}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusUnauthorized, code)
		assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
	})

	t.Run("flag off, valid token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, bearer(t, testUserID, time.Hour),
			RequireAuth(fakeFlags{enabled: false}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "user@example.com", body["email"])
		assert.Equal(t, 1, users.calls)
	})

	t.Run("flag on, valid token", func(t *testing.T) {
		users := knownUsers()
		code, body := serveWith(t, bearer(t, testUserID, time.Hour),
			RequireAuth(fakeFlags{enabled: true}, users, testSecret), RequireLogin(users, testSecret))
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "user@example.com", body["email"])
		assert.Equal(t, 1, users.calls)
	})
}

func TestCurrentUserMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, ok := CurrentUser(c)
	assert.False(t, ok)
}
