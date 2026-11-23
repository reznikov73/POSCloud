package ui

import (
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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
	browser := newFolderBrowser(ctrl, start, s.win)

	d := dialog.NewCustom("Папка синхронизации", "Отмена", browser.object(), s.win)
	d.SetButtons([]fyne.CanvasObject{
		widget.NewButton("Отмена", func() { d.Hide() }),
		widget.NewButtonWithIcon("Выбрать эту папку", theme.ConfirmIcon(), func() {
			s.setDir(browser.current())
			d.Hide()
		}),
	})
	d.Resize(fyne.NewSize(420, 560))
	d.Show()
}

// folderBrowser — состояние браузера каталогов: текущий путь и список.
type folderBrowser struct {
	ctrl *client.Controller
	win  fyne.Window
	cur  string
	path *widget.Label
	list *fyne.Container
}

func newFolderBrowser(ctrl *client.Controller, start string, win fyne.Window) *folderBrowser {
	b := &folderBrowser{ctrl: ctrl, win: win}
	b.path = widget.NewLabel(start)
	b.path.Wrapping = fyne.TextWrapBreak
	b.list = container.NewVBox()
	b.load(start)
	return b
}

// current — выбранный сейчас каталог.
func (b *folderBrowser) current() string { return b.cur }

// object собирает браузер: путь сверху, быстрые переходы, список каталогов.
func (b *folderBrowser) object() fyne.CanvasObject {
	// Быстрые переходы: приватная папка, папка на карте, корень памяти и
	// типовые фото-папки телефона (галерея и снимки экрана).
	quick := container.NewVBox(
		container.NewHBox(
			dialogRow("Приложение", theme.HomeIcon(), func() { b.load(b.ctrl.DefaultSyncDir()) }),
			dialogRow("На карте", theme.StorageIcon(), func() {
				dir := b.ctrl.ExternalSyncDir()
				if dir == "" {
					dialog.ShowInformation("Папка на карте",
						"Android не дал доступ к общей памяти. Оставьте папку приложения.", b.win)
					return
				}
				b.load(dir)
			}),
			dialogRow("Память", theme.FolderOpenIcon(), func() { b.load(sharedRoot()) }),
		),
		container.NewHBox(
			dialogRow("Фото", theme.MediaPhotoIcon(), func() { b.load("/storage/emulated/0/DCIM/Camera") }),
			dialogRow("Снимки", theme.MediaPhotoIcon(), func() { b.load("/storage/emulated/0/Pictures/Screenshots") }),
			dialogRow("POSCloud", theme.FolderIcon(), func() { b.load("/storage/emulated/0/DCIM/POScloud") }),
		),
	)
	return container.NewBorder(
		container.NewVBox(b.path, quick, widget.NewSeparator()),
		nil, nil, nil,
		container.NewVScroll(b.list),
	)
}

// load перестраивает список каталогов.
func (b *folderBrowser) load(dir string) {
	b.cur = dir
	b.path.SetText(dir)
	b.list.Objects = nil

	if parent := filepath.Dir(dir); parent != dir {
		b.list.Add(dialogRow("На уровень выше", theme.NavigateBackIcon(), func() {
			b.load(parent)
		}))
	}

	names, err := subdirs(dir)
	if err != nil {
		b.list.Add(caption("Нет доступа: " + err.Error()))
		b.list.Refresh()
		return
	}
	if len(names) == 0 {
		b.list.Add(caption("Подкаталогов нет"))
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		b.list.Add(dialogRow(name, theme.FolderIcon(), func() {
			b.load(path)
		}))
	}
	b.list.Refresh()
}

// dialogRow — строка списка каталогов: высокая (палец попадает), текст слева,
// иконка в начале. Обычная кнопка Fyne центрирует текст и мельче.
func dialogRow(text string, icon fyne.Resource, tapped func()) fyne.CanvasObject {
	btn := widget.NewButtonWithIcon(text, icon, tapped)
	btn.Alignment = widget.ButtonAlignLeading
	bg := canvas.NewRectangle(color.Transparent)
	bg.SetMinSize(fyne.NewSize(0, 48))
	return container.NewStack(bg, btn)
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
