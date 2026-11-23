// Package ui — интерфейс POSCloud Client для Android на Fyne: статус,
// подключение (в том числе по QR-коду со снимка), выборочная синхронизация
// папок и журнал. Пакет сознательно не импортирует fyne.io/fyne/v2/app:
// интерфейс собирается и рендерится тестами без Android и OpenGL.
package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	eng "poscloud-engine"

	"poscloud-client-android/internal/client"
)

// режимы выборочной синхронизации: заголовки для интерфейса и коды движка
var modePairs = []struct {
	title string
	mode  string
}{
	{"в обе стороны", eng.FolderBoth},
	{"только отправлять", eng.FolderUp},
	{"только получать", eng.FolderDown},
	{"не синхронизировать", eng.FolderOff},
}

func modeTitle(mode string) string {
	for _, p := range modePairs {
		if p.mode == mode {
			return p.title
		}
	}
	return mode
}

// ---------- элементы интерфейса ----------

// heading возвращает крупный заголовок раздела (markdown-стиль).
func heading(text string) fyne.CanvasObject {
	rt := widget.NewRichTextFromMarkdown("## " + text)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// caption возвращает второстепенную подпись.
func caption(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	return l
}

// hint возвращает курсивную подсказку.
func hint(text string) fyne.CanvasObject {
	rt := widget.NewRichText(&widget.TextSegment{
		Text:  text,
		Style: widget.RichTextStyleInline,
	})
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// statusDot — цветной индикатор состояния.
type statusDot struct {
	dot  *canvas.Circle
	text *widget.Label
}

func newStatusDot() *statusDot {
	d := &statusDot{
		dot:  canvas.NewCircle(colMuted),
		text: widget.NewLabelWithStyle("Синхронизация остановлена", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
	}
	d.text.Wrapping = fyne.TextWrapWord
	d.dot.StrokeWidth = 0
	return d
}

func (s *statusDot) object() fyne.CanvasObject {
	return container.NewHBox(container.NewPadded(s.dot), s.text)
}

func (s *statusDot) set(running, online bool, errMsg string) {
	switch {
	case running && online:
		s.dot.FillColor = colSuccess
		s.text.SetText("Синхронизация работает")
	case running:
		s.dot.FillColor = colWarn
		if errMsg != "" {
			s.text.SetText("Проблема связи: " + errMsg)
		} else {
			s.text.SetText("Синхронизация работает — ждём сервер")
		}
	default:
		s.dot.FillColor = colMuted
		s.text.SetText("Синхронизация остановлена")
	}
	canvas.Refresh(s.dot)
}

// ---------- состояние интерфейса ----------

// State держит виджеты, которые обновляет таймер.
type State struct {
	Content fyne.CanvasObject

	// статус
	dot          *statusDot
	serverValue  *widget.Label
	onlineValue  *widget.Label
	folderValue  *widget.Label
	syncValue    *widget.Label
	countValue   *widget.Label
	errLabel     *widget.Label
	transferText *widget.Label
	transferBar  *widget.ProgressBar

	btnStart *widget.Button
	btnStop  *widget.Button
	btnNow   *widget.Button

	// подключение (поля читает «Запустить»)
	hostEntry     *widget.Entry
	portEntry     *widget.Entry
	tokenEntry    *widget.Entry
	deviceEntry   *widget.Entry
	dirEntry      *widget.Entry
	intervalEntry *widget.Entry
	tlsCheck      *widget.Check
	fpEntry       *widget.Entry
	autoRunCheck  *widget.Check
	pairEntry     *widget.Entry

	// папки
	foldersBox    *fyne.Container
	foldersState  *widget.Label
	freeCheck     *widget.Check
	btnResync     *widget.Button
	btnRefreshFld *widget.Button

	// журнал
	logLabel *widget.Label

	// вкладки
	tabs *container.AppTabs

	win fyne.Window
}

func Build(ctrl *client.Controller, w fyne.Window) *State {
	ui := &State{win: w}
	cfg := ctrl.Config()

	// ===== вкладка «Статус» =====
	ui.dot = newStatusDot()

	ui.serverValue = widget.NewLabel(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
	ui.onlineValue = widget.NewLabel("неизвестно")
	ui.folderValue = widget.NewLabel(cfg.Dir)
	ui.syncValue = widget.NewLabel("—")
	ui.countValue = widget.NewLabel("—")
	for _, l := range []*widget.Label{ui.serverValue, ui.onlineValue, ui.folderValue, ui.syncValue, ui.countValue} {
		l.Wrapping = fyne.TextWrapWord
	}
	// Путь к папке — одна длинная строка без пробелов: переносим её в любом месте,
	// иначе карточка требует ширину больше экрана телефона.
	ui.folderValue.Wrapping = fyne.TextWrapBreak

	serverCard := widget.NewCard("Сервер", "", container.NewVBox(
		fieldRow("Адрес", ui.serverValue),
		fieldRow("Связь", ui.onlineValue),
	))
	syncCard := widget.NewCard("Синхронизация", "", container.NewVBox(
		fieldRow("Папка", ui.folderValue),
		fieldRow("Файлов", ui.countValue),
		fieldRow("Последний цикл", ui.syncValue),
	))

	ui.errLabel = widget.NewLabel("")
	ui.errLabel.Wrapping = fyne.TextWrapWord
	ui.errLabel.Importance = widget.DangerImportance
	ui.errLabel.Hide()

	ui.transferText = caption("Передач нет")
	ui.transferBar = widget.NewProgressBar()
	ui.transferBar.Hide()
	transferCard := widget.NewCard("Передачи", "", container.NewVBox(ui.transferText, ui.transferBar))

	ui.btnStart = widget.NewButtonWithIcon("Запустить", theme.MediaPlayIcon(), func() {
		host := strings.TrimSpace(ui.hostEntry.Text)
		port := atoiDefault(ui.portEntry.Text, cfg.Port)
		interval := atoiDefault(ui.intervalEntry.Text, cfg.Interval)
		if err := ctrl.Start(host, port, cleanPath(ui.dirEntry.Text), interval,
			ui.tokenEntry.Text, ui.tlsCheck.Checked, ui.fpEntry.Text); err != nil {
			dialog.ShowError(err, w)
			return
		}
		ui.refreshStatic(ctrl)
	})
	ui.btnStart.Importance = widget.HighImportance

	ui.btnStop = widget.NewButtonWithIcon("Остановить", theme.MediaStopIcon(), func() {
		ctrl.Stop()
	})
	ui.btnNow = widget.NewButtonWithIcon("Синхронизировать сейчас", theme.ViewRefreshIcon(), func() { ctrl.SyncNow() })

	// Кнопки — сразу под состоянием: на телефоне до них не нужно
	// докручивать карточки, а нижняя граница прокрутки их не срезает.
	actions := container.NewVBox(ui.btnStart, ui.btnStop, ui.btnNow)
	statusSpacer := canvas.NewRectangle(colBg)
	statusSpacer.SetMinSize(fyne.NewSize(0, 24))

	statusCard := container.NewVBox(
		heading("POSCloud"),
		ui.dot.object(),
		actions,
		serverCard,
		syncCard,
		transferCard,
		ui.errLabel,
		container.NewPadded(caption("Синхронизация работает, пока приложение открыто или живо в фоне; для надёжной фоновой работы держите приложение в списке недавних.")),
		statusSpacer,
	)

	// ===== вкладка «Подключение» =====
	ui.pairEntry = widget.NewMultiLineEntry()
	ui.pairEntry.SetPlaceHolder("poscloud://connect?…")
	ui.pairEntry.Wrapping = fyne.TextWrapBreak

	pairBtn := widget.NewButtonWithIcon("Подключиться по коду", theme.LoginIcon(), func() {
		code := strings.TrimSpace(ui.pairEntry.Text)
		if code == "" {
			dialog.ShowInformation("Код подключения", "Вставьте код, выданный сервером POSCloud (кнопка «Код подключения» в окне сервера), или отсканируйте его снимком.", w)
			return
		}
		ui.pairConnect(ctrl, code)
	})

	// выбор снимка с QR-кодом: сервер показывает QR, телефон снимает его
	// камерой (системной), снимок декодируется прямо в приложении.
	qrBtn := widget.NewButtonWithIcon("Выбрать снимок QR", theme.MediaPhotoIcon(), func() {
		fd := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if r == nil {
				return
			}
			go func(r fyne.URIReadCloser) {
				defer r.Close()
				code, derr := client.DecodeQRReader(r)
				fyne.Do(func() {
					if derr != nil {
						dialog.ShowError(derr, w)
						return
					}
					ui.pairEntry.SetText(code)
					ui.pairConnect(ctrl, code)
				})
			}(r)
		}, w)
		fd.SetFilter(storage.NewMimeTypeFileFilter([]string{"image/*"}))
		fd.Show()
	})
	qrBtn.Importance = widget.HighImportance

	quickCard := widget.NewCard("Быстрое подключение", "", container.NewVBox(
		qrBtn,
		container.NewPadded(caption("На сервере нажмите «Код подключения» — появится QR-код. Сфотографируйте его камерой телефона и выберите снимок здесь: приложение подключится само.")),
		widget.NewSeparator(),
		caption("Или вставьте код текстом:"),
		ui.pairEntry,
		pairBtn,
	))

	ui.hostEntry = widget.NewEntry()
	ui.hostEntry.SetPlaceHolder("192.168.1.10")
	ui.portEntry = widget.NewEntry()
	ui.portEntry.SetPlaceHolder("8090")
	ui.tokenEntry = widget.NewPasswordEntry()
	ui.tokenEntry.SetPlaceHolder("токен устройства")
	ui.deviceEntry = widget.NewEntry()
	ui.deviceEntry.SetPlaceHolder("имя устройства")
	ui.dirEntry = widget.NewMultiLineEntry()
	ui.dirEntry.SetPlaceHolder("папка синхронизации")
	// Путь длинный и без пробелов: в многострочном поле он виден целиком,
	// а не уезжает за край горизонтальной прокруткой.
	ui.dirEntry.Wrapping = fyne.TextWrapBreak
	ui.dirEntry.SetMinRowsVisible(2)
	pickBtn := widget.NewButtonWithIcon("Выбрать папку…", theme.FolderOpenIcon(), func() {
		ui.pickFolder(ctrl)
	})
	appDirBtn := widget.NewButtonWithIcon("Папка приложения", theme.HomeIcon(), func() {
		ui.dirEntry.SetText(ctrl.DefaultSyncDir())
	})
	extDirBtn := widget.NewButtonWithIcon("Папка на карте", theme.StorageIcon(), func() {
		dir := ctrl.ExternalSyncDir()
		if dir == "" {
			dialog.ShowInformation("Папка на карте",
				"Android не дал доступ к общей памяти. Оставьте папку приложения: синхронизация будет работать, но файлы не будут видны в проводнике.", w)
			return
		}
		ui.dirEntry.SetText(dir)
	})
	ui.intervalEntry = widget.NewEntry()
	ui.intervalEntry.SetPlaceHolder("3")
	ui.tlsCheck = widget.NewCheck("TLS (самоподписанный сертификат)", nil)
	ui.fpEntry = widget.NewMultiLineEntry()
	ui.fpEntry.SetPlaceHolder("SHA-256")
	ui.fpEntry.Wrapping = fyne.TextWrapBreak
	ui.fpEntry.SetMinRowsVisible(2)
	ui.fpEntry.Hide()
	ui.tlsCheck.OnChanged = func(on bool) {
		if on {
			ui.fpEntry.Show()
		} else {
			ui.fpEntry.Hide()
		}
	}
	ui.autoRunCheck = widget.NewCheck("Запускать синхронизацию при старте", nil)

	// Форма для узкого экрана: подпись над полем, поля и кнопки — на всю ширину.
	// Классическая форма Fyne держит подписи в отдельной колонке слева и на
	// телефоне оставляет полям слишком мало места.
	form := container.NewVBox(
		formRow("Сервер", ui.hostEntry),
		formRow("Порт", ui.portEntry),
		formRow("Токен", ui.tokenEntry),
		formRow("Имя устройства", ui.deviceEntry),
		formRow("Папка", container.NewVBox(ui.dirEntry, pickBtn, appDirBtn, extDirBtn)),
		formRow("Интервал, с", ui.intervalEntry),
		ui.tlsCheck,
		formRow("Отпечаток", ui.fpEntry),
		ui.autoRunCheck,
	)

	testBtn := widget.NewButtonWithIcon("Проверить связь", theme.ConfirmIcon(), func() {
		d := dialog.NewProgress("Проверка связи", "Обращаемся к серверу…", w)
		d.Show()
		go func() {
			res := ctrl.TestConnection(strings.TrimSpace(ui.hostEntry.Text),
				atoiDefault(ui.portEntry.Text, cfg.Port), ui.tokenEntry.Text,
				ui.tlsCheck.Checked, ui.fpEntry.Text)
			fyne.Do(func() {
				d.Hide()
				title := "Связь установлена"
				if strings.HasPrefix(res, "Ошибка") {
					title = "Нет связи"
				}
				dialog.ShowInformation(title, res, w)
			})
		}()
	})
	saveBtn := widget.NewButtonWithIcon("Сохранить настройки", theme.DocumentSaveIcon(), func() {
		ctrl.SaveSettings(strings.TrimSpace(ui.hostEntry.Text),
			atoiDefault(ui.portEntry.Text, cfg.Port),
			cleanPath(ui.dirEntry.Text),
			atoiDefault(ui.intervalEntry.Text, cfg.Interval),
			ui.tokenEntry.Text, ui.tlsCheck.Checked, ui.fpEntry.Text,
			ui.deviceEntry.Text, ui.autoRunCheck.Checked)
		ui.refreshStatic(ctrl)
		dialog.ShowInformation("Настройки", "Настройки сохранены.", w)
	})

	manualCard := widget.NewCard("Ручная настройка", "", container.NewVBox(
		form,
		container.NewVBox(testBtn, saveBtn),
	))

	connectCard := container.NewVBox(
		heading("Подключение"),
		quickCard,
		manualCard,
		container.NewPadded(caption("Сервер создаёт пользователя и устройство и выдаёт токен. На Android по умолчанию используется папка внутри приложения; внешние пути (например /storage/emulated/0/…) требуют разрешения «Доступ ко всем файлам» и могут не работать.")),
	)
	ui.loadFromConfig(ctrl)

	// ===== вкладка «Папки» =====
	ui.freeCheck = widget.NewCheck("Без правил — синхронизировать всё", func(on bool) {
		go func() {
			if err := ctrl.SetRulesFree(on); err != nil {
				fyne.Do(func() { dialog.ShowError(err, ui.win) })
			}
		}()
	})
	ui.freeCheck.SetChecked(ctrl.RulesFreeEnabled())

	ui.foldersState = caption("")
	ui.foldersBox = container.NewVBox()
	ui.btnRefreshFld = widget.NewButtonWithIcon("Обновить", theme.ViewRefreshIcon(), nil)
	ui.btnResync = widget.NewButtonWithIcon("Полная сверка", theme.SearchIcon(), func() {
		ctrl.Resync()
		dialog.ShowInformation("Полная сверка", "Сервер будет перечитан полностью при следующем цикле.", w)
	})
	ui.btnRefreshFld.OnTapped = func() { refreshFolders(ctrl, ui) }

	foldersCard := container.NewBorder(
		container.NewVBox(
			heading("Папки"),
			widget.NewCard("", "", container.NewVBox(
				ui.freeCheck,
				container.NewVBox(ui.btnRefreshFld, ui.btnResync),
				ui.foldersState,
			)),
		),
		nil, nil, nil,
		container.NewScroll(ui.foldersBox),
	)

	// ===== вкладка «Журнал» =====
	ui.logLabel = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	ui.logLabel.Wrapping = fyne.TextWrapWord
	clearBtn := widget.NewButtonWithIcon("Очистить журнал", theme.DeleteIcon(), func() {
		ctrl.ClearLogs()
		ui.logLabel.SetText("")
	})
	logCard := container.NewBorder(
		container.NewVBox(heading("Журнал"), clearBtn),
		nil, nil, nil,
		container.NewScroll(ui.logLabel),
	)

	// Нижняя панель вкладок в Fyne ниже своего MinSize: иконки с подписями
	// выступают над ней и накрывают содержимое. Оставляем пустую полосу
	// внизу, чтобы кнопки и текст не оказывались под панелью.
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("Статус", theme.HomeIcon(), withBottomInset(container.NewScroll(statusCard))),
		container.NewTabItemWithIcon("Связь", theme.SettingsIcon(), withBottomInset(container.NewScroll(connectCard))),
		container.NewTabItemWithIcon("Папки", theme.FolderOpenIcon(), withBottomInset(foldersCard)),
		container.NewTabItemWithIcon("Журнал", theme.DocumentIcon(), withBottomInset(logCard)),
	)
	tabs.SetTabLocation(container.TabLocationBottom)
	ui.tabs = tabs
	ui.Content = tabs

	ui.refreshStatic(ctrl)
	refreshFolders(ctrl, ui)
	return ui
}

// fieldRow строит пару «подпись — значение» в столбик. Классический Border
// с подписью слева на телефоне сжимает переносимую подпись до нулевой ширины,
// и текст наезжает на значение.
func fieldRow(label string, value *widget.Label) fyne.CanvasObject {
	l := caption(label)
	l.TextStyle = fyne.TextStyle{Italic: true}
	return container.NewVBox(l, value)
}

// formRow строит строку формы для узкого экрана: подпись над полем.
func formRow(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	l := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.Wrapping = fyne.TextWrapWord
	return container.NewVBox(l, obj)
}

// pairConnect обменивает код подключения на токен и сообщает результат.
func (ui *State) pairConnect(ctrl *client.Controller, code string) {
	w := ui.win
	d := dialog.NewProgress("Подключение", "Обмениваем код на токен…", w)
	d.Show()
	go func() {
		res, err := ctrl.Pair(code, ui.deviceEntry.Text)
		fyne.Do(func() {
			d.Hide()
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			ui.loadFromConfig(ctrl)
			ui.refreshStatic(ctrl)
			// у формы с готовым токеном устройства нет имени пользователя
			lines := fmt.Sprintf("Сервер: %s\nУстройство: %s",
				eng.BaseURL(ctrl.Config().Host, ctrl.Config().Port, ctrl.Config().TLS), res.DeviceName)
			if res.UserName != "" {
				lines = fmt.Sprintf("Сервер: %s\nПользователь: %s\nУстройство: %s",
					eng.BaseURL(ctrl.Config().Host, ctrl.Config().Port, ctrl.Config().TLS), res.UserName, res.DeviceName)
			}
			dialog.ShowInformation("Подключено", lines+"\n\nТокен сохранён. Нажмите «Запустить» на вкладке «Статус».", w)
		})
	}()
}

// pickFolder показывает выбор каталога средствами Fyne: начинаем с текущей
// папки синхронизации. Внешние каталоги (включая Android/data) видны, если
// система разрешает их чтение.
func (ui *State) pickFolder(ctrl *client.Controller) {
	start := cleanPath(ui.dirEntry.Text)
	if start == "" {
		start = ctrl.DefaultSyncDir()
	}
	fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, ui.win)
			return
		}
		if uri == nil {
			return
		}
		ui.dirEntry.SetText(strings.TrimPrefix(uri.String(), "file://"))
	}, ui.win)
	if lister, err := storage.ListerForURI(storage.NewFileURI(start)); err == nil {
		fd.SetLocation(lister)
	}
	fd.Resize(fyne.NewSize(420, 620))
	fd.Show()
}

