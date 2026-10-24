package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/NLstn/chunkd/internal/config"
	"github.com/NLstn/chunkd/internal/domain"
	odata "github.com/nlstn/go-odata"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const cookieName = "chunkd_session"
const sessionLifetime = 12 * time.Hour

type SessionInfo struct {
	Authenticated bool `json:"Authenticated"`
	SetupRequired bool `json:"SetupRequired"`
}

type app struct {
	db         *gorm.DB
	cfg        config.Config
	service    *odata.Service
	setupToken string
	setupMu    sync.Mutex
	rateMu     sync.Mutex
	window     time.Time
	attempts   int
}

// New registers every application endpoint through go-odata. The surrounding
// handler only enforces transport, session, request-size and CSRF policy.
func New(db *gorm.DB, cfg config.Config) (http.Handler, error) {
	s, err := odata.NewService(db)
	if err != nil {
		return nil, err
	}
	s.SetBasePath("/odata")
	if err := s.SetNamespace("Chunkd"); err != nil {
		return nil, err
	}
	if err := s.SetDefaultMaxTop(100); err != nil {
		return nil, err
	}
	for _, model := range []any{&domain.Server{}, &domain.Job{}} {
		if err := s.RegisterEntity(model); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"Servers", "Jobs"} {
		if err := s.DisableHTTPMethods(name, "POST", "PUT", "PATCH", "DELETE"); err != nil {
			return nil, err
		}
	}
	a := &app{db: db, cfg: cfg, service: s}
	needed, err := a.setupRequired()
	if err != nil {
		return nil, err
	}
	if needed {
		// Rotate on daemon restart. The token is never returned through the API.
		a.setupToken, err = randomToken()
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(cfg.DataDir, "setup-token"), []byte(a.setupToken+"\n"), 0600); err != nil {
			return nil, err
		}
	}
	if err := s.RegisterFunction(odata.FunctionDefinition{
		Name: "GetSession", ReturnType: reflect.TypeOf(SessionInfo{}),
		Handler: func(w http.ResponseWriter, r *http.Request, _ any, _ map[string]any) (any, error) {
			needed, err := a.setupRequired()
			if err != nil {
				return nil, err
			}
			return SessionInfo{Authenticated: a.authenticated(r), SetupRequired: needed}, nil
		},
	}); err != nil {
		return nil, err
	}
	parameters := []odata.ParameterDefinition{{Name: "Username", Type: reflect.TypeOf(""), Required: true}, {Name: "Password", Type: reflect.TypeOf(""), Required: true}}
	setupParameters := append([]odata.ParameterDefinition{}, parameters...)
	setupParameters = append(setupParameters, odata.ParameterDefinition{Name: "SetupToken", Type: reflect.TypeOf(""), Required: true})
	for _, action := range []odata.ActionDefinition{
		{Name: "Setup", Parameters: setupParameters, Handler: a.setup},
		{Name: "Login", Parameters: parameters, Handler: a.login},
		{Name: "Logout", Handler: a.logout},
	} {
		if err := s.RegisterAction(action); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *app) setupRequired() (bool, error) {
	var count int64
	err := a.db.Model(&domain.Admin{}).Count(&count).Error
	return count == 0, err
}

func (a *app) authenticated(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || len(c.Value) != 64 {
		return false
	}
	var session domain.Session
	return a.db.Where("token_hash = ?", hash(c.Value)).First(&session).Error == nil && session.ExpiresAt.After(time.Now())
}

