package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	engine "poscloud-engine"

	"golang.org/x/sys/windows/registry"
)

// liteConfig — настройки POSCloud Lite: движковая часть (совместима с
// client.json обычного клиента) плюс признак автозапуска при входе в Windows.
type liteConfig struct {
	engine.Config
	Autostart bool `json:"liteAutostart,omitempty"`
}

// exeDir — каталог программы.
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// configPath — путь к файлу настроек рядом с программой. Если рядом лежит
// client.json (перенесённый с другого ПК) — используем его.
func configPath() string {
	dir := exeDir()
	p := filepath.Join(dir, "client.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(dir, "poscloud-lite.json")
}

// loadConfig читает настройки; отсутствующий файл — настройки по умолчанию.
func loadConfig() liteConfig {
	cfg := liteConfig{}
	cfg.Host = "localhost"
	cfg.Port = 8099
	if home, err := os.UserHomeDir(); err == nil {
		cfg.Dir = filepath.Join(home, "POSCloud", "sync")
	}
	cfg.Interval = 3
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Interval <= 0 {
		cfg.Interval = 3
	}
	if cfg.Port <= 0 {
		cfg.Port = 8099
	}
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	return cfg
}

// saveConfig записывает настройки.
func saveConfig(cfg liteConfig) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configPath(), b, 0644)
}

// configText — текущие настройки как JSON (для окна настроек).
func configText(cfg liteConfig) string {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return string(b)
}

// setAutostart включает/выключает автозапуск при входе в Windows.
func setAutostart(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if on {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		return k.SetStringValue("POSCloud Lite", `"`+exe+`"`)
	}
	err = k.DeleteValue("POSCloud Lite")
	if err == registry.ErrNotExist {
		return nil
	}
	return err
}

// autostartEnabled сообщает, прописан ли автозапуск.
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue("POSCloud Lite")
	return err == nil
}
