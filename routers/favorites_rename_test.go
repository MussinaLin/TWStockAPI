package routers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"main/db"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Group name fixtures ──

// setGroupNameRow writes a custom group name directly, bypassing the PUT checks.
func setGroupNameRow(t *testing.T, userID string, groupNo int, name string) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		"INSERT INTO user_favorite_groups (user_id, group_no, name) VALUES ($1::uuid, $2, $3)",
		userID, groupNo, name)
	require.NoError(t, err)
}

// storedGroupName returns the stored custom name of a group and whether one exists.
func storedGroupName(t *testing.T, userID string, groupNo int) (string, bool) {
	t.Helper()
	var name string
	err := db.Pool().QueryRow(context.Background(),
		"SELECT name FROM user_favorite_groups WHERE user_id = $1::uuid AND group_no = $2",
		userID, groupNo).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	require.NoError(t, err)
	return name, true
}

func groupNameRows(t *testing.T, userID string) int {
	t.Helper()
	var n int
	err := db.Pool().QueryRow(context.Background(),
		"SELECT COUNT(*) FROM user_favorite_groups WHERE user_id = $1::uuid", userID).Scan(&n)
	require.NoError(t, err)
	return n
}

// nameBody returns the PUT body {"name": name}.
func nameBody(t *testing.T, name string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"name": name})
	require.NoError(t, err)
	return string(b)
}

// ── PUT /api/favorites/groups/:group_no ──

func TestRenameFavoriteGroup_OK(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"科技股"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, map[string]any{"group_no": float64(1), "name": "科技股"}, body)
	name, ok := storedGroupName(t, userID, 1)
	require.True(t, ok)
	assert.Equal(t, "科技股", name)
}

func TestRenameFavoriteGroup_TrimsName(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"  科技股　"}`)
	require.Equal(t, 200, status)
	assert.Equal(t, "科技股", body.(map[string]any)["name"])
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, "科技股", name)
}

func TestRenameFavoriteGroup_Overwrites(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, name := range []string{"科技股", "金融股", "金融股"} {
		status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, name))
		require.Equal(t, 200, status, name)
		assert.Equal(t, name, body.(map[string]any)["name"])
	}
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, "金融股", name)
	assert.Equal(t, 1, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_MaxLength(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	ten := "一二三四五六七八九十"
	status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, ten))
	require.Equal(t, 200, status, "10 個中文字應該存得下：%v", body)
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, ten, name)

	status, body = doJSONBody(t, r, "PUT", groupPath(2), nameBody(t, ten+"一"))
	assert.Equal(t, 400, status)
	assert.Equal(t, map[string]any{"error": "invalid request"}, body)
	_, ok := storedGroupName(t, userID, 2)
	assert.False(t, ok)
}

func TestRenameFavoriteGroup_Emoji(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	ten := strings.Repeat("📈", 10)
	status, body := doJSONBody(t, r, "PUT", groupPath(1), nameBody(t, ten))
	require.Equal(t, 200, status, "10 個 emoji 應該存得下：%v", body)
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, ten, name)

	status, _ = doJSONBody(t, r, "PUT", groupPath(2), nameBody(t, ten+"📈"))
	assert.Equal(t, 400, status)
	_, ok := storedGroupName(t, userID, 2)
	assert.False(t, ok)
}

func TestRenameFavoriteGroup_HTMLCharacters(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	html := `<b>&"'</b>`

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), nameBody(t, html))
	require.Equal(t, 200, status)
	assert.Equal(t, html, body.(map[string]any)["name"])
	name, _ := storedGroupName(t, userID, 1)
	assert.Equal(t, html, name)
}

