package handler_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"findus/backend/internal/config"
	"findus/backend/internal/repository/sqlite"
	"findus/backend/internal/secrets"
	"findus/backend/internal/service"
	"findus/backend/internal/transport/http/handler"
)

type env struct {
	h   http.Handler
	dir string
	s   *handler.Server
}

func newEnv(t *testing.T) env {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	key, err := secrets.LoadOrCreateJWTSecret(dir, "")
	require.NoError(t, err)
	db, err := sqlite.OpenDB(ctx, dir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	u, g, l, i, q := sqlite.NewUserRepo(db), sqlite.NewGroupRepo(db), sqlite.NewLocationRepo(db), sqlite.NewItemRepo(db), sqlite.NewItemQRTokenReservationRepo(db)
	lb, tp, inv, st := sqlite.NewLabelRepo(db), sqlite.NewItemTemplateRepo(db), sqlite.NewInviteRepo(db), sqlite.NewSettingsRepo(db)
	s := &handler.Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.Config{DataDir: dir}, DB: db,
		Users: u, Groups: g, Locs: l, Items: i, ItemQRTokens: q, Labels: lb, Templates: tp, Invites: inv, Settings: st, MCP: sqlite.NewMCPRepo(db),
		Auth:      &service.Auth{Users: u, Settings: st, Invites: inv},
		Admin:     &service.Admin{Users: u, Settings: st, Invites: inv, Groups: g},
		Inventory: &service.Inventory{Locations: l, Items: i, ItemQRTokens: q, Labels: lb, Templates: tp, Settings: st, ItemAttachments: sqlite.NewItemAttachmentRepo(db)},
		QR:        &service.QR{}, JWTSecret: key}
	h, err := s.Handler()
	require.NoError(t, err)
	return env{h, dir, s}
}

