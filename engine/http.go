package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestFile — запись о файле, как её отдаёт сервер.
type ManifestFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MTime   int64  `json:"mtime"`
	Hash    string `json:"hash"`
	Version int64  `json:"version"`
	Deleted bool   `json:"deleted"`
	Dir     bool   `json:"dir,omitempty"`
}

// ProgressSink получает события о ходе передачи файлов (для индикации в интерфейсе).
type ProgressSink interface {
	Begin(name, dir string, total int64)
	Add(name string, n int64)
	End(name string)
}

type nopProgress struct{}

func (nopProgress) Begin(string, string, int64) {}
func (nopProgress) Add(string, int64)           {}
func (nopProgress) End(string)                  {}

// Progress — приёмник событий о ходе передачи; приложение может заменить его при старте.
var Progress ProgressSink = nopProgress{}

// countingReader считает прочитанные байты (для прогресса передачи).
type countingReader struct {
	r  io.Reader
	fn func(int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.fn != nil {
		c.fn(int64(n))
	}
	return n, err
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

// ErrUnauthorized — сервер отверг токен (401). Отдельная ошибка нужна,
// чтобы клиент не путал это с потерей сети.
var ErrUnauthorized = errors.New("неверный или отозванный токен")

// AuthHeader добавляет заголовок авторизации к запросу.
func AuthHeader(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func urlQ(s string) string {
	r := strings.NewReplacer(" ", "%20", "+", "%2B", "&", "%26", "#", "%23", "?", "%3F")
	return r.Replace(s)
}

// FetchChanges запрашивает изменения на сервере после версии since.
func FetchChanges(base, token string, since int64) (int64, []ManifestFile, error) {
	url := strings.TrimRight(base, "/") + "/changes?since=" + fmt.Sprintf("%d", since)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, nil, err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var payload struct {
		Version int64          `json:"version"`
		Changes []ManifestFile `json:"changes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, nil, err
	}
	return payload.Version, payload.Changes, nil
}

// FetchManifest запрашивает полный список файлов пользователя (без содержимого).
func FetchManifest(base, token string) (int64, []ManifestFile, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/manifest", nil)
	if err != nil {
		return 0, nil, err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var payload struct {
		Version int64          `json:"version"`
		Files   []ManifestFile `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, nil, err
	}
	return payload.Version, payload.Files, nil
}

// UploadFile выгружает файл на сервер (крупные — блоками), возвращает
// хэш, версию и время изменения с сервера.
func UploadFile(base, dir, name, token string) (string, int64, int64, error) {
	local := LocalPath(dir, name)
	if fi, err := os.Stat(local); err == nil && fi.Size() >= blockSize {
		if h, v, mt, err := uploadFileBlockwise(base, dir, name, token); err == nil {
			return h, v, mt, nil
		}
	}
	f, err := os.Open(local)
	if err != nil {
		return "", 0, 0, err
	}
	defer f.Close()
	var clen int64
	if fi, err := f.Stat(); err == nil {
		clen = fi.Size()
	}
	Progress.Begin(name, "up", clen)
	defer Progress.End(name)
	req, err := http.NewRequest("POST", strings.TrimRight(base, "/")+"/upload?name="+urlQ(name), &countingReader{r: f, fn: func(n int64) { Progress.Add(name, n) }})
	if err != nil {
		return "", 0, 0, err
	}
	if clen > 0 {
		req.ContentLength = clen
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, 0, fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		MTime   int64  `json:"mtime"`
		Hash    string `json:"hash"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, 0, err
	}
	if r.MTime > 0 {
		t := time.Unix(r.MTime, 0)
		_ = os.Chtimes(local, t, t)
	}
	return r.Hash, r.Version, r.MTime, nil
}

// DownloadFile скачивает файл с сервера (крупные — блоками) и проставляет время.
func DownloadFile(base, dir, name string, mtime int64, token string) error {
	if blocks, size, err := fetchBlockList(base, name, token); err == nil && len(blocks) > 0 && size >= blockSize {
		if err := downloadFileBlockwise(base, dir, name, mtime, token, blocks, size); err == nil {
			return nil
		}
	}
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/download?name="+urlQ(name), nil)
	if err != nil {
		return err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("статус %d", resp.StatusCode)
	}
	_ = os.MkdirAll(dir, 0755)
	dst := LocalPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmpf, err := os.CreateTemp(filepath.Dir(dst), ".poscloud-dl-*")
	if err != nil {
		return err
	}
	tmp := tmpf.Name()
	Progress.Begin(name, "down", resp.ContentLength)
	defer Progress.End(name)
	if _, err := io.Copy(tmpf, &countingReader{r: resp.Body, fn: func(n int64) { Progress.Add(name, n) }}); err != nil {
		tmpf.Close()
		os.Remove(tmp)
		return err
	}
	if err := tmpf.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	if mtime > 0 {
		t := time.Unix(mtime, 0)
		_ = os.Chtimes(dst, t, t)
	}
	return nil
}

// DeleteRemote помечает файл удалённым на сервере.
func DeleteRemote(base, name, token string) error {
	req, err := http.NewRequest("DELETE", strings.TrimRight(base, "/")+"/delete?name="+urlQ(name), nil)
	if err != nil {
		return err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("статус %d", resp.StatusCode)
	}
	return nil
}

// MkdirRemote регистрирует папку (пустой каталог) на сервере, возвращает её версию.
func MkdirRemote(base, name, token string) (int64, error) {
	req, err := http.NewRequest("POST", strings.TrimRight(base, "/")+"/mkdir?name="+urlQ(name), nil)
	if err != nil {
		return 0, err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var payload struct {
		Version int64 `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, err
	}
	return payload.Version, nil
}
