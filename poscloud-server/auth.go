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
	TokenEnc  string `json:"tokenEnc,omitempty"` // токен, зашифрованный DPAPI (для показа в UI)
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
	UsedBytes  int64  `json:"usedBytes"`
}

// TokenInfo — DTO токена для интерфейса (без хэша).
type TokenInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	LastSeen  string `json:"lastSeen"`
	Token     string `json:"token,omitempty"`
	Available bool   `json:"available"` // false для старых токенов: хранится только хэш
}

type authStore struct {
	mu    sync.Mutex
	path  string
	users []User

	// Одноразовые коды подключения. В памяти хранятся только SHA-256-хэши:
	// после перезапуска сервера неиспользованные коды становятся недействительны.
	pairings map[string]pairingEntry

	logf    func(string)  // журнал приложения (необязательный)
	pending []string      // сообщения, накопленные до подключения журнала
	dirty   bool          // есть несохранённые изменения (отметки LastSeen)
	stopF   chan struct{} // остановка фонового сброса
}

func newAuthStore(path string) *authStore {
	s := &authStore{path: path, pairings: map[string]pairingEntry{}}
	s.load()
	return s
}

// setLogger подключает журнал приложения: сообщения о сохранении и
// восстановлении реестра попадают в общий журнал сервера.
func (s *authStore) setLogger(fn func(string)) {
	s.mu.Lock()
	s.logf = fn
	pend := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, m := range pend {
		fn(m)
	}
}

func (s *authStore) log(msg string) {
	if s.logf != nil {
		s.logf(msg)
		return
	}
	s.pending = append(s.pending, msg)
}

// readUsersFile читает файл реестра. Возвращает список пользователей, признак
// успешного разбора и признак того, что файл существует.
func readUsersFile(path string) ([]User, bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	if len(data) == 0 {
		return nil, false, true
	}
	var f usersFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, false, true
	}
	if f.Users == nil {
		f.Users = []User{}
	}
	return f.Users, true, true
}

func (s *authStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	users, ok, exists := readUsersFile(s.path)
	if ok {
		s.users = users
		return
	}
	// Основной файл повреждён или отсутствует — пробуем резервную копию.
	usersBak, okBak, _ := readUsersFile(s.path + ".bak")
	if exists {
		s.preserveCorruptLocked()
	}
	if okBak {
		s.users = usersBak
		if err := s.saveLocked(); err != nil {
			s.log("реестр: восстановлен из users.json.bak, но основной файл перезаписать не удалось: " + err.Error())
		} else {
			s.log("реестр: users.json отсутствовал или был повреждён — восстановлен из users.json.bak")
		}
		return
	}
	if exists {
		s.users = []User{}
		if err := s.saveLocked(); err != nil {
			s.log("реестр: не удалось записать пустой реестр: " + err.Error())
		}
		s.log("реестр: users.json повреждён, резервной копии нет — список пользователей пуст")
	}
}

// preserveCorruptLocked переносит повреждённый users.json в сторону, чтобы его
// не затёрла первая же запись: файл может понадобиться для разбора.
// Вызывать под s.mu.
func (s *authStore) preserveCorruptLocked() {
	dst := s.path + ".corrupt-" + time.Now().Format("20060102-150405")
	if err := os.Rename(s.path, dst); err == nil {
		s.log("реестр: повреждённый файл сохранён как " + filepath.Base(dst))
	}
}

// saveLocked пишет реестр на диск: основную копию — атомарной подменой файла,
// предыдущее исправное состояние — в users.json.bak. Даже при внезапном
// выключении ПК на диске остаётся либо прежняя, либо новая копия целиком:
// «обрезать» или опустошить users.json запись не может.
// Вызывать ТОЛЬКО под s.mu.
func (s *authStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(usersFile{Users: s.users}, "", "  ")
	if err != nil {
		return err
	}
	var prev []byte
	if old, err := os.ReadFile(s.path); err == nil && len(old) > 0 {
		prev = old
	}
	if err := atomicReplaceFile(s.path, b, 0600); err != nil {
		return err
	}
	if prev != nil && json.Valid(prev) {
		if err := atomicReplaceFile(s.path+".bak", prev, 0600); err != nil {
			s.log("реестр: не удалось обновить резервную копию: " + err.Error())
		}
	}
	s.dirty = false
	return nil
}

// ---------- фоновый сброс отложенных изменений ----------

// authFlushInterval — период фонового сохранения отложенных изменений (LastSeen).
const authFlushInterval = 60 * time.Second

// startFlusher запускает фоновое сохранение отложенных изменений: отметки
// LastSeen копятся в памяти и пишутся на диск не чаще раза в минуту, поэтому
// реестр больше не переписывается на каждый запрос с токеном.
func (s *authStore) startFlusher() {
	s.mu.Lock()
	if s.stopF != nil {
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	s.stopF = stop
	s.mu.Unlock()
	go func() {
		t := time.NewTicker(authFlushInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.flush()
			}
		}
	}()
}