func (e env) do(method, path, ct string, body []byte, session string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	if ct != "" {
		r.Header.Set("Content-Type", ct)
	}
	// attacker picks the CSRF value himself
	r.AddCookie(&http.Cookie{Name: "findus_csrf", Value: "x"})
	r.Header.Set("X-CSRF-Token", "x")
	if session != "" {
		r.AddCookie(&http.Cookie{Name: "findus_session", Value: session})
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e env) json(method, path, body, session string) *httptest.ResponseRecorder {
	return e.do(method, path, "application/json", []byte(body), session)
}

func sess(w *httptest.ResponseRecorder) string {
	return cookieValue(w.Result().Cookies(), "findus_session")
}

func (e env) register(name string, invite string) *httptest.ResponseRecorder {
	return e.json("POST", "/api/auth/register", `{"username":"`+name+`","email":"`+name+`@example.com","password":"password1234","invite":"`+invite+`"}`, "")
}

func importBody(itemID, locID, photo string) string {
	return `{"export_version":1,"exported_at":"2026-01-01T00:00:00Z","locations":[{"ID":"` + locID + `","Name":"L","Description":"","QRToken":"lq","CreatedAt":"2026-01-01T00:00:00Z","UpdatedAt":"2026-01-01T00:00:00Z"}],"labels":[],"item_templates":[{"id":"poc","display_name":"P","fields_json":[],"sort_order":1}],"items":[{"id":"` + itemID + `","name":"n","location_id":"` + locID + `","template_type":"poc","photo_path":` + photo + `,"qr_token":"iq"}]}`
}

func TestSecurityImportCannotChooseFilePaths(t *testing.T) {
	e := newEnv(t)
	admin := sess(e.register("owner", ""))
	w := e.json("POST", "/api/admin/inventory-import", importBody("evil", "loc1", `"../data/.jwt_secret"`), admin)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	it, err := e.s.Items.GetByID(context.Background(), "evil")
	require.NoError(t, err)
	require.Equal(t, "images/evil.webp", *it.PhotoPath)
	require.Equal(t, http.StatusNotFound, e.json("GET", "/items/evil/photo", "", admin).Code)

	// A path stored before this fix must not be served either.
	for _, bad := range []string{"../data/.jwt_secret", "images/../.jwt_secret", "/etc/passwd"} {
		it.PhotoPath = &bad
		require.NoError(t, e.s.Items.Update(context.Background(), it))
		require.Equal(t, http.StatusNotFound, e.json("GET", "/items/evil/photo", "", admin).Code, bad)
	}

	for _, bad := range []string{"..", "../x", "a/b", "a.b"} {
		w = e.json("POST", "/api/admin/inventory-import", importBody(bad, "loc2", `null`), admin)
		require.Equal(t, http.StatusBadRequest, w.Code, bad)
	}
	require.Error(t, service.RemoveItemAttachmentDir(e.dir, ".."))
	require.DirExists(t, e.dir)
}

func race(e env, invite string) (ok int) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.register("racer"+string(rune('a'+i)), invite).Code == http.StatusOK {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return ok
}

func TestSecurityOneInviteOneAccount(t *testing.T) {
	e := newEnv(t)
	admin := sess(e.register("owner", ""))
	e.json("POST", "/api/admin/settings/registration", `{"mode":"invite"}`, admin)
	e.json("POST", "/api/admin/invites", `{"role":"admin","ttl_hours":24}`, admin)
	list, err := e.s.Invites.ListRecent(context.Background(), 5)
	require.NoError(t, err)
	require.Equal(t, 1, race(e, list[0].Token))
	// A used invite must not reveal which accounts exist.
	require.Equal(t, http.StatusForbidden, e.register("owner", list[0].Token).Code)
}

func TestSecurityFirstAccountNeedsSetupToken(t *testing.T) {
	e := newEnv(t)
	e.s.Auth.SetupToken = "the-setup-token"
	require.Equal(t, http.StatusForbidden, e.register("intruder", "").Code)
	require.Equal(t, http.StatusForbidden, e.register("intruder", "guess").Code)
	require.Equal(t, 1, race(e, "the-setup-token"))
}

func TestSecuritySessionsEndOnLogoutAndPasswordChange(t *testing.T) {
	e := newEnv(t)
	first := sess(e.register("owner", ""))
	require.Equal(t, http.StatusOK, e.json("POST", "/api/auth/logout", "", first).Code)
	require.Equal(t, http.StatusSeeOther, e.do("GET", "/api/me", "", nil, first).Code)

	login := func(pw string) string {
		return sess(e.json("POST", "/api/auth/login", `{"username":"owner","password":"`+pw+`"}`, ""))
	}
	stolen, mine := login("password1234"), login("password1234")
	require.Equal(t, http.StatusOK, e.json("POST", "/api/mcp/token", "", mine).Code)
	w := e.json("POST", "/api/profile", `{"username":"owner","email":"owner@example.com","current_password":"password1234","new_password":"another-password-99"}`, mine)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, http.StatusSeeOther, e.do("GET", "/api/me", "", nil, stolen).Code)
	require.Equal(t, http.StatusSeeOther, e.do("GET", "/api/me", "", nil, mine).Code)
	require.Equal(t, http.StatusOK, e.do("GET", "/api/me", "", nil, sess(w)).Code, "this browser stays signed in")
	var n int
	require.NoError(t, e.s.DB.QueryRow(`SELECT COUNT(*) FROM mcp_access`).Scan(&n))
	require.Zero(t, n, "MCP token revoked")
}

func TestSecurityLoginIsRateLimited(t *testing.T) {
	e := newEnv(t)
	e.register("owner", "")
	right := `{"username":"owner","password":"password1234"}`
	for range 15 { // successful logins do not count
		require.Equal(t, http.StatusOK, e.json("POST", "/api/auth/login", right, "").Code)
	}
	for range 10 {
		require.Equal(t, http.StatusUnauthorized, e.json("POST", "/api/auth/login", `{"username":"owner","password":"wrongwrong"}`, "").Code)
	}
	require.Equal(t, http.StatusTooManyRequests, e.json("POST", "/api/auth/login", right, "").Code)
}

func TestSecurityHeadersAndBodyLimit(t *testing.T) {
	e := newEnv(t)
	h := e.json("GET", "/", "", "").Header()
	for _, k := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		require.NotEmpty(t, h.Get(k), k)
	}
	big := `{"username":"` + strings.Repeat("a", 2<<20) + `","password":"x"}`
	require.Equal(t, http.StatusBadRequest, e.json("POST", "/api/auth/login", big, "").Code)
}

// multipartBody is one file field plus optional text fields.
func multipartBody(t *testing.T, field string, content []byte, values map[string]string) (string, []byte) {
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	for k, v := range values {
		require.NoError(t, mw.WriteField(k, v))
	}
	f, err := mw.CreateFormFile(field, "upload.jpg")
	require.NoError(t, err)
	_, err = f.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	return mw.FormDataContentType(), b.Bytes()
}

