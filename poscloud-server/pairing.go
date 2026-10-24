package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const pairingTTL = 10 * time.Minute

type pairingEntry struct {
	UserID  string
	Expires time.Time
}

type pairingInfo struct {
	Code     string
	UserID   string
	UserName string
	Expires  time.Time
}

// issuePairing создаёт одноразовый код для пользователя. В памяти хранится
// только SHA-256-хэш; сам код возвращается один раз и больше не логируется.
func (s *authStore) issuePairing(userID string, ttl time.Duration) (pairingInfo, error) {
	if ttl <= 0 {
		ttl = pairingTTL
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.userIndexLocked(userID)
	if i < 0 {
		return pairingInfo{}, fmt.Errorf("пользователь не найден")
	}
	if !s.users[i].Enabled {
		return pairingInfo{}, fmt.Errorf("пользователь отключён")
	}
	if s.pairings == nil {
		s.pairings = map[string]pairingEntry{}
	}
	now := time.Now()
	s.cleanupPairingsLocked(now)
	if len(s.pairings) >= 64 {
		s.dropOldestPairingLocked()
	}
	buf := make([]byte, 18)
	if _, err := rand.Read(buf); err != nil {
		return pairingInfo{}, fmt.Errorf("не удалось создать код: %w", err)
	}
	code := "pcp_" + base64.RawURLEncoding.EncodeToString(buf)
	expires := now.Add(ttl)
	s.pairings[hashToken(code)] = pairingEntry{UserID: userID, Expires: expires}
	return pairingInfo{Code: code, UserID: userID, UserName: s.users[i].Name, Expires: expires}, nil
}

// redeemPairing атомарно проверяет и погашает одноразовый код, после чего
// выпускает обычный токен устройства. Код удаляется только после успешной
// выдачи токена; при ошибке сохранения реестра его можно попробовать снова.
func (s *authStore) redeemPairing(code, deviceName string) (string, User, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return "", User{}, fmt.Errorf("пустой код подключения")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	h := hashToken(code)
	e, ok := s.pairings[h]
	if !ok {
		return "", User{}, fmt.Errorf("код подключения недействителен")
	}
	if time.Now().After(e.Expires) {
		delete(s.pairings, h)
		return "", User{}, fmt.Errorf("код подключения истёк")
	}
	i := s.userIndexLocked(e.UserID)
	if i < 0 || !s.users[i].Enabled {
		delete(s.pairings, h)
		return "", User{}, fmt.Errorf("пользователь недоступен")
	}
	tok, err := s.issueTokenLocked(e.UserID, deviceName)
	if err != nil {
		return "", User{}, err
	}
	delete(s.pairings, h)
	return tok, s.users[i], nil
}

func (s *authStore) cleanupPairingsLocked(now time.Time) {
	for h, e := range s.pairings {
		if now.After(e.Expires) {
			delete(s.pairings, h)
		}
	}
}

func (s *authStore) dropOldestPairingLocked() {
	var oldest string
	var expiry time.Time
	for h, e := range s.pairings {
		if oldest == "" || e.Expires.Before(expiry) {
			oldest, expiry = h, e.Expires
		}
	}
	if oldest != "" {
		delete(s.pairings, oldest)
	}
}

// ConnectionCode — DTO кода подключения для интерфейса сервера.
type ConnectionCode struct {
	Code      string `json:"code"`
	UserID    string `json:"userID"`
	UserName  string `json:"userName"`
	ExpiresAt string `json:"expiresAt"`
	ExpiresIn int    `json:"expiresIn"`
	Error     string `json:"error,omitempty"`
}

// CreateConnectionCode создаёт код подключения для выбранного пользователя.
// Код содержит адрес, порт, TLS-отпечаток и одноразовый секрет, но не токен.
func (a *App) CreateConnectionCode(userID string) ConnectionCode {
	st := a.GetServerState()
	if !st.Running {
		return ConnectionCode{Error: "сначала запустите сервер"}
	}
	if st.TLS && strings.TrimSpace(st.Fingerprint) == "" {
		return ConnectionCode{Error: "не удалось получить отпечаток TLS-сертификата"}
	}
	info, err := a.auth.issuePairing(userID, pairingTTL)
	if err != nil {
		return ConnectionCode{Error: err.Error()}
	}
	host := firstIPOrLocalhost()
	q := url.Values{}
	q.Set("v", "1")
	q.Set("host", host)
	q.Set("port", fmt.Sprintf("%d", st.Port))
	if st.TLS {
		q.Set("tls", "1")
		q.Set("fp", st.Fingerprint)
	} else {
		q.Set("tls", "0")
	}
	q.Set("code", info.Code)
	u := url.URL{Scheme: "poscloud", Host: "connect", RawQuery: q.Encode()}
	a.log("создан код подключения для пользователя " + info.UserName)
	return ConnectionCode{
		Code:      u.String(),
		UserID:    info.UserID,
		UserName:  info.UserName,
		ExpiresAt: info.Expires.Format("2006-01-02 15:04:05"),
		ExpiresIn: int(pairingTTL.Seconds()),
	}
}

type pairRequest struct {
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.Trim(r.RemoteAddr, "[]")
}

type pairLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newPairLimiter(limit int, window time.Duration) *pairLimiter {
	return &pairLimiter{limit: limit, window: window, hits: map[string][]time.Time{}}
}

func (l *pairLimiter) allow(key string) bool {
	now := time.Now()
	cut := now.Add(-l.window)
	l.mu.Lock()
	defer l.mu.Unlock()
	hits := l.hits[key]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

var pairAttempts = newPairLimiter(10, time.Minute)

// handlePair — единственный неавторизованный (кроме /health) endpoint сервера.
// Доступ к нему защищён одноразовым кодом с коротким сроком жизни.
func (a *App) handlePair(w http.ResponseWriter, r *http.Request) {
	if !pairAttempts.allow(remoteIP(r)) {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "слишком много попыток подключения, повторите позже"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var req pairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "неверный запрос"})
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "не указан код подключения"})
		return
	}
	tok, user, err := a.auth.redeemPairing(code, req.DeviceName)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "код подключения недействителен или истёк"})
		return
	}
	a.log(fmt.Sprintf("устройство %q подключено к пользователю %q", strings.TrimSpace(req.DeviceName), user.Name))
	writeJSON(w, http.StatusOK, map[string]any{
		"token":      tok,
		"userID":     user.ID,
		"userName":   user.Name,
		"deviceName": strings.TrimSpace(req.DeviceName),
	})
}
