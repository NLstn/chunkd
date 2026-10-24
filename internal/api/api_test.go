package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NLstn/chunkd/internal/config"
	"github.com/NLstn/chunkd/internal/domain"
	"github.com/NLstn/chunkd/internal/storage"
)

const origin = "https://chunkd.example"
const credentials = `{"Username":"admin","Password":"a long test passphrase"}`

func testApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	t.Cleanup(func() { _ = conn.Close() })
	h, err := New(db, config.Config{DataDir: dir, Origin: origin, SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	return h.(*app)
}

func request(a http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Chunkd-Request", "1")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func setupBody(a *app) string {
	return fmt.Sprintf(`{"Username":"admin","Password":"a long test passphrase","SetupToken":%q}`, a.setupToken)
}

func login(t *testing.T, a *app) *http.Cookie {
	t.Helper()
	w := request(a, "POST", "/odata/Setup", setupBody(a), nil)
	if w.Code != 204 {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	w = request(a, "POST", "/odata/Login", credentials, nil)
	if w.Code != 204 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("missing session cookie: %v", cookies)
	}
	return cookies[0]
}

func TestAuthenticationAndODataContracts(t *testing.T) {
	a := testApp(t)
	if w := request(a, "GET", "/odata/Servers", "", nil); w.Code != 401 {
		t.Fatalf("anonymous read: %d", w.Code)
	}
	w := request(a, "GET", "/odata/GetSession()", "", nil)
	var info struct {
		Value SessionInfo `json:"value"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil || w.Code != 200 || !info.Value.SetupRequired || info.Value.Authenticated {
		t.Fatalf("session: %d %s err=%v", w.Code, w.Body.String(), err)
	}
	cookie := login(t, a)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 43200 || cookie.Path != "/odata" {
		t.Fatalf("unsafe cookie: %v", cookie)
	}
	if _, err := os.Stat(filepath.Join(a.cfg.DataDir, "setup-token")); !os.IsNotExist(err) {
		t.Fatal("setup token still present")
	}
	var admin domain.Admin
	if err := a.db.First(&admin, 1).Error; err != nil || admin.PasswordHash == "a long test passphrase" || !strings.HasPrefix(admin.PasswordHash, "$2a$") {
		t.Fatalf("password hash invalid: %v", err)
	}
	var sessions []domain.Session
	if err := a.db.Find(&sessions).Error; err != nil || len(sessions) != 1 || sessions[0].TokenHash == cookie.Value {
		t.Fatal("session token not hashed")
	}
	for _, path := range []string{"/odata/$metadata", "/odata/Servers", "/odata/Jobs", "/odata/Servers?$filter=State%20eq%20'Stopped'&$select=ID,Name", "/odata/GetSession()"} {
		w = request(a, "GET", path, "", cookie)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if path == "/odata/$metadata" {
			for _, contract := range []string{`EntitySet Name="Servers"`, `EntitySet Name="Jobs"`, `Action Name="Login"`, `Function Name="GetSession"`} {
				if !strings.Contains(w.Body.String(), contract) {
					t.Fatalf("missing %s: %s", contract, w.Body.String())
				}
			}
		}
		if path == "/odata/Servers" || path == "/odata/Jobs" {
			var data map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
				t.Fatal(err)
			}
			if len(data["value"].([]any)) != 0 {
				t.Fatal("expected empty collection")
			}
		}
	}
	// Recreating the application preserves login sessions without reopening setup.
	h, err := New(a.db, a.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if w = request(h, "GET", "/odata/Servers", "", cookie); w.Code != 200 {
		t.Fatal("session lost after restart")
	}
	if w = request(h, "POST", "/odata/Setup", `{"Username":"admin","Password":"a long test passphrase","SetupToken":"already-configured"}`, nil); w.Code != 409 {
		t.Fatalf("setup reopened: %d %s", w.Code, w.Body.String())
	}
	w = request(a, "POST", "/odata/Logout", "{}", cookie)
	if w.Code != 204 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout failed")
	}
	if w = request(a, "GET", "/odata/Servers", "", cookie); w.Code != 401 {
		t.Fatal("revoked session accepted")
	}
}

func TestRejectInvalidSetupLoginAndExpiredSessions(t *testing.T) {
	a := testApp(t)
	w := request(a, "POST", "/odata/Setup", `{"Username":"admin","Password":"a long test passphrase","SetupToken":"wrong"}`, nil)
	if w.Code != 403 {
		t.Fatalf("setup accepted invalid token: %d", w.Code)
	}
	w = request(a, "POST", "/odata/Setup", strings.Replace(setupBody(a), "a long test passphrase", "short", 1), nil)
	if w.Code != 400 {
		t.Fatalf("setup accepted short password: %d", w.Code)
	}
	cookie := login(t, a)
	w = request(a, "POST", "/odata/Login", `{"Username":"admin","Password":"incorrect password"}`, nil)
	if w.Code != 401 {
		t.Fatalf("invalid login accepted: %d", w.Code)
	}
	if err := a.db.Model(&domain.Session{}).Where("token_hash = ?", hash(cookie.Value)).Update("expires_at", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if w = request(a, "GET", "/odata/Servers", "", cookie); w.Code != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestTransportGuards(t *testing.T) {
	a := testApp(t)
	cookie := login(t, a)
	for _, path := range []string{"/odata/Servers", "/odata/Jobs", "/odata/$batch"} {
		if w := request(a, "POST", path, "{}", cookie); w.Code != 405 {
			t.Fatalf("mutation permitted %s: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/odata/Admins", "/odata/Sessions"} {
		if w := request(a, "GET", path, "", cookie); w.Code != 404 {
			t.Fatalf("private model exposed %s: %d", path, w.Code)
		}
	}
	for _, header := range []string{"Origin", "X-Chunkd-Request"} {
		r := httptest.NewRequest("POST", "/odata/Logout", strings.NewReader("{}"))
		r.Header.Set("Origin", origin)
		r.Header.Set("X-Chunkd-Request", "1")
		r.AddCookie(cookie)
		r.Header.Del(header)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("missing %s allowed: %d", header, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/odata/Login", strings.NewReader(credentials))
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("X-Chunkd-Request", "1")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin login allowed")
	}
	w = request(a, "POST", "/odata/Login", `{"Username":"admin","Password":"`+strings.Repeat("x", 17000)+`"}`, nil)
	if w.Code < 400 {
		t.Fatal("oversized request allowed")
	}
}

func TestLoginRateLimit(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 10; i++ {
		_ = request(a, "POST", "/odata/Login", credentials, nil)
	}
	w := request(a, "POST", "/odata/Login", credentials, nil)
	if w.Code != 429 || w.Header().Get("Retry-After") != "60" {
		t.Fatalf("limiter: %d", w.Code)
	}
}

func TestConcurrentSetupCreatesOnlyOneAdmin(t *testing.T) {
	a := testApp(t)
	body := setupBody(a)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() { codes <- request(a, "POST", "/odata/Setup", body, nil).Code })
	}
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[204] != 1 || counts[409] != 1 {
		t.Fatalf("concurrent setup: %v", counts)
	}
}