// Needs a real server: net/http's own temp file cleanup is part of what is tested.
func TestSecurityUploadsLeaveNoTempFiles(t *testing.T) {
	e := newEnv(t)
	session := sess(e.register("owner", ""))
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	srv := httptest.NewServer(e.h)
	ct, body := multipartBody(t, "avatar", make([]byte, 36<<20), map[string]string{"username": "owner", "email": "owner@example.com", "current_password": "password1234"})
	for _, who := range []string{"", session} {
		req, err := http.NewRequest("POST", srv.URL+"/api/profile", bytes.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-CSRF-Token", "x")
		req.AddCookie(&http.Cookie{Name: "findus_csrf", Value: "x"})
		if who != "" {
			req.AddCookie(&http.Cookie{Name: "findus_session", Value: who})
		}
		if res, err := http.DefaultClient.Do(req); err == nil {
			_ = res.Body.Close()
		}
	}
	srv.Close()
	files, err := os.ReadDir(tmp)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestSecurityCraftedTIFFIsRejected(t *testing.T) {
	e := newEnv(t)
	s := sess(e.register("owner", ""))
	ct, body := multipartBody(t, "avatar", evilTIFF(), map[string]string{"username": "owner", "email": "owner@example.com", "current_password": "password1234"})
	w := e.do("POST", "/api/profile", ct, body, s)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// evilTIFF is a palette TIFF whose pixels point past its two-colour map; decoding and resizing it
// panicked in a goroutine and killed the process.
func evilTIFF() []byte {
	const side = 1300
	pix := bytes.Repeat([]byte{200}, side*side)
	le := binary.LittleEndian
	var out bytes.Buffer
	out.WriteString("II*\x00")
	cmap := make([]byte, 3*2*2) // two colours only
	pixOff := 8
	cmapOff := pixOff + len(pix)
	ifd := cmapOff + len(cmap)
	_ = binary.Write(&out, le, uint32(ifd))
	out.Write(pix)
	out.Write(cmap)
	tags := [][4]uint32{{256, 3, 1, side}, {257, 3, 1, side}, {258, 3, 1, 8}, {259, 3, 1, 1}, {262, 3, 1, 3}, {273, 4, 1, uint32(pixOff)}, {278, 3, 1, side}, {279, 4, 1, uint32(len(pix))}, {320, 3, 6, uint32(cmapOff)}}
	_ = binary.Write(&out, le, uint16(len(tags)))
	for _, tg := range tags {
		_ = binary.Write(&out, le, uint16(tg[0]))
		_ = binary.Write(&out, le, uint16(tg[1]))
		_ = binary.Write(&out, le, tg[2])
		_ = binary.Write(&out, le, tg[3])
	}
	_ = binary.Write(&out, le, uint32(0))
	return out.Bytes()
}

// The public setup: secure cookies carry the __Host- prefix, and planted unprefixed cookies are ignored.
func TestSecurityHTTPSCookies(t *testing.T) {
	e := newEnv(t)
	e.s.Config.CookieSecure = true
	h, err := e.s.Handler()
	require.NoError(t, err)
	call := func(method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("X-CSRF-Token", "x")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	register := `{"username":"owner","email":"owner@example.com","password":"password1234","invite":""}`
	require.Equal(t, http.StatusForbidden, call("POST", "/api/auth/register", register, &http.Cookie{Name: "findus_csrf", Value: "x"}).Code)
	w := call("POST", "/api/auth/register", register, &http.Cookie{Name: "__Host-findus_csrf", Value: "x"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotEmpty(t, w.Header().Get("Strict-Transport-Security"))

	token := cookieValue(w.Result().Cookies(), "__Host-findus_session")
	require.NotEmpty(t, token)
	for _, c := range w.Result().Cookies() {
		require.True(t, c.Secure && c.HttpOnly && c.Path == "/" && c.Domain == "", c.Name)
	}
	require.Equal(t, http.StatusOK, call("GET", "/api/me", "", &http.Cookie{Name: "__Host-findus_session", Value: token}).Code)
	require.Equal(t, http.StatusSeeOther, call("GET", "/api/me", "", &http.Cookie{Name: "findus_session", Value: token}).Code)
}
