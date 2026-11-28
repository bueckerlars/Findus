package handler_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"findus/backend/internal/config"
	"findus/backend/internal/domain"
	"findus/backend/internal/repository/sqlite"
	"findus/backend/internal/service"
	"findus/backend/internal/transport/http/handler"
	"findus/backend/internal/transport/http/middleware"
)

func TestMCPTogglesAndCRUD(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.OpenDB(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	users, locs, items, labels := sqlite.NewUserRepo(db), sqlite.NewLocationRepo(db), sqlite.NewItemRepo(db), sqlite.NewLabelRepo(db)
	groups, templates, settings := sqlite.NewGroupRepo(db), sqlite.NewItemTemplateRepo(db), sqlite.NewSettingsRepo(db)
	srv := &handler.Server{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.Config{DataDir: dir, BaseURL: "http://findus.test"},
		Users: users, Groups: groups, Locs: locs, Items: items, Labels: labels, Templates: templates, Settings: settings,
		MCP: sqlite.NewMCPRepo(db),
		Inventory: &service.Inventory{
			Locations: locs, Items: items, ItemQRTokens: sqlite.NewItemQRTokenReservationRepo(db), Labels: labels,
			Templates: templates, Settings: settings, ItemAttachments: sqlite.NewItemAttachmentRepo(db),
		},
		JWTSecret: []byte("test-secret-test-secret-test-secret"),
	}
	h, err := srv.Handler()
	require.NoError(t, err)

	admin := &domain.User{ID: "u1", Username: "owner", Email: "o@example.com", PasswordHash: "x", Role: domain.RoleAdmin, IsActive: true, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, users.Create(ctx, admin))
	cookies := httptest.NewRecorder()
	require.NoError(t, middleware.IssueSessionCookie(cookies, srv.JWTSecret, admin, false))
	session := cookies.Result().Cookies()[0]

	// settings manages the toggles through the web API (session + CSRF).
	settingsCall := func(path, body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.AddCookie(session)
		req.AddCookie(&http.Cookie{Name: "findus_csrf", Value: "abc"})
		req.Header.Set("X-CSRF-Token", "abc")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var out map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}
	token := ""
	rpc := func(method string, params any) (int, map[string]any) {
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(string(body)))
		req.Header.Set("X-API-Key", token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var out map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}
	// call runs a tool and returns (text, isError); a rejected call counts as an error.
	call := func(name string, args map[string]any) (string, bool) {
		_, out := rpc("tools/call", map[string]any{"name": name, "arguments": args})
		if e, ok := out["error"].(map[string]any); ok {
			return e["message"].(string), true
		}
		res := out["result"].(map[string]any)
		text, _ := res["content"].([]any)[0].(map[string]any)["text"].(string)
		isErr, _ := res["isError"].(bool)
		return text, isErr
	}
	ok := func(name string, args map[string]any) map[string]any {
		text, isErr := call(name, args)
		require.False(t, isErr, "%s: %s", name, text)
		var out map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &out))
		return out
	}
	toolNames := func() []string {
		_, out := rpc("tools/list", nil)
		var names []string
		for _, x := range out["result"].(map[string]any)["tools"].([]any) {
			names = append(names, x.(map[string]any)["name"].(string))
		}
		return names
	}
	setScopes := func(scopes ...string) {
		b, _ := json.Marshal(map[string]any{"scopes": scopes})
		code, out := settingsCall("/api/mcp/scopes", string(b))
		require.Equal(t, http.StatusOK, code, out)
	}

	code, _ := rpc("tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code, "no token")

	code, out := settingsCall("/api/mcp/token", `{}`)
	require.Equal(t, http.StatusOK, code, out)
	token = out["token"].(string)

	// A new token is read-only.
	require.Contains(t, toolNames(), "search_items")
	require.NotContains(t, toolNames(), "create_item")
	_, isErr := call("create_location", map[string]any{"name": "Nope"})
	require.True(t, isErr, "disabled tool must not run")

	setScopes(domain.MCPScopes...)
	locID := ok("create_location", map[string]any{"name": "Basement"})["id"].(string)
	labelID := ok("create_label", map[string]any{"name": "Camping"})["id"].(string)
	ok("create_template", map[string]any{"id": "tool", "display_name": "Tool", "field_schema": []any{map[string]any{"key": "brand", "label": "Brand", "widget": "text"}}})
	ok("update_template", map[string]any{"id": "tool", "display_name": "Tools"})
	itemID := ok("create_item", map[string]any{"name": "Tent", "location_id": locID, "template_type": "tool", "label_ids": []string{labelID}, "fields": map[string]any{"brand": "Acme", "size": 3}})["id"].(string)

	// Partial update keeps everything that was not sent.
	got := ok("update_item", map[string]any{"id": itemID, "description": "two person", "fields": map[string]any{"size": nil}})
	require.Equal(t, "Tent", got["name"])
	require.Equal(t, map[string]any{"brand": "Acme"}, got["fields"])
	require.Len(t, got["labels"], 1)
	require.Equal(t, "Basement", got["location_path"])
	require.Len(t, ok("search_items", map[string]any{"query": "tent"})["items"], 1)

	attID := ok("add_attachment", map[string]any{"item_id": itemID, "filename": "m.pdf", "content_base64": "JVBERi0xLjQK"})["id"].(string)
	ok("update_attachment", map[string]any{"item_id": itemID, "id": attID, "title": "Manual"})
	require.Len(t, ok("list_attachments", map[string]any{"item_id": itemID})["attachments"], 1)
	_, isErr = call("get_attachment", map[string]any{"item_id": "other", "id": attID})
	require.True(t, isErr)
	_, isErr = call("get_attachment", map[string]any{"item_id": itemID, "id": attID})
	require.False(t, isErr)

	// Read toggles also filter data embedded in other results.
	setScopes("items.read")
	got = ok("get_item", map[string]any{"id": itemID})
	require.NotContains(t, got, "labels")
	require.NotContains(t, got, "location_path")
	setScopes("locations.read")
	require.NotContains(t, ok("get_location", map[string]any{"id": locID}), "items")
	setScopes("items.update")
	require.Equal(t, map[string]any{"id": itemID}, ok("update_item", map[string]any{"id": itemID}))

	setScopes(domain.MCPScopes...)
	ok("delete_attachment", map[string]any{"item_id": itemID, "id": attID})
	ok("update_location", map[string]any{"id": locID, "description": "dry"})
	ok("update_label", map[string]any{"id": labelID, "color": "#000000"})
	ok("delete_item", map[string]any{"id": itemID})
	ok("delete_label", map[string]any{"id": labelID})
	ok("delete_location", map[string]any{"id": locID})
	ok("delete_template", map[string]any{"id": "tool"})
	_, isErr = call("delete_location", map[string]any{})
	require.True(t, isErr, "missing required argument")

	// A demoted user loses write tools at once, even with the toggles still stored.
	admin.Role = domain.RoleUser
	require.NoError(t, users.Update(ctx, admin))
	for _, name := range toolNames() {
		require.Regexp(t, `^(get|list|search)_`, name)
	}
	admin.IsActive = false
	require.NoError(t, users.Update(ctx, admin))
	code, _ = rpc("tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code, "inactive user")

	// Revoking kills the token.
	admin.Role, admin.IsActive = domain.RoleAdmin, true
	require.NoError(t, users.Update(ctx, admin))
	code, _ = settingsCall("/api/mcp/revoke", `{}`)
	require.Equal(t, http.StatusOK, code)
	code, _ = rpc("tools/list", nil)
	require.Equal(t, http.StatusUnauthorized, code, "revoked token")
}
