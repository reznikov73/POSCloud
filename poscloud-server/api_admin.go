package main

import (
	"strings"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ---------- вход в веб-админку ----------
//
// Порт сервера открыт в интернет, а админка умеет создавать пользователей,
// выдавать токены и удалять файлы. Поэтому доступ к /api закрыт паролем.
//
// Пароль задаётся один раз при первом запуске (файл admin.json рядом с
// server.json) и хранится как PBKDF2-хеш с солью — открытый пароль на диск
// не попадает.
//
// Сессия — случайный токен в cookie HttpOnly. Cookie SameSite=Strict и Secure,
// когда включён HTTPS, чтобы её нельзя было унести межсайтовым запросом.

const (
	adminFile      = "admin.json"
	sessionCookie  = "poscloud_admin"
	sessionTTL     = 12 * time.Hour
	defaultAdminPW = "poscloud" // временный пароль первого запуска
)

// adminStore — учётные данные администратора.
type adminStore struct {
	mu       sync.Mutex
	path     string
	Salt     string `json:"salt"`
	Hash     string `json:"hash"`
	NotFirst bool   `json:"passwordChanged"`
}

// adminSession — активная сессия.
type adminSession struct {
	expires time.Time
	addr    string
}

var (
	adminMu       sync.Mutex
	adminSessions = map[string]adminSession{}
)

// adminPath возвращает путь к файлу учётных данных.
func (a *App) adminPath() string {
	return filepath.Join(a.root(), adminFile)
}

// loadAdmin читает admin.json, создавая его при первом запуске.
func (a *App) loadAdmin() (*adminStore, error) {
	st := &adminStore{path: a.adminPath()}
	data, err := os.ReadFile(st.path)
	if err == nil {
		if err := json.Unmarshal(data, st); err == nil && st.Salt != "" && st.Hash != "" {
			return st, nil
		}
	}
	// Первый запуск: соль и хеш временного пароля.
	salt, hash, err := hashAdminPassword(defaultAdminPW)
	if err != nil {
		return nil, err
	}
	st.Salt, st.Hash = salt, hash
	if err := st.save(); err != nil {
		return nil, err
	}
	return st, nil
}

// save записывает файл с правами 0600.
func (s *adminStore) save() error {
	b, err := json.MarshalIndent(struct {
		Salt            string `json:"salt"`
		Hash            string `json:"hash"`
		PasswordChanged bool   `json:"passwordChanged"`
	}{s.Salt, s.Hash, s.NotFirst}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, b, 0600); err != nil {
		return err
	}
	return os.Chmod(s.path, 0600)
}

// hashAdminPassword считает PBKDF2-хеш пароля со случайной солью.
// Используется sha256 в режиме HMAC-итераций — без внешних зависимостей.
func hashAdminPassword(pw string) (salt, hash string, err error) {
	sb := make([]byte, 16)
	if _, err = rand.Read(sb); err != nil {
		return "", "", err
	}
	salt = base64.StdEncoding.EncodeToString(sb)
	hash = deriveAdminHash(pw, salt)
	return salt, hash, nil
}

// deriveAdminHash — PBKDF2-HMAC-SHA256 с 120 000 итераций.
// Реализовано вручную, чтобы не тянуть golang.org/x/crypto в зависимости ради
// одной функции; алгоритм стандартный (RFC 8018).
func deriveAdminHash(pw, saltB64 string) string {
	salt, _ := base64.StdEncoding.DecodeString(saltB64)
	const iter = 120000
	const keyLen = 32
	// PBKDF2 (RFC 8018): U1 = HMAC(pw, salt || INT(1)),
	// Un = HMAC(pw, U(n-1)), T = U1 xor U2 xor ... xor U(iter).
	// Пароль — КЛЮЧ HMAC, а не данные: иначе он не влияет на результат.
	mac := hmac.New(sha256.New, []byte(pw))
	mac.Write(salt)
	var idx [4]byte
	idx[3] = 1
	mac.Write(idx[:])
	u := mac.Sum(nil)
	t := make([]byte, len(u))
	copy(t, u)
	for i := 1; i < iter; i++ {
		mac.Reset()
		mac.Write(u)
		u = mac.Sum(nil)
		for j := range t {
			t[j] ^= u[j]
		}
	}
	return hex.EncodeToString(t[:keyLen])
}

// verifyAdminPassword сравнивает пароль с сохранённым хешем за постоянное время.
func (s *adminStore) verify(pw string) bool {
	got := deriveAdminHash(pw, s.Salt)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.Hash)) == 1
}

// adminLoginRequest — тело запроса входа.
type adminLoginRequest struct {
	Password string `json:"password"`
}

// handleAdminLogin проверяет пароль и ставит cookie сессии.
func (a *App) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if !loginLimiter.allow(remoteIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "слишком много попыток, подождите"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req adminLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неверный запрос"})
		return
	}
	st, err := a.loadAdmin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "не удалось прочитать учётные данные"})
		return
	}
	if !st.verify(req.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "неверный пароль"})
		return
	}

	tok := newSessionToken()
	adminMu.Lock()
	adminSessions[tok] = adminSession{expires: time.Now().Add(sessionTTL), addr: remoteIP(r)}
	adminMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   a.tlsActive(),
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "defaultPassword": !st.NotFirst})
}

// handleAdminLogout удаляет сессию.
func (a *App) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		adminMu.Lock()
		delete(adminSessions, c.Value)
		adminMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.tlsActive(),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdminPassword меняет пароль администратора.
func (a *App) handleAdminPassword(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неверный запрос"})
		return
	}
	if len(req.Next) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "пароль короче 8 символов"})
		return
	}
	st, err := a.loadAdmin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "не удалось прочитать учётные данные"})
		return
	}
	if !st.verify(req.Current) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "текущий пароль неверен"})
		return
	}
	salt, hash, err := hashAdminPassword(req.Next)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "не удалось сохранить пароль"})
		return
	}
	st.Salt, st.Hash, st.NotFirst = salt, hash, true
	if err := st.save(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "не удалось сохранить пароль"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAdminStatus сообщает, есть ли активная сессия.
func (a *App) handleAdminStatus(w http.ResponseWriter, r *http.Request) {
	_, ok := a.adminSession(r)
	writeJSON(w, http.StatusOK, map[string]any{"authorized": ok})
}

// adminSession проверяет cookie сессии.
func (a *App) adminSession(r *http.Request) (adminSession, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return adminSession{}, false
	}
	adminMu.Lock()
	s, ok := adminSessions[c.Value]
	adminMu.Unlock()
	if !ok {
		return adminSession{}, false
	}
	if time.Now().After(s.expires) {
		adminMu.Lock()
		delete(adminSessions, c.Value)
		adminMu.Unlock()
		return adminSession{}, false
	}
	return s, true
}

// tlsActive сообщает, работает ли сервер по HTTPS.
func (a *App) tlsActive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.srvTLS
}

// newSessionToken возвращает случайный токен сессии.
func newSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// loginLimiter — счётчик попыток входа: 10 за минуту с одного адреса.
// Переиспользуется pairLimiter, он делает то же самое.
var loginLimiter = newPairLimiter(10, time.Minute)

// withAdminAuth требует сессию админки для /api/*, кроме входа и статуса.
func (a *App) withAdminAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// Сессия админки нужна только для /api/*. Всё остальное (/health,
		// /pair, /list, /upload и прочий обмен с клиентами) защищено
		// токенами устройств в withAuth — здесь его не трогаем.
		if !strings.HasPrefix(p, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		switch p {
		case "/api/login", "/api/logout", "/api/admin-status":
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := a.adminSession(r); !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "требуется вход в админку"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

