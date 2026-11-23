package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"poscloud-client-android/internal/client"
)

// pickFolder показывает собственный браузер каталогов. Системный диалог
// (dialog.NewFolderOpen) на Android отдаёт content://-URI из SAF: движок
// синхронизации работает с обычными путями, и разбор такого URI в Fyne
// роняет приложение. Поэтому ходим по каталогам сами — обычным os.ReadDir.
func (s *State) pickFolder(ctrl *client.Controller) {
	start := cleanPath(s.dirValue)
	if !isDir(start) {
		start = ctrl.DefaultSyncDir()
	}
	cur := start

	pathLabel := widget.NewLabel(start)
	pathLabel.Wrapping = fyne.TextWrapBreak

	list := container.NewVBox()
	var load func(string)
	load = func(dir string) {
		cur = dir
		pathLabel.SetText(dir)
		list.Objects = nil

		if parent := filepath.Dir(dir); parent != dir {
			list.Add(widget.NewButtonWithIcon("На уровень выше", theme.NavigateBackIcon(), func() {
				load(parent)
			}))
		}

		names, err := subdirs(dir)
		if err != nil {
			list.Add(caption("Нет доступа: " + err.Error()))
			list.Refresh()
			return
		}
		if len(names) == 0 {
			list.Add(caption("Подкаталогов нет"))
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			list.Add(widget.NewButtonWithIcon(name, theme.FolderIcon(), func() {
				load(path)
			}))
		}
		list.Refresh()
	}

	// Быстрые переходы: приватный каталог, внешний каталог приложения и
	// корень общей памяти (последний доступен не на всех прошивках).
	quick := container.NewHBox(
		widget.NewButton("Приложение", func() { load(ctrl.DefaultSyncDir()) }),
		widget.NewButton("На карте", func() {
			dir := ctrl.ExternalSyncDir()
			if dir == "" {
				dialog.ShowInformation("Папка на карте",
					"Android не дал доступ к общей памяти. Оставьте папку приложения.", s.win)
				return
			}
			load(dir)
		}),
		widget.NewButton("Память", func() { load(sharedRoot()) }),
	)

	content := container.NewBorder(
		container.NewVBox(pathLabel, quick, widget.NewSeparator()),
		nil, nil, nil,
		container.NewVScroll(list),
	)

	d := dialog.NewCustom("Папка синхронизации", "Отмена", content, s.win)
	d.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Отмена", func() { d.Hide() }),
		widget.NewButtonWithIcon("Выбрать эту папку", theme.ConfirmIcon(), func() {
			s.setDir(cur)
			d.Hide()
		}),
	})
	d.Resize(fyne.NewSize(420, 560))
	load(start)
	d.Show()
}

// subdirs возвращает имена подкаталогов без скрытых, по алфавиту.
func subdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// isDir сообщает, что путь существует и является каталогом.
func isDir(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// sharedRoot — корень общей памяти: на Android /storage/emulated/0,
// на остальных системах — домашний каталог.
func sharedRoot() string {
	candidates := []string{strings.TrimSpace(os.Getenv("EXTERNAL_STORAGE")), "/storage/emulated/0", "/sdcard"}
	for _, dir := range candidates {
		if isDir(dir) {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil && isDir(home) {
		return home
	}
	return "/"
}