func TestRenameFavoriteGroup_InvalidRequest(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	cases := []struct{ name, path, body string }{
		{"group_no abc", "/api/favorites/groups/abc", `{"name":"科技股"}`},
		{"group_no zero", groupPath(0), `{"name":"科技股"}`},
		{"group_no negative", groupPath(-1), `{"name":"科技股"}`},
		{"invalid json", groupPath(1), `{"name":`},
		{"empty body", groupPath(1), ``},
		{"no name", groupPath(1), `{}`},
		{"number name", groupPath(1), `{"name":123}`},
		{"null name", groupPath(1), `{"name":null}`},
		{"empty name", groupPath(1), `{"name":""}`},
		{"blank name", groupPath(1), `{"name":"   "}`},
		{"newline inside", groupPath(1), `{"name":"科技\n股"}`},
		{"nul", groupPath(1), `{"name":"\u0000"}`},
		{"blank name on missing group", groupPath(maxFavoriteGroups + 1), `{"name":"   "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := doJSONBody(t, r, "PUT", tc.path, tc.body)
			assert.Equal(t, 400, status)
			assert.Equal(t, map[string]any{"error": "invalid request"}, body)
		})
	}
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_GroupNoVariants(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, p := range []string{"+1", "01"} {
		status, body := doJSONBody(t, r, "PUT", "/api/favorites/groups/"+p, `{"name":"科技股"}`)
		require.Equal(t, 200, status, p)
		assert.Equal(t, float64(1), body.(map[string]any)["group_no"], p)
	}
	assert.Equal(t, 1, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_GroupNotFound(t *testing.T) {
	rule := groupRuleFor(t, 0)
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, groupNo := range []int{rule.Groups + 1, 99999} {
		status, body := doJSONBody(t, r, "PUT", groupPath(groupNo), `{"name":"科技股"}`)
		assert.Equal(t, 404, status, "group %d", groupNo)
		assert.Equal(t, map[string]any{"error": "group not found"}, body, "group %d", groupNo)
	}
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_Downgraded(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups >= 8 {
		t.Skipf("level 0 has %d groups; this test needs fewer than 8", rule.Groups)
	}
	userID := newFavoritesUser(t, 0)
	setGroupNameRow(t, userID, 8, "舊名稱")

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(8), `{"name":"新名稱"}`)
	assert.Equal(t, 404, status)
	assert.Equal(t, map[string]any{"error": "group not found"}, body)
	name, _ := storedGroupName(t, userID, 8)
	assert.Equal(t, "舊名稱", name)
}

func TestRenameFavoriteGroup_DefaultNameStored(t *testing.T) {
	userID := newFavoritesUser(t, 0)

	status, _ := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"群組三"}`)
	require.Equal(t, 200, status)
	name, ok := storedGroupName(t, userID, 1)
	require.True(t, ok)
	assert.Equal(t, "群組三", name)
}

func TestRenameFavoriteGroup_DuplicateNamesAllowed(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	for _, g := range []int{1, 2} {
		status, _ := doJSONBody(t, r, "PUT", groupPath(g), `{"name":"科技股"}`)
		assert.Equal(t, 200, status, "group %d", g)
	}
	assert.Equal(t, 2, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_OnlyOwn(t *testing.T) {
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	setGroupNameRow(t, other, 1, "別人的")

	status, _ := doJSONBody(t, newFavoritesRouter(owner), "PUT", groupPath(1), `{"name":"科技股"}`)
	require.Equal(t, 200, status)
	name, _ := storedGroupName(t, other, 1)
	assert.Equal(t, "別人的", name)
}

func TestRenameFavoriteGroup_ConcurrentRequests(t *testing.T) {
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	const n = 10
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("PUT", groupPath(1), strings.NewReader(fmt.Sprintf(`{"name":"名稱%d"}`, i)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			codes[i] = w.Code
		}()
	}
	wg.Wait()

	for i, code := range codes {
		assert.Equal(t, 200, code, "request %d", i)
	}
	assert.Equal(t, 1, groupNameRows(t, userID))
	name, _ := storedGroupName(t, userID, 1)
	assert.Regexp(t, `^名稱\d$`, name)
}

func TestRenameFavoriteGroup_UnknownLevel(t *testing.T) {
	userID := newFavoritesUser(t, 99)

	status, body := doJSONBody(t, newFavoritesRouter(userID), "PUT", groupPath(1), `{"name":"科技股"}`)
	assert.Equal(t, 500, status)
	assert.Equal(t, map[string]any{"detail": "Internal server error"}, body)
	assert.Equal(t, 0, groupNameRows(t, userID))
}

func TestRenameFavoriteGroup_UserGone(t *testing.T) {
	status, body := doJSONBody(t, newFavoritesRouter(uuid.NewString()), "PUT", groupPath(1), `{"name":"科技股"}`)
	assert.Equal(t, 401, status)
	assert.Equal(t, map[string]any{"error": "unauthorized"}, body)
}

// ── GET /api/favorites with custom names ──

func TestListFavorites_CustomNames(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups < 3 {
		t.Skipf("level 0 has %d groups; this test needs at least 3", rule.Groups)
	}
	symbols := enabledSymbols(t, 1)
	userID := newFavoritesUser(t, 0)
	addFavoriteRow(t, userID, 1, symbols[0], time.Now())
	setGroupNameRow(t, userID, 1, "科技股")
	setGroupNameRow(t, userID, 3, "群組一")

	status, body := doJSON(t, newFavoritesRouter(userID), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule.Groups)
	for i, g := range groups {
		want := groupName(i + 1)
		switch i + 1 {
		case 1:
			want = "科技股"
		case 3:
			want = "群組一"
		}
		assert.Equal(t, want, g["name"], "group %d", i+1)
		assert.Equal(t, float64(i+1), g["group_no"])
	}
	assert.Equal(t, float64(1), groups[0]["count"])
	items := groups[0]["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, symbols[0], items[0].(map[string]any)["symbol"])
}

func TestListFavorites_RenamedViaPut(t *testing.T) {
	rule := groupRuleFor(t, 0)
	if rule.Groups < 3 {
		t.Skipf("level 0 has %d groups; this test needs at least 3", rule.Groups)
	}
	userID := newFavoritesUser(t, 0)
	r := newFavoritesRouter(userID)

	status, _ := doJSONBody(t, r, "PUT", groupPath(1), `{"name":"群組三"}`)
	require.Equal(t, 200, status)

	status, body := doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	assert.Equal(t, "群組三", groups[0]["name"])
	assert.Equal(t, groupName(2), groups[1]["name"])
	assert.Equal(t, "群組三", groups[2]["name"])
}

func TestListFavorites_CustomNamesOnlyOwn(t *testing.T) {
	owner := newFavoritesUser(t, 0)
	other := newFavoritesUser(t, 0)
	setGroupNameRow(t, owner, 1, "科技股")

	status, body := doJSON(t, newFavoritesRouter(other), "GET", "/api/favorites")
	require.Equal(t, 200, status)
	assert.Equal(t, groupName(1), groupsOf(t, body)[0]["name"])
}

func TestListFavorites_CustomNameHiddenAfterDowngrade(t *testing.T) {
	rule0 := groupRuleFor(t, 0)
	rule1 := groupRuleFor(t, 1)
	if rule0.Groups >= 8 || rule1.Groups < 8 {
		t.Skipf("needs level 0 < 8 groups and level 1 >= 8 groups, got %d and %d", rule0.Groups, rule1.Groups)
	}
	userID := newFavoritesUser(t, 0)
	setGroupNameRow(t, userID, 8, "隱藏的")
	r := newFavoritesRouter(userID)

	status, body := doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups := groupsOf(t, body)
	require.Len(t, groups, rule0.Groups)
	for _, g := range groups {
		assert.NotEqual(t, "隱藏的", g["name"])
	}

	_, err := db.Pool().Exec(context.Background(),
		"UPDATE users SET member_level = 1 WHERE id = $1::uuid", userID)
	require.NoError(t, err)

	status, body = doJSON(t, r, "GET", "/api/favorites")
	require.Equal(t, 200, status)
	groups = groupsOf(t, body)
	require.Len(t, groups, rule1.Groups)
	assert.Equal(t, "隱藏的", groups[7]["name"])
}