// loadFromConfig заполняет поля подключения из сохранённых настроек.
func (ui *State) loadFromConfig(ctrl *client.Controller) {
	cfg := ctrl.Config()
	ui.hostEntry.SetText(cfg.Host)
	ui.portEntry.SetText(strconv.Itoa(cfg.Port))
	if cfg.Token != "" {
		ui.tokenEntry.SetText(cfg.Token)
	}
	if cfg.DeviceName != "" {
		ui.deviceEntry.SetText(cfg.DeviceName)
	} else {
		ui.deviceEntry.SetText(ctrl.DeviceName())
	}
	ui.dirEntry.SetText(cfg.Dir)
	ui.intervalEntry.SetText(strconv.Itoa(cfg.Interval))
	ui.tlsCheck.SetChecked(cfg.TLS)
	if cfg.TLS {
		ui.fpEntry.SetText(cfg.TLSFingerprint)
	}
	ui.autoRunCheck.SetChecked(ctrl.AutoRunEnabled())
}

// refreshStatic обновляет статические подписи (сервер, папка).
func (ui *State) refreshStatic(ctrl *client.Controller) {
	cfg := ctrl.Config()
	ui.serverValue.SetText(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
	ui.folderValue.SetText(cfg.Dir)
}

// refreshLoop раз в секунду обновляет статус, передачи и журнал.
func Refresh(ctrl *client.Controller, ui *State, stop chan struct{}) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	var lastLog string
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			st := ctrl.Status()
			cfg := ctrl.Config()
			running := ctrl.Running()
			tr := client.Transfers.Snapshot()

			onlineTxt := "неизвестно"
			if running {
				if st.Online {
					onlineTxt = "есть"
				} else if st.ErrMsg != "" {
					onlineTxt = st.ErrMsg
				} else {
					onlineTxt = "ожидание"
				}
			}
			syncTxt := "—"
			if st.LastSync != "" {
				syncTxt = st.LastSync
			}
			countTxt := fmt.Sprintf("%d", st.Count)

			var logTxt string
			if ctrl.LogDirtyAndReset() {
				lines := ctrl.Logs()
				if len(lines) > 300 {
					lines = lines[len(lines)-300:]
				}
				logTxt = strings.Join(lines, "\n")
				lastLog = logTxt
			}

			fyne.Do(func() {
				ui.dot.set(running, st.Online, st.ErrMsg)
				ui.onlineValue.SetText(onlineTxt)
				ui.serverValue.SetText(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
				ui.folderValue.SetText(cfg.Dir)
				ui.syncValue.SetText(syncTxt)
				ui.countValue.SetText(countTxt)
				if st.ErrMsg != "" {
					ui.errLabel.SetText(st.ErrMsg)
					ui.errLabel.Show()
				} else {
					ui.errLabel.Hide()
				}
				if running {
					ui.btnStart.Disable()
					ui.btnStop.Enable()
					ui.btnNow.Enable()
				} else {
					ui.btnStart.Enable()
					ui.btnStop.Disable()
					ui.btnNow.Disable()
				}
				if len(tr) == 0 {
					ui.transferText.SetText("Передач нет")
					ui.transferBar.SetValue(0)
					ui.transferBar.Hide()
				} else {
					top := tr[0]
					arrow := "↑ отправка"
					if top.Dir == "down" {
						arrow = "↓ загрузка"
					}
					extra := ""
					if len(tr) > 1 {
						extra = fmt.Sprintf("  (+ ещё %d)", len(tr)-1)
					}
					ui.transferText.SetText(fmt.Sprintf("%s  %s — %d%%%s", arrow, top.Name, top.Pct, extra))
					ui.transferBar.Show()
					ui.transferBar.SetValue(float64(top.Pct) / 100)
				}
				if logTxt != "" {
					ui.logLabel.SetText(lastLog)
				}
			})
		}
	}
}