func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("OData-Version", "4.0")
	path := r.URL.Path
	if !strings.HasPrefix(path, "/odata/") && path != "/odata" {
		http.NotFound(w, r)
		return
	}
	// These exact paths are the only anonymous operations. All aliases, metadata,
	// entity reads and batch requests must pass the same session boundary.
	public := (r.Method == "GET" && path == "/odata/GetSession()") || (r.Method == "POST" && (path == "/odata/Setup" || path == "/odata/Login"))
	if !public && !a.authenticated(r) {
		fail(w, 401, "Unauthorized", "Sign in required")
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
		if r.Header.Get("Origin") != a.cfg.Origin || r.Header.Get("X-Chunkd-Request") != "1" {
			fail(w, 403, "Forbidden", "Same-origin request required")
			return
		}
		// M1 has only these three writable operations. Batch is deliberately deferred
		// so no nested request can bypass authentication or mutation policy.
		if r.Method != "POST" || (path != "/odata/Setup" && path != "/odata/Login" && path != "/odata/Logout") {
			fail(w, 405, "MethodNotAllowed", "Operation unavailable in M1")
			return
		}
	}
	if path == "/odata/Setup" || path == "/odata/Login" {
		if r.Method == "POST" && !a.allowAttempt() {
			w.Header().Set("Retry-After", "60")
			fail(w, 429, "TooManyRequests", "Try again in one minute")
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	a.service.ServeHTTP(w, r)
}

// A bounded global limiter avoids spoofable forwarded-IP headers and unbounded
// per-IP maps. Ten attempts/minute is appropriate for this one-admin host.
func (a *app) allowAttempt() bool {
	a.rateMu.Lock()
	defer a.rateMu.Unlock()
	if time.Since(a.window) >= time.Minute {
		a.window = time.Now()
		a.attempts = 0
	}
	a.attempts++
	return a.attempts <= 10
}

func (a *app) setup(w http.ResponseWriter, r *http.Request, _ any, p map[string]any) error {
	a.setupMu.Lock()
	defer a.setupMu.Unlock()
	needed, err := a.setupRequired()
	if err != nil {
		return err
	}
	if !needed {
		fail(w, 409, "Conflict", "Administrator already configured")
		return nil
	}
	token := p["SetupToken"].(string)
	if subtle.ConstantTimeCompare([]byte(hash(token)), []byte(hash(a.setupToken))) != 1 {
		fail(w, 403, "Forbidden", "Invalid setup token")
		return nil
	}
	username := strings.TrimSpace(p["Username"].(string))
	password := p["Password"].(string)
	if len(username) < 1 || len(username) > 64 || len(password) < 12 || len(password) > 72 {
		fail(w, 400, "BadRequest", "Username must be 1–64 bytes; password must be 12–72 bytes")
		return nil
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if err := a.db.Create(&domain.Admin{ID: 1, Username: username, PasswordHash: string(passwordHash)}).Error; err != nil {
		return err
	}
	// The database guard remains authoritative if removal fails or the process
	// crashes after inserting the administrator.
	a.setupToken = ""
	_ = os.Remove(filepath.Join(a.cfg.DataDir, "setup-token"))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *app) login(w http.ResponseWriter, r *http.Request, _ any, p map[string]any) error {
	var admin domain.Admin
	err := a.db.First(&admin, 1).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if len(p["Password"].(string)) > 72 {
		fail(w, 401, "Unauthorized", "Invalid credentials")
		return nil
	}
	if err != nil || bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(p["Password"].(string))) != nil || admin.Username != strings.TrimSpace(p["Username"].(string)) {
		fail(w, 401, "Unauthorized", "Invalid credentials")
		return nil
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	expires := time.Now().UTC().Add(sessionLifetime)
	err = a.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at <= ?", time.Now().UTC()).Delete(&domain.Session{}).Error; err != nil {
			return err
		}
		if old, err := r.Cookie(cookieName); err == nil {
			if err := tx.Delete(&domain.Session{}, "token_hash = ?", hash(old.Value)).Error; err != nil {
				return err
			}
		}
		return tx.Create(&domain.Session{TokenHash: hash(token), ExpiresAt: expires}).Error
	})
	if err != nil {
		return err
	}
	http.SetCookie(w, a.cookie(token, int(sessionLifetime.Seconds()), expires))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, _ any, _ map[string]any) error {
	if c, err := r.Cookie(cookieName); err == nil {
		if err := a.db.Delete(&domain.Session{}, "token_hash = ?", hash(c.Value)).Error; err != nil {
			return err
		}
	}
	http.SetCookie(w, a.cookie("", -1, time.Unix(1, 0)))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *app) cookie(value string, age int, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: cookieName, Value: value, Path: "/odata", HttpOnly: true, Secure: a.cfg.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: age, Expires: expires}
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func hash(s string) string { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }

func fail(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json;odata.metadata=minimal")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
