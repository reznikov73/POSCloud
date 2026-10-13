package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// blockSize — размер блока для дельта-передачи (4 МиБ).
const blockSize = 4 << 20

// --- content-defined chunking (CDC) ---
const (
	chunkMin  = 1 << 20
	chunkAvg  = 4 << 20
	chunkMax  = 16 << 20
	chunkMask = chunkAvg - 1
)

// gear — таблица скользящего хэша (детерминированная, splitmix64).
var gear [256]uint64

func init() {
	var x uint64 = 0x9E3779B97F4A7C15
	for i := 0; i < 256; i++ {
		x += 0x9E3779B97F4A7C15
		z := x
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z = z ^ (z >> 31)
		gear[i] = z
	}
}

// chunkRef — границы и хэш одного чанка.
type chunkRef struct {
	Offset int64
	Size   int
	Hash   string
}

// splitChunks режет файл на чанки переменного размера; границы определяются содержимым,
// поэтому вставка/сдвиг в начале файла не сдвигает все последующие чанки.
func splitChunks(path string) ([]chunkRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []chunkRef
	var offset int64
	rbuf := make([]byte, 256*1024)
	cbuf := make([]byte, chunkMax)
	clen := 0
	var h uint64
	cut := func() {
		sum := sha256.Sum256(cbuf[:clen])
		out = append(out, chunkRef{Offset: offset, Size: clen, Hash: hex.EncodeToString(sum[:])})
		offset += int64(clen)
		clen = 0
		h = 0
	}
	for {
		n, rerr := f.Read(rbuf)
		for i := 0; i < n; i++ {
			b := rbuf[i]
			cbuf[clen] = b
			clen++
			h = (h << 1) + gear[b]
			if clen >= chunkMin && (h&chunkMask) == 0 {
				cut()
				continue
			}
			if clen >= chunkMax {
				cut()
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	if clen > 0 {
		cut()
	}
	return out, nil
}

// readChunk читает чанк (offset, size) из файла.
func readChunk(path string, off int64, size int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

// fetchBlockList запрашивает у сервера список блоков файла и его размер.
func fetchBlockList(base, name, token string) ([]string, int64, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/blocks?name="+urlQ(name), nil)
	if err != nil {
		return nil, 0, err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("статус %d", resp.StatusCode)
	}
	var p struct {
		Version int64    `json:"version"`
		Size    int64    `json:"size"`
		Blocks  []string `json:"blocks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, 0, err
	}
	return p.Blocks, p.Size, nil
}

// putBlock отправляет один блок на сервер.
func putBlock(base, name, hash string, data []byte, token string) error {
	req, err := http.NewRequest("PUT", strings.TrimRight(base, "/")+"/block?hash="+hash+"&name="+urlQ(name), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// getBlock скачивает один блок с сервера.
func getBlock(base, name, hash, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(base, "/")+"/block?hash="+hash+"&name="+urlQ(name), nil)
	if err != nil {
		return nil, err
	}
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("статус %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// commitBlocks фиксирует новый список блоков файла на сервере.
// Если сервер вернул список отсутствующих блоков — возвращает их в missing при err == nil.
func commitBlocks(base, name string, size, mtime int64, blocks []string, token string) (string, int64, int64, []string, error) {
	body, _ := json.Marshal(map[string]any{"size": size, "mtime": mtime, "blocks": blocks})
	req, err := http.NewRequest("POST", strings.TrimRight(base, "/")+"/commit?name="+urlQ(name), bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	AuthHeader(req, token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest {
		var e struct {
			Error   string   `json:"error"`
			Missing []string `json:"missing"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "missing blocks" {
			return "", 0, 0, e.Missing, nil
		}
		return "", 0, 0, nil, fmt.Errorf("статус 400: %s", e.Error)
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", 0, 0, nil, fmt.Errorf("статус %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Name    string `json:"name"`
		Size    int64  `json:"size"`
		MTime   int64  `json:"mtime"`
		Hash    string `json:"hash"`
		Version int64  `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", 0, 0, nil, err
	}
	return r.Hash, r.Version, r.MTime, nil, nil
}

// sendMissingChunks догружает отсутствующие на сервере чанки.
func sendMissingChunks(base, name, local string, chunks []chunkRef, have map[string]bool, token string) error {
	for _, c := range chunks {
		if have[c.Hash] {
			continue
		}
		data, err := readChunk(local, c.Offset, c.Size)
		if err != nil {
			return err
		}
		if err := putBlock(base, name, c.Hash, data, token); err != nil {
			return err
		}
		Progress.Add(name, int64(len(data)))
		have[c.Hash] = true
	}
	return nil
}

// uploadFileBlockwise загружает файл чанками (content-defined); при неудаче вызывающий откатится к целому пути.
func uploadFileBlockwise(base, dir, name, token string) (string, int64, int64, error) {
	local := LocalPath(dir, name)
	fi, err := os.Stat(local)
	if err != nil {
		return "", 0, 0, err
	}
	chunks, err := splitChunks(local)
	if err != nil {
		return "", 0, 0, err
	}
	if len(chunks) == 0 {
		return "", 0, 0, fmt.Errorf("нет чанков")
	}
	hashes := make([]string, 0, len(chunks))
	for _, c := range chunks {
		hashes = append(hashes, c.Hash)
	}
	srv, _, err := fetchBlockList(base, name, token)
	if err != nil {
		return "", 0, 0, err
	}
	have := map[string]bool{}
	for _, h := range srv {
		have[h] = true
	}
	var total int64
	for _, c := range chunks {
		if !have[c.Hash] {
			total += int64(c.Size)
		}
	}
	Progress.Begin(name, "up", total)
	defer Progress.End(name)
	if err := sendMissingChunks(base, name, local, chunks, have, token); err != nil {
		return "", 0, 0, err
	}
	hash, ver, mt, missing, err := commitBlocks(base, name, fi.Size(), fi.ModTime().Unix(), hashes, token)
	if err != nil {
		return "", 0, 0, err
	}
	if len(missing) > 0 {
		want := map[string]bool{}
		for _, h := range missing {
			want[h] = true
		}
		miss := make([]chunkRef, 0, len(missing))
		for _, c := range chunks {
			if want[c.Hash] {
				miss = append(miss, c)
			}
		}
		if err := sendMissingChunks(base, name, local, miss, have, token); err != nil {
			return "", 0, 0, err
		}
		hash, ver, mt, missing, err = commitBlocks(base, name, fi.Size(), fi.ModTime().Unix(), hashes, token)
		if err != nil {
			return "", 0, 0, err
		}
		if len(missing) > 0 {
			return "", 0, 0, fmt.Errorf("сервер не принял чанки")
		}
	}
	return hash, ver, mt, nil
}

// downloadFileBlockwise собирает файл из чанков: неизменённые берёт из текущей
// локальной версии файла, отсутствующие докачивает с сервера.
func downloadFileBlockwise(base, dir, name string, mtime int64, token string, blocks []string, size int64) error {
	dst := LocalPath(dir, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	byHash := map[string]chunkRef{}
	var oldF *os.File
	if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
		if lay, err := splitChunks(dst); err == nil {
			for _, c := range lay {
				byHash[c.Hash] = c
			}
			if f, err := os.Open(dst); err == nil {
				oldF = f
			}
		}
	}
	var toFetch int64
	for _, h := range blocks {
		if _, ok := byHash[h]; !ok {
			toFetch++
		}
	}
	Progress.Begin(name, "down", toFetch)
	defer Progress.End(name)
	tmpf, err := os.CreateTemp(filepath.Dir(dst), ".poscloud-dl-*")
	if err != nil {
		if oldF != nil {
			oldF.Close()
		}
		return err
	}
	tn := tmpf.Name()
	ok := true
	for _, h := range blocks {
		if oldF != nil {
			if c, found := byHash[h]; found {
				buf := make([]byte, c.Size)
				if n, err := oldF.ReadAt(buf, c.Offset); err == nil && n == c.Size {
					if _, err := tmpf.Write(buf); err == nil {
						continue
					}
				}
			}
		}
		data, err := getBlock(base, name, h, token)
		if err != nil {
			ok = false
			break
		}
		if _, err := tmpf.Write(data); err != nil {
			ok = false
			break
		}
		Progress.Add(name, 1)
	}
	if oldF != nil {
		oldF.Close()
	}
	if err := tmpf.Close(); err != nil {
		ok = false
	}
	if !ok {
		os.Remove(tn)
		return fmt.Errorf("не удалось собрать файл из чанков")
	}
	if size > 0 {
		if fi, err := os.Stat(tn); err == nil && fi.Size() != size {
			os.Remove(tn)
			return fmt.Errorf("размер собранного файла не совпал")
		}
	}
	if err := os.Rename(tn, dst); err != nil {
		os.Remove(tn)
		return err
	}
	if mtime > 0 {
		t := time.Unix(mtime, 0)
		_ = os.Chtimes(dst, t, t)
	}
	return nil
}