// refreshFolders запрашивает у движка список папок первого уровня и строит строки.
func refreshFolders(ctrl *client.Controller, ui *State) {
	ui.btnRefreshFld.Disable()
	ui.foldersState.SetText("Загрузка…")
	go func() {
		nodes, err := ctrl.ServerFolders()
		fyne.Do(func() {
			ui.btnRefreshFld.Enable()
			if err != nil {
				ui.foldersState.SetText("Ошибка: " + err.Error())
				return
			}
			if len(nodes) == 0 {
				ui.foldersState.SetText("Папок пока нет: синхронизируйте что-нибудь или создайте папку на сервере.")
				ui.foldersBox.Objects = nil
				ui.foldersBox.Refresh()
				return
			}
			ui.foldersState.SetText(fmt.Sprintf("Папок: %d", len(nodes)))
			objs := make([]fyne.CanvasObject, 0, len(nodes))
			for _, n := range nodes {
				objs = append(objs, folderRow(ctrl, ui, n))
			}
			ui.foldersBox.Objects = objs
			ui.foldersBox.Refresh()
		})
	}()
}

// folderRow строит карточку папки: имя, статистика и выбор режима.
func folderRow(ctrl *client.Controller, ui *State, n eng.ServerFolderNode) fyne.CanvasObject {
	info := fmt.Sprintf("на сервере: %d файлов (%s)", n.Files, humanBytes(n.Bytes))
	if n.LocFiles > 0 || n.Local {
		info += fmt.Sprintf("\nздесь: %d файлов (%s)", n.LocFiles, humanBytes(n.LocBytes))
	} else {
		info += "\nздесь: нет копии"
	}
	if n.New {
		info += "\nновая папка — выберите режим"
	}
	name := widget.NewLabelWithStyle(n.Path, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.Wrapping = fyne.TextWrapBreak
	details := caption(info)
	details.Wrapping = fyne.TextWrapWord

	titles := make([]string, len(modePairs))
	for i, p := range modePairs {
		titles[i] = p.title
	}
	sel := widget.NewSelect(titles, func(pick string) {
		for _, p := range modePairs {
			if p.title == pick {
				if err := ctrl.SetFolderRule(n.Path, p.mode); err != nil {
					dialog.ShowError(err, ui.win)
				}
				return
			}
		}
	})
	sel.Selected = modeTitle(n.Mode)
	if n.Mode == "" || (!n.New && n.Rule == "" && n.Mode == eng.FolderOff) {
		sel.Selected = modeTitle(eng.FolderOff)
	}

	return widget.NewCard("", "", container.NewVBox(
		name,
		details,
		sel,
	))
}

// ---------- утилиты ----------

// bottomInset — пустая полоса под нижней панелью вкладок: в Fyne панель
// с иконками и подписями выше своего MinSize и выступает над содержимым.
const bottomInset = 24

// withBottomInset добавляет пустую полосу под содержимым вкладки.
func withBottomInset(obj fyne.CanvasObject) fyne.CanvasObject {
	spacer := canvas.NewRectangle(colBg)
	spacer.SetMinSize(fyne.NewSize(0, bottomInset))
	return container.NewBorder(nil, spacer, nil, nil, obj)
}

// cleanPath убирает переводы строк, которые может добавить многострочное поле.
func cleanPath(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.TrimSpace(s)
}

// atoiDefault разбирает целое из поля ввода, при ошибке — значение по умолчанию.
func atoiDefault(s string, def int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// humanBytes форматирует размер файла для подписей.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f ГБ", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f МБ", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f КБ", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d Б", b)
	}
}

// unusedImageImport — image/png и image/jpeg нужны для декодирования снимков.
