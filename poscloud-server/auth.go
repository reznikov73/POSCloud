package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---------- Модель: пользователь + устройства (токены) ----------

// DeviceToken — токен одного устройства пользователя. Хранится только ХЭШ токена.
type DeviceToken struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Hash      string `json:"hash"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	LastSeen  string `json:"lastSeen"`
}

// User — человек (аккаунт). У него своё изолированное пространство на сервере.
type User struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Enabled   bool          `json:"enabled"`
	CreatedAt string        `json:"createdAt"`
	Tokens    []DeviceToken `json:"tokens"`
}

type usersFile struct {
	Users []User `json:"users"`
}

// UserInfo — DTO для интерфейса сервера (без секретов).
type UserInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	CreatedAt  string `json:"createdAt"`
	TokenCount int    `json:"tokenCount"`
	FileCount  int    `json:"fileCount"`
}

// TokenInfo — DTO токена для интерфейса (без хэша).
type TokenInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	LastSeen  string `json:"lastSeen"`
}

type authStore struct {
	mu    sync.Mutex
	path  string
	users []User
}

func newAuthStore(path string) *authStore {
	s := &authStore{path: path}
	s.load()
	return s
}

func (s *authStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var f usersFile
	if json.Unmarshal(data, &f) == nil {
		s.users = f.Users
	}
}

// saveLocked пишет реестр на диск. Вызывать ТОЛЬКО под s.mu.
func (s *authStore) saveLocked() {
	_ = os.MkdirAll(filepath.Dir(s.path), 0755)
	b, _ := json.MarshalIndent(usersFile{Users: s.users}, "", "  ")
	_ = os.WriteFile(s.path, b, 0600)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func genToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return "pcs_" + base64.RawURLEncoding.EncodeToString(b)
}

func genID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

var slugRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// translit — простая транслитерация кириллицы, чтобы id/папки были читаемыми.
var translit = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ё': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n", 'о': "o",
	'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f", 'х': "h", 'ц': "ts",
	'ч': "ch", 'ш': "sh", 'щ': "sch", 'ъ': "", 'ы': "y", 'ь': "", 'э': "e", 'ю': "yu",
	'я': "ya", 'і': "i", 'ї': "yi", 'є': "ye", 'ґ': "g",
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if t, ok := translit[r]; ok {
			b.WriteString(t)
			continue
		}
		b.WriteRune(r)
	}
	out := slugRe.ReplaceAllString(b.String(), "-")
	return strings.Trim(out, "-")
}

func nowStamp() string { return time.Now().Format("2006-01-02 15:04:05") }

// ---------- Поиск и изменение ----------

func (s *authStore) userIndexLocked(id string) int {
	for i := range s.users {
		if s.users[i].ID == id {
			return i
		}
	}
	return -1
}

// findUserByToken возвращает userID и tokenID по предъявленному токену.
func (s *authStore) findUserByToken(tok string) (string, string, bool) {
	h := hashToken(tok)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ui := range s.users {
		u := &s.users[ui]
		if !u.Enabled {
			continue
		}
		for ti := range u.Tokens {
			t := &u.Tokens[ti]
			if t.Enabled && t.Hash == h {
				t.LastSeen = nowStamp()
				s.saveLocked()
				return u.ID, t.ID, true
			}
		}
	}
	return "", "", false
}

func (s *authStore) uniqueUserIDLocked(name string) string {
	base := slugify(name)
	if base == "" {
		base = "user"
	}
	id := base
	for i := 2; s.userIndexLocked(id) >= 0; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}
	return id
}

func (s *authStore) createUser(name string) (User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return User{}, fmt.Errorf("пустое имя пользователя")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := User{
		ID:        s.uniqueUserIDLocked(name),
		Name:      name,
		Enabled:   true,
		CreatedAt: nowStamp(),
	}
	s.users = append(s.users, u)
	s.saveLocked()
	return u, nil
}

func (s *authStore) setUserEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	s.users[i].Enabled = enabled
	s.saveLocked()
	return nil
}

func (s *authStore) deleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	s.users = append(s.users[:i], s.users[i+1:]...)
	s.saveLocked()
	return nil
}

// issueToken создаёт новый токен устройства и ВОЗВРАЩАЕТ его открытый текст (один раз!).
func (s *authStore) issueToken(userID, deviceName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 {
		return "", fmt.Errorf("пользователь не найден")
	}
	deviceName = strings.TrimSpace(deviceName)
	if deviceName == "" {
		deviceName = "Устройство"
	}
	tok := genToken()
	s.users[i].Tokens = append(s.users[i].Tokens, DeviceToken{
		ID:        genID(),
		Name:      deviceName,
		Hash:      hashToken(tok),
		Enabled:   true,
		CreatedAt: nowStamp(),
	})
	s.saveLocked()
	return tok, nil
}

func (s *authStore) revokeToken(userID, tokenID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	ts := s.users[i].Tokens
	j := -1
	for k := range ts {
		if ts[k].ID == tokenID {
			j = k
			break
		}
	}
	if j < 0 {
		return fmt.Errorf("токен не найден")
	}
	s.users[i].Tokens = append(ts[:j], ts[j+1:]...)
	s.saveLocked()
	return nil
}

func (s *authStore) list() []User {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]User, len(s.users))
	copy(out, s.users)
	return out
}

func (s *authStore) get(id string) (User, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(id)
	if i < 0 {
		return User{}, false
	}
	return s.users[i], true
}

// ---------- HTTP: разбор и проверка токена ----------

type ctxKey string

const userCtxKey ctxKey = "poscloudUserID"

func contextWithUser(r *http.Request, uid string) context.Context {
	return context.WithValue(r.Context(), userCtxKey, uid)
}

// userIDFromCtx возвращает id пользователя из контекста запроса ("" если нет).
func userIDFromCtx(r *http.Request) string {
	if v, ok := r.Context().Value(userCtxKey).(string); ok {
		return v
	}
	return ""
}

func tokenFromRequest(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(h[len("Bearer "):])
	}
	return ""
}

// withAuth пропускает /health без проверки, остальные запросы требуют валидный Bearer-токен.
func (a *App) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		tok := tokenFromRequest(r)
		if tok == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "требуется токен (Authorization: Bearer ...)"})
			return
		}
		uid, _, ok := a.auth.findUserByToken(tok)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "неверный или отозванный токен"})
			return
		}
		next.ServeHTTP(w, r.WithContext(contextWithUser(r, uid)))
	})
}