// stopFlusher останавливает фоновый сброс и сохраняет остаток. Вызывать при завершении.
func (s *authStore) stopFlusher() {
	s.mu.Lock()
	stop := s.stopF
	s.stopF = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	s.flush()
}

// flush сохраняет реестр, если есть несохранённые изменения.
func (s *authStore) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return
	}
	if err := s.saveLocked(); err != nil {
		s.log("реестр: не удалось сохранить users.json: " + err.Error())
	}
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
				// Запись откладывается до ближайшего сброса: реестр больше не
				// переписывается на каждый запрос с токеном (иначе файл почти
				// постоянно был «в процессе записи»).
				s.dirty = true
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
	if err := s.saveLocked(); err != nil {
		s.users = s.users[:len(s.users)-1]
		return User{}, fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
	return u, nil
}

func (s *authStore) setUserEnabled(id string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	prev := s.users[i].Enabled
	s.users[i].Enabled = enabled
	if err := s.saveLocked(); err != nil {
		s.users[i].Enabled = prev
		return fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
	return nil
}

func (s *authStore) deleteUser(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	before := append([]User(nil), s.users...)
	s.users = append(s.users[:i], s.users[i+1:]...)
	if err := s.saveLocked(); err != nil {
		s.users = before
		return fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
	return nil
}

// issueToken создаёт новый токен устройства и ВОЗВРАЩАЕТ его открытый текст (один раз!).
func (s *authStore) issueToken(userID, deviceName string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issueTokenLocked(userID, deviceName)
}

// normalizeDeviceName приводит имя устройства к безопасному для UI виду.
func normalizeDeviceName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Устройство"
	}
	r := []rune(name)
	if len(r) > 80 {
		name = string(r[:80])
	}
	return name
}

// issueTokenLocked — внутренняя версия issueToken; вызывать под s.mu.
func (s *authStore) issueTokenLocked(userID, deviceName string) (string, error) {
	i := s.userIndexLocked(userID)
	if i < 0 {
		return "", fmt.Errorf("пользователь не найден")
	}
	deviceName = normalizeDeviceName(deviceName)
	tok := genToken()
	enc, err := protectToken(tok)
	if err != nil {
		return "", fmt.Errorf("не удалось защитить токен: %w", err)
	}
	s.users[i].Tokens = append(s.users[i].Tokens, DeviceToken{
		ID:        genID(),
		Name:      deviceName,
		Hash:      hashToken(tok),
		TokenEnc:  enc,
		Enabled:   true,
		CreatedAt: nowStamp(),
	})
	if err := s.saveLocked(); err != nil {
		s.users[i].Tokens = s.users[i].Tokens[:len(s.users[i].Tokens)-1]
		return "", fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
	return tok, nil
}

// reissueToken заменяет секрет существующего устройства: старый токен
// перестаёт работать, новый токен показывается в интерфейсе один раз.
func (s *authStore) reissueToken(userID, tokenID string) (string, error) {
	tok := genToken()
	enc, err := protectToken(tok)
	if err != nil {
		return "", fmt.Errorf("не удалось защитить токен: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 {
		return "", fmt.Errorf("пользователь не найден")
	}
	j := -1
	for k := range s.users[i].Tokens {
		if s.users[i].Tokens[k].ID == tokenID {
			j = k
			break
		}
	}
	if j < 0 {
		return "", fmt.Errorf("токен не найден")
	}
	before := s.users[i].Tokens[j]
	s.users[i].Tokens[j].Hash = hashToken(tok)
	s.users[i].Tokens[j].TokenEnc = enc
	s.users[i].Tokens[j].Enabled = true
	s.users[i].Tokens[j].CreatedAt = nowStamp()
	s.users[i].Tokens[j].LastSeen = ""
	if err := s.saveLocked(); err != nil {
		s.users[i].Tokens[j] = before
		return "", fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
	return tok, nil
}

// renameToken переименовывает устройство, не меняя сам токен.
func (s *authStore) renameToken(userID, tokenID, name string) error {
	name = normalizeDeviceName(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 {
		return fmt.Errorf("пользователь не найден")
	}
	for k := range s.users[i].Tokens {
		if s.users[i].Tokens[k].ID == tokenID {
			before := s.users[i].Tokens[k].Name
			s.users[i].Tokens[k].Name = name
			if err := s.saveLocked(); err != nil {
				s.users[i].Tokens[k].Name = before
				return fmt.Errorf("не удалось сохранить реестр: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("токен не найден")
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
	before := append([]DeviceToken(nil), ts...)
	s.users[i].Tokens = append(ts[:j], ts[j+1:]...)
	if err := s.saveLocked(); err != nil {
		s.users[i].Tokens = before
		return fmt.Errorf("не удалось сохранить реестр: %w", err)
	}
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
		if r.URL.Path == "/health" || r.URL.Path == "/pair" {
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
