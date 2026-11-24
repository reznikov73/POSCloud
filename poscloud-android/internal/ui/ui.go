// Package ui — интерфейс POSCloud Client для Android на Fyne: статус,
// подключение (в том числе по QR-коду со снимка), выборочная синхронизация
// папок и журнал. Пакет сознательно не импортирует fyne.io/fyne/v2/app:
// интерфейс собирается и рендерится тестами без Android и OpenGL.
package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	eng "poscloud-engine"

	"poscloud-client-android/internal/camera"
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

// shortModeTitle — подпись режима для чипа в строке папки: полные названия
// в строку не влезают, а открыть выбор можно касанием.
func shortModeTitle(mode string) string {
	switch mode {
	case eng.FolderBoth:
		return "В обе"
	case eng.FolderUp:
		return "Отправка"
	case eng.FolderDown:
		return "Приём"
	default:
		return "Нет"
	}
}

// ---------- элементы интерфейса ----------

// heading возвращает крупный заголовок раздела (markdown-стиль).
func heading(text string) fyne.CanvasObject {
	rt := widget.NewRichTextFromMarkdown("## " + text)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}

// pageHeader — заголовок страницы с действиями справа (иконки).
func pageHeader(title string, actions ...fyne.CanvasObject) fyne.CanvasObject {
	h := heading(title)
	if len(actions) == 0 {
		return h
	}
	return container.NewBorder(nil, nil, nil, container.NewHBox(actions...), h)
}

// iconButton — кнопка-иконка в шапке страницы.
func iconButton(icon fyne.Resource, tapped func()) *widget.Button {
	b := widget.NewButtonWithIcon("", icon, tapped)
	b.Importance = widget.LowImportance
	return b
}

// caption возвращает второстепенную подпись.
func caption(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Importance = widget.LowImportance
	l.Wrapping = fyne.TextWrapWord
	l.SizeName = theme.SizeNameCaptionText
	return l
}

// setDir запоминает выбранную папку и показывает её в форме.
func (st *State) setDir(dir string) {
	st.dirValue = dir
	st.dirLabel.SetText(dir)
}

// ---------- состояние интерфейса ----------

// State держит виджеты, которые обновляет таймер.
type State struct {
	Content fyne.CanvasObject

	// статус
	stateSub     *widget.Label
	swSync       *pillSwitch
	onToggle     func(bool)
	serverValue  *widget.Label
	onlineValue  *widget.Label
	folderValue  *widget.Label
	countValue   *widget.Label
	upValue      *widget.Label
	downValue    *widget.Label
	errLabel     *widget.Label
	transferText *widget.Label
	transferBar  *widget.ProgressBar
	transferBox  *fyne.Container
	shotBox      *fyne.Container
	shotLabel    *widget.Label
	btnShotPerm  *widget.Button

	btnNow *widget.Button

	// хранилище: занято здесь и на сервере (storage.go)
	storageLocal  *widget.Label
	storageServer *widget.Label
	storageNote   *widget.Label
	btnStorage    *widget.Button
	storageLast   *client.StorageStats
	storageAt     atomic.Int64 // время последнего расчёта (нс)
	storageBusy   atomic.Int32 // 1 — расчёт идёт

	// обновление
	updateInfo *widget.Label
	updateAPK  string
	btnInstall *widget.Button

	// подключение (поля читает «Запустить»)
	hostEntry      *formEntry
	portEntry      *formEntry
	tokenEntry     *formEntry
	deviceEntry    *formEntry
	dirLabel       *widget.Label
	dirValue       string
	intervalEntry  *formEntry
	tlsSwitch      *pillSwitch
	fpEntry        *widget.Entry
	fpBox          fyne.CanvasObject
	autoSwitch     *pillSwitch
	pairEntry      *widget.Entry
	pairBox        fyne.CanvasObject
	pairToggle     *widget.Button
	advancedCard   *widget.Card   // дополнительно: свёрнуто
	advancedToggle *widget.Button // заголовок-кнопка, показывает сервер
	testInfo       *widget.Label  // результат «Проверить связь» строкой
	saveInfo       *widget.Label  // «Настройки сохранены» строкой

	// тихие часы и лимит скачивания
	quietSwitch *pillSwitch
	quietFrom   *formEntry
	quietTo     *formEntry
	quietRow    fyne.CanvasObject
	quietErr    *widget.Label
	limitErr    *widget.Label
	maxDownEntr *formEntry

	// папки
	foldersBox    *fyne.Container
	foldersState  *widget.Label
	freeSwitch    *pillSwitch
	btnRefreshFld *widget.Button

	// журнал
	logLabel     *widget.Label
	logPathLabel *widget.Label
	logPathValue string
	logSaveLabel *widget.Label
	btnSaveLog   *widget.Button

	// файлы
	files *filesPage

	// лента «Недавние»
	recent *recentPage

	// вкладки
	pager *pager

	win fyne.Window
}

func Build(ctrl *client.Controller, w fyne.Window) *State {
	ui := &State{win: w}
	cfg := ctrl.Config()

	// ===== вкладка «Статус» =====
	// Переключатель — главное действие (как Wi-Fi в настройках телефона),
	// цифры — одной полосой, пустые состояния — строками, а не карточками.
	ui.serverValue = widget.NewLabel(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
	ui.serverValue.SizeName = theme.SizeNameText
	ui.onlineValue = widget.NewLabel("—")
	ui.countValue = widget.NewLabel("—")
	ui.upValue = widget.NewLabel("0 Б")
	ui.downValue = widget.NewLabel("0 Б")
	ui.folderValue = widget.NewLabel(cfg.Dir)
	// Путь — одна длинная строка без пробелов: переносим её в любом месте,
	// иначе карточка требует ширину больше экрана телефона.
	ui.folderValue.Wrapping = fyne.TextWrapBreak
	ui.folderValue.SizeName = theme.SizeNameCaptionText
	for _, l := range []*widget.Label{ui.onlineValue, ui.countValue, ui.upValue, ui.downValue} {
		l.TextStyle = fyne.TextStyle{Bold: true}
		l.SizeName = theme.SizeNameSubHeadingText
		l.Alignment = fyne.TextAlignTrailing
	}

	ui.stateSub = caption("остановлена")
	// Подпись статуса не переносится: в строке рядом с переключателем
	// перенос ломал «работает · цикл 21:33:26» на две строки.
	ui.stateSub.Wrapping = fyne.TextWrapOff
	ui.swSync = newPillSwitch(false, nil)
	ui.onToggle = func(on bool) {
		if on {
			ui.startSync(ctrl)
		} else {
			ctrl.Stop()
		}
	}
	ui.swSync.OnChanged = ui.onToggle
	syncTitle := widget.NewLabelWithStyle("Синхронизация", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	syncTitle.SizeName = theme.SizeNameSubHeadingText
	syncCard := cardBox(container.NewBorder(nil, nil,
		container.NewCenter(container.NewVBox(syncTitle, ui.stateSub)),
		container.New(layout.NewGridWrapLayout(fyne.NewSize(toggleW, toggleTouchH)), ui.swSync)))

	ui.btnNow = widget.NewButtonWithIcon("Синхронизировать сейчас", theme.ViewRefreshIcon(), func() { ctrl.SyncNow() })
	ui.btnNow.Disable()

	// Цифры — сеткой 2×2 «подпись слева, значение справа»: четыре колонки
	// на 360 dp не влезали и подписи переносились посередине слова.
	stat := func(label string, v *widget.Label) fyne.CanvasObject {
		l := caption(label)
		return container.NewBorder(nil, nil, l, nil, v)
	}
	statsCard := cardBox(container.NewGridWithColumns(2,
		stat("Связь", ui.onlineValue), stat("Файлов", ui.countValue),
		stat("Отправлено", ui.upValue), stat("Получено", ui.downValue)))

	storageCard := newStorageCard(ctrl, ui)

	ui.errLabel = widget.NewLabel("")
	ui.errLabel.Wrapping = fyne.TextWrapWord
	ui.errLabel.Importance = widget.DangerImportance
	ui.errLabel.SizeName = theme.SizeNameCaptionText
	ui.errLabel.Hide()

	// Передачи — строка: пустое состояние больше не занимает карточку.
	ui.transferText = caption("Передачи: нет")
	ui.transferBar = widget.NewProgressBar()
	ui.transferBar.Hide()
	ui.transferBox = container.NewVBox(ui.transferText, ui.transferBar)

	// Снимки экрана создаёт система; пока нет разрешения на чтение фото,
	// они не попадают в папку синхронизации — говорим об этом и даём кнопку.
	ui.shotLabel = caption("Снимки экрана не переносятся в папку: у приложения нет доступа к фото.")
	ui.shotLabel.Importance = widget.WarningImportance
	ui.btnShotPerm = widget.NewButtonWithIcon("Разрешить доступ к фото", theme.MediaPhotoIcon(), func() {
		go ctrl.RequestPhotoAccess()
	})
	ui.shotBox = container.NewVBox(ui.shotLabel, ui.btnShotPerm)
	ui.shotBox.Hide()

	serverCard := cardBox(container.NewVBox(
		valueRow("Сервер", ui.serverValue),
		valueRow("Папка", ui.folderValue),
	))

	// обновление: версию берём из манифеста, APK ищем в папке синхронизации
	ui.updateInfo = caption("Проверяем обновление…")
	ui.btnInstall = widget.NewButtonWithIcon("Установить", theme.DownloadIcon(), func() {
		path := ui.updateAPK
		if path == "" {
			return
		}
		go func() {
			if err := camera.OpenFile(path); err != nil {
				fyne.Do(func() { dialog.ShowError(err, ui.win) })
			}
		}()
	})
	ui.btnInstall.Importance = widget.HighImportance
	ui.btnInstall.Hide()
	updateTitle := widget.NewLabelWithStyle("Обновление", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	updateTitle.SizeName = theme.SizeNameText
	updateCard := cardBox(container.NewVBox(
		updateTitle,
		ui.updateInfo,
		buttonBox(widget.NewButtonWithIcon("Проверить обновление", theme.ViewRefreshIcon(), func() {
			ui.checkUpdate(ctrl, true)
		})),
		ui.btnInstall,
	))

	statusPage := container.NewVBox(
		heading("POSCloud"),
		syncCard,
		buttonBox(ui.btnNow),
		ui.shotBox,
		statsCard,
		storageCard,
		ui.transferBox,
		ui.errLabel,
		updateCard,
		serverCard,
		helpText("Синхронизация работает, пока приложение открыто; для фоновой работы держите его в недавних."),
		layout.NewSpacer(),
	)

	// ===== вкладка «Связь» =====
	// Одно главное действие — скан QR. Ручной ввод кода и ручная настройка
	// свёрнуты; кнопки формы закреплены внизу и не уезжают за прокруткой.
	ui.pairEntry = widget.NewMultiLineEntry()
	ui.pairEntry.SetPlaceHolder("poscloud://connect?…")
	ui.pairEntry.Wrapping = fyne.TextWrapBreak
	ui.pairEntry.SetMinRowsVisible(2)

	pairBtn := widget.NewButtonWithIcon("Подключиться по коду", theme.LoginIcon(), func() {
		code := strings.TrimSpace(ui.pairEntry.Text)
		if code == "" {
			dialog.ShowInformation("Код подключения", "Вставьте код, выданный сервером POSCloud (кнопка «Код подключения» в окне сервера), или отсканируйте его камерой.", w)
			return
		}
		ui.pairConnect(ctrl, code)
	})
	ui.pairBox = container.NewVBox(fieldFill(ui.pairEntry), buttonBox(pairBtn))
	ui.pairBox.Hide()
	ui.pairToggle = widget.NewButton("Ввести код вручную", nil)
	ui.pairToggle.Alignment = widget.ButtonAlignLeading
	ui.pairToggle.Importance = widget.LowImportance
	ui.pairToggle.OnTapped = func() {
		show := !ui.pairBox.Visible()
		showIf(ui.pairBox, show)
	}

	scanBtn := widget.NewButtonWithIcon("Сканировать QR-код", theme.MediaPhotoIcon(), func() {
		ui.scanQR(ctrl)
	})
	scanBtn.Importance = widget.HighImportance

	pasteBtn := widget.NewButtonWithIcon("Вставить код из буфера", theme.ContentPasteIcon(), func() {
		if cb := w.Clipboard(); cb != nil {
			if code := strings.TrimSpace(cb.Content()); code != "" {
				ui.pairEntry.SetText(code)
				showIf(ui.pairBox, true)
			}
		}
	})

	quickCard := cardBox(container.NewVBox(
		buttonBox(scanBtn),
		buttonBox(pasteBtn),
		ui.pairToggle,
		ui.pairBox,
	))

	ui.hostEntry = newFormEntry(false)
	ui.hostEntry.SetPlaceHolder("192.168.1.10")
	ui.portEntry = newFormEntry(true)
	ui.portEntry.SetPlaceHolder("8090")
	ui.tokenEntry = &formEntry{Entry: widget.NewPasswordEntry()}
	ui.tokenEntry.SetPlaceHolder("токен устройства")
	ui.deviceEntry = newFormEntry(false)
	ui.deviceEntry.SetPlaceHolder("имя устройства")

	ui.tlsSwitch = newPillSwitch(false, nil)
	ui.fpEntry = widget.NewMultiLineEntry()
	ui.fpEntry.Wrapping = fyne.TextWrapBreak
	ui.fpEntry.TextStyle = fyne.TextStyle{Monospace: true}
	ui.fpEntry.SetMinRowsVisible(2)
	ui.fpEntry.SetPlaceHolder("SHA-256")
	ui.fpBox = container.NewVBox(
		fieldRow("Отпечаток", fieldFill(ui.fpEntry)),
		helperLine("64 hex-символа, сверяется по SHA-256."),
	)
	ui.fpBox.Hide()
	ui.tlsSwitch.OnChanged = func(on bool) { showIf(ui.fpBox, on) }

	serverCardForm := cardBox(container.NewVBox(
		pairRow("Адрес", ui.hostEntry, "Порт", ui.portEntry, colPort),
		fieldRow("Токен", fieldFill(ui.tokenEntry)),
		fieldRow("Имя устройства", fieldFill(ui.deviceEntry)),
		switchRow("TLS с самоподписанным сертификатом", ui.tlsSwitch),
		ui.fpBox,
	))

	// —— дополнительно: папка, интервал, тихие часы, лимит, автозапуск ——
	ui.dirLabel = widget.NewLabel("")
	ui.dirLabel.Wrapping = fyne.TextWrapBreak
	ui.dirLabel.SizeName = theme.SizeNameCaptionText
	pickBtn := widget.NewButtonWithIcon("Выбрать папку…", theme.FolderOpenIcon(), func() {
		ui.pickFolder(ctrl)
	})
	ui.intervalEntry = newFormEntry(true)
	ui.intervalEntry.SetPlaceHolder("3")
	intervalRow := container.NewHBox(fieldBox(colNarrow, ui.intervalEntry), helpText("с"))

	ui.quietSwitch = newPillSwitch(false, nil)
	ui.quietFrom = newFormEntry(false)
	ui.quietFrom.SetPlaceHolder("23:00")
	ui.quietFrom.Validator = clockValidator
	ui.quietFrom.OnChanged = func(string) { clockFilter(ui.quietFrom.Entry) }
	ui.quietTo = newFormEntry(false)
	ui.quietTo.SetPlaceHolder("07:00")
	ui.quietTo.Validator = clockValidator
	ui.quietTo.OnChanged = func(string) { clockFilter(ui.quietTo.Entry) }
	ui.quietErr = fieldErrorLabel()
	quietFields := container.NewGridWithColumns(2,
		container.NewVBox(fieldLabel("с"), fieldFill(ui.quietFrom)),
		container.NewVBox(fieldLabel("по"), fieldFill(ui.quietTo)),
	)
	ui.quietRow = container.NewVBox(quietFields, ui.quietErr)
	ui.quietRow.Hide()
	ui.quietSwitch.OnChanged = func(on bool) { showIf(ui.quietRow, on) }

	ui.maxDownEntr = newFormEntry(true)
	ui.maxDownEntr.SetPlaceHolder("0")
	ui.maxDownEntr.Validator = limitValidator
	ui.maxDownEntr.OnChanged = func(string) { digitsFilter(ui.maxDownEntr.Entry, 5) }
	ui.limitErr = fieldErrorLabel()
	limitRow := container.NewHBox(fieldBox(colNarrow, ui.maxDownEntr), helpText("МБ"))

	ui.autoSwitch = newPillSwitch(false, nil)

	advanced := container.NewVBox(
		fieldRow("Папка", container.NewVBox(ui.dirLabel, pickBtn)),
		fieldRow("Интервал", intervalRow),
		switchRow("Тихие часы", ui.quietSwitch),
		ui.quietRow,
		fieldRow("Лимит скачивания", limitRow),
		helperLine("0 — без ограничения. Файлы больше лимита не скачиваются, причина — в журнале."),
		ui.limitErr,
		switchRow("Запускать синхронизацию при старте", ui.autoSwitch),
	)
	ui.advancedCard = widget.NewCard("", "", advanced)
	ui.advancedCard.Hide()
	ui.advancedToggle = widget.NewButtonWithIcon("Дополнительно", theme.SettingsIcon(), nil)
	ui.advancedToggle.Alignment = widget.ButtonAlignLeading
	ui.advancedToggle.OnTapped = func() {
		showIf(ui.advancedCard, !ui.advancedCard.Visible())
	}
	advancedBlock := container.NewVBox(ui.advancedToggle, ui.advancedCard)

	ui.testInfo = caption("")
	ui.testInfo.Wrapping = fyne.TextWrapWord
	ui.testInfo.Hide()
	ui.saveInfo = caption("")
	ui.saveInfo.Importance = widget.SuccessImportance
	ui.saveInfo.Hide()

	testBtn := widget.NewButtonWithIcon("Проверить связь", theme.ConfirmIcon(), func() {
		setTextIfChanged(ui.testInfo, "Проверяем связь…")
		ui.testInfo.Importance = widget.LowImportance
		showIf(ui.testInfo, true)
		go func() {
			res := ctrl.TestConnection(strings.TrimSpace(ui.hostEntry.Text),
				atoiDefault(ui.portEntry.Text, cfg.Port), ui.tokenEntry.Text,
				ui.tlsSwitch.Checked(), ui.fingerprint())
			fyne.Do(func() {
				setTextIfChanged(ui.testInfo, res)
				if strings.HasPrefix(res, "Ошибка") {
					ui.testInfo.Importance = widget.DangerImportance
				} else {
					ui.testInfo.Importance = widget.SuccessImportance
				}
				ui.testInfo.Refresh()
			})
		}()
	})
	saveBtn := widget.NewButtonWithIcon("Сохранить настройки", theme.DocumentSaveIcon(), func() {
		ui.saveSettings(ctrl)
	})
	saveBtn.Importance = widget.HighImportance

	footer := container.NewVBox(
		widget.NewSeparator(),
		ui.testInfo, ui.saveInfo,
		container.NewGridWithColumns(2, buttonBox(testBtn), buttonBox(saveBtn)))

	formScroll := container.NewVScroll(container.NewVBox(
		sectionLabel("Быстрое подключение"),
		quickCard,
		sectionLabel("Сервер"),
		serverCardForm,
		advancedBlock,
		helpText("Сервер создаёт пользователя и устройство и выдаёт токен. На Android по умолчанию используется папка внутри приложения; внешние пути (например /storage/emulated/0/…) требуют разрешения «Доступ ко всем файлам» и могут не работать."),
		bottomPad(),
		layout.NewSpacer(),
	))
	ui.setFormScroll(formScroll)

	connectPage := container.NewBorder(
		heading("Подключение"),
		footer,
		nil, nil,
		formScroll,
	)
	ui.loadFromConfig(ctrl)

	// ===== вкладка «Папки» =====
	ui.freeSwitch = newPillSwitch(ctrl.RulesFreeEnabled(), func(on bool) {
		go func() {
			if err := ctrl.SetRulesFree(on); err != nil {
				fyne.Do(func() { dialog.ShowError(err, ui.win) })
			}
			fyne.Do(func() { refreshFolders(ctrl, ui) })
		}()
	})

	ui.foldersState = caption("")
	ui.foldersBox = container.NewVBox()
	ui.btnRefreshFld = iconButton(theme.ViewRefreshIcon(), func() { refreshFolders(ctrl, ui) })
	moreBtn := iconButton(theme.MoreHorizontalIcon(), func() {
		var d *dialog.CustomDialog
		d = dialog.NewCustom("Папки", "Отмена", container.NewVBox(
			dialogRow("Полная сверка", theme.SearchIcon(), func() {
				d.Hide()
				ctrl.Resync()
				dialog.ShowInformation("Полная сверка", "Сервер будет перечитан полностью при следующем цикле.", w)
			}),
			dialogRow("Обновить список", theme.ViewRefreshIcon(), func() {
				d.Hide()
				refreshFolders(ctrl, ui)
			}),
		), w)
		d.Resize(dialogSize(w))
		d.Show()
	})

	foldersHeader := container.NewVBox(
		pageHeader("Папки", ui.btnRefreshFld, moreBtn),
		cardBox(container.NewVBox(
			switchRow("Без правил — синхронизировать всё", ui.freeSwitch),
			ui.foldersState,
		)),
	)
	foldersPage := container.NewBorder(foldersHeader, nil, nil, nil, container.NewVScroll(ui.foldersBox))

	// ===== вкладка «Журнал» =====
	ui.logLabel = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	ui.logLabel.Wrapping = fyne.TextWrapWord
	ui.logLabel.SizeName = theme.SizeNameCaptionText
	// куда пишется журнал — вопрос возникает сразу, как только что-то не работает:
	// полный журнал в каталоге приложения (на телефоне без root не прочитать),
	// а копия — в папке синхронизации, её видно на компьютере.
	ui.logPathLabel = caption("")
	ui.logPathLabel.Wrapping = fyne.TextWrapBreak
	ui.logPathValue = logPathText(ctrl)
	ui.logPathLabel.SetText(ui.logPathValue)
	// копию журнала кладём в папку синхронизации только по кнопке: файл уезжает
	// на сервер и дальше на компьютер, где его можно открыть и прочитать
	ui.logSaveLabel = caption("")
	ui.logSaveLabel.Wrapping = fyne.TextWrapWord
	ui.btnSaveLog = iconButton(theme.DocumentSaveIcon(), func() {
		ui.btnSaveLog.Disable()
		go func() {
			p, err := ctrl.SaveLogCopy()
			fyne.Do(func() {
				ui.btnSaveLog.Enable()
				if err != nil {
					ui.logSaveLabel.Importance = widget.DangerImportance
					ui.logSaveLabel.SetText("Не удалось сохранить: " + err.Error())
					return
				}
				ui.logSaveLabel.Importance = widget.SuccessImportance
				ui.logSaveLabel.SetText("Сохранено в " + time.Now().Format("15:04:05") + ": " + filepath.Base(p) + " → уедет на компьютер")
			})
		}()
	})
	clearBtn := iconButton(theme.DeleteIcon(), func() {
		ctrl.ClearLogs()
		ui.logLabel.SetText("")
	})
	logTop := container.NewVBox(
		pageHeader("Журнал", ui.btnSaveLog, clearBtn),
		ui.logPathLabel,
		ui.logSaveLabel,
	)
	logPage := container.NewBorder(logTop, nil, nil, nil, container.NewScroll(ui.logLabel))

	// ===== вкладка «Файлы» =====
	ui.files = newFilesPage(ctrl, w)

	// ===== вкладка «Лента» =====
	ui.recent = newRecentPage(ctrl, w)

	// Нижняя панель — своя (см. pager.go): пять стандартных вкладок Fyne на
	// экране телефона не влезают, и «Файлы» с «Журналом» уезжали бы в «…».
	pg := newPager()
	pg.Add("Статус", theme.HomeIcon(), container.NewScroll(statusPage))
	pg.Add("Связь", theme.SettingsIcon(), connectPage)
	pg.Add("Папки", theme.FolderOpenIcon(), foldersPage)
	pg.Add("Файлы", theme.ListIcon(), ui.files.object())
	pg.Add("Лента", theme.HistoryIcon(), ui.recent.object())
	pg.Add("Журнал", theme.DocumentIcon(), logPage)
	// дерево файлов и ленту перечитываем при открытии страницы; клавиатуру
	// при смене вкладки убираем — иначе она остаётся висеть поверх новой
	pg.OnSelect = func(name string) {
		if w.Canvas() != nil {
			w.Canvas().Unfocus()
		}
		if name == "Статус" {
			refreshStorage(ctrl, ui, false)
		}
		if name == "Файлы" {
			ui.files.Refresh()
		}
		if name == "Лента" {
			ui.recent.Refresh()
		}
	}
	// переход из ленты к дереву файлов
	ui.recent.goFiles = func() { pg.SelectByName("Файлы") }
	ui.pager = pg
	ui.Content = pg.object()

	ui.refreshStatic(ctrl)
	// Список папок запрашиваем только при настроенном подключении: без токена
	// сервер отвечает отказом, а горутина запроса не должна переживать сборку
	// интерфейса (в тестах это даёт гонку в шейпере текста).
	if ctrl.HasCredentials() {
		refreshFolders(ctrl, ui)
		refreshStorage(ctrl, ui, true)
	} else {
		ui.foldersState.SetText("Подключитесь к серверу — здесь появятся папки для выборочной синхронизации.")
	}
	return ui
}

// helpText — пояснение под блоком: мелкое, второстепенное.
func helpText(text string) fyne.CanvasObject {
	l := caption(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}

// fieldErrorLabel — подпись ошибки под полем: скрыта, пока ошибки нет.
func fieldErrorLabel() *widget.Label {
	l := caption("")
	l.Importance = widget.DangerImportance
	l.Hide()
	return l
}

// setFieldError показывает или прячет ошибку под полем.
func setFieldError(l *widget.Label, err error) {
	if err == nil {
		l.SetText("")
		showIf(l, false)
		return
	}
	l.SetText(err.Error())
	showIf(l, true)
}

// setFormScroll привязывает поля формы к прокрутке: при фокусе форма
// подкручивается к полю, иначе клавиатура закрывает его.
func (ui *State) setFormScroll(s *container.Scroll) {
	for _, e := range []*formEntry{ui.hostEntry, ui.portEntry, ui.tokenEntry,
		ui.deviceEntry, ui.intervalEntry, ui.quietFrom, ui.quietTo, ui.maxDownEntr} {
		if e != nil {
			e.setScroll(s)
		}
	}
	// «Далее» ведёт по форме сверху вниз, последнее поле закрывает клавиатуру.
	chain(ui.hostEntry, ui.portEntry, ui.tokenEntry, ui.deviceEntry,
		ui.intervalEntry, ui.quietFrom, ui.quietTo, ui.maxDownEntr)
}

// scanQR — выбор способа сканирования: снять камерой или взять готовый снимок.
func (ui *State) scanQR(ctrl *client.Controller) {
	var d *dialog.CustomDialog
	rows := container.NewVBox(
		caption("Снимок сохранится в папке синхронизации, приложение подключится само."),
		dialogRow("Сфотографировать", theme.MediaPhotoIcon(), func() {
			d.Hide()
			ui.shootAndConnect(ctrl)
		}),
		dialogRow("Выбрать снимок из галереи", theme.FolderOpenIcon(), func() {
			d.Hide()
			ui.pickQR(ctrl)
		}),
	)
	d = dialog.NewCustom("Сканировать QR-код", "Отмена", rows, ui.win)
	d.Resize(dialogSize(ui.win))
	d.Show()
}

// pickQR берёт снимок с QR-кодом из файлов устройства.
func (ui *State) pickQR(ctrl *client.Controller) {
	w := ui.win
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
				showIf(ui.pairBox, true)
				ui.pairConnect(ctrl, code)
			})
		}(r)
	}, w)
	fd.SetFilter(storage.NewMimeTypeFileFilter([]string{"image/*"}))
	fd.Show()
}

// shootAndConnect снимает QR системной камерой. Снимок сохраняется
// в папку синхронизации (через MediaStore), затем декодируется.
func (ui *State) shootAndConnect(ctrl *client.Controller) {
	dir := cleanPath(ui.dirValue)
	if dir == "" {
		dir = ctrl.DefaultSyncDir()
	}
	// Снимок всегда кладём в подпапку «Фото смартфона» внутри папки
	// синхронизации: она уезжает на сервер вместе с остальными файлами.
	captureRel := "DCIM/POScloud"
	photoDir := filepath.Join(dir, "Фото смартфона")
	if err := os.MkdirAll(photoDir, 0o755); err != nil {
		dialog.ShowError(err, ui.win)
		return
	}
	// Каталог DCIM/POScloud на Android 10+ через файловый API не создаётся
	// (scoped storage, EACCES) — его создаёт MediaStore при вставке снимка;
	// mkdir нужен только старым версиям Android, поэтому ошибка не критична.
	_ = os.MkdirAll(filepath.Join("/storage/emulated/0", captureRel), 0o755)
	name := "POSCloud-" + time.Now().Format("20060102-150405") + ".jpg"

	wait := dialog.NewCustomWithoutButtons("Камера", container.NewVBox(
		widget.NewLabel("Открываем камеру и ждём снимок…"),
		widget.NewProgressBarInfinite(),
	), ui.win)
	wait.Show()

	go func() {
		uri, err := camera.Capture(name, captureRel)
		if err != nil {
			fyne.Do(func() {
				wait.Hide()
				dialog.ShowError(err, ui.win)
			})
			return
		}
		deadline := time.Now().Add(3 * time.Minute)
		var size int64
		stable := 0
		for time.Now().Before(deadline) {
			s := camera.Size(uri)
			if s > 0 && s == size {
				stable++
				if stable >= 2 {
					break
				}
			} else {
				stable = 0
			}
			size = s
			time.Sleep(400 * time.Millisecond)
		}
		fyne.Do(func() { wait.Hide() })
		if size <= 0 {
			_ = camera.Delete(uri)
			fyne.Do(func() {
				dialog.ShowInformation("Камера", "Снимок не появился — съёмка отменена.", ui.win)
			})
			return
		}

		// Файл создаёт приложение камеры: читаем его через свою запись MediaStore.
		src, openErr := camera.Open(uri)
		if openErr != nil {
			fyne.Do(func() { dialog.ShowError(openErr, ui.win) })
			return
		}

		target := filepath.Join(photoDir, name)
		_ = os.MkdirAll(photoDir, 0o755)
		out, createErr := os.Create(target)
		copyErr := createErr
		if copyErr == nil {
			_, copyErr = io.Copy(out, src)
			if cerr := out.Close(); copyErr == nil {
				copyErr = cerr
			}
		}
		_ = src.Close()
		// Оригинал в DCIM/POScloud больше не нужен: снимок живёт в папке
		// синхронизации, дубликат в галерее только путает.
		_ = camera.Delete(uri)
		if copyErr != nil {
			fyne.Do(func() { dialog.ShowError(copyErr, ui.win) })
			return
		}
		f, err := os.Open(target)
		if err != nil {
			fyne.Do(func() { dialog.ShowError(err, ui.win) })
			return
		}
		code, decodeErr := client.DecodeQRReader(f)
		_ = f.Close()
		fyne.Do(func() {
			if decodeErr != nil {
				dialog.ShowInformation("Фото сохранено",
					"Снимок лежит в папке синхронизации:\n"+target+"\n\nQR-код на снимке не найден.", ui.win)
				return
			}
			ui.pairEntry.SetText(code)
			showIf(ui.pairBox, true)
			ui.pairConnect(ctrl, code)
		})
	}()
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
			refreshFolders(ctrl, ui)
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
	ui.dirLabel.SetText(cfg.Dir)
	ui.dirValue = cfg.Dir
	ui.intervalEntry.SetText(strconv.Itoa(cfg.Interval))
	ui.tlsSwitch.SetCheckedSilently(cfg.TLS)
	showIf(ui.fpBox, cfg.TLS)
	if cfg.TLS {
		ui.fpEntry.SetText(cfg.TLSFingerprint)
	}
	ui.autoSwitch.SetCheckedSilently(ctrl.AutoRunEnabled())
	// переключатель отражает то, что тихие часы настроены, а не то, что они активны сейчас
	quietConfigured := cfg.QuietFrom != "" && cfg.QuietTo != "" && cfg.QuietFrom != cfg.QuietTo
	ui.quietSwitch.SetCheckedSilently(quietConfigured)
	showIf(ui.quietRow, quietConfigured)
	ui.quietFrom.SetText(cfg.QuietFrom)
	ui.quietTo.SetText(cfg.QuietTo)
	if cfg.MaxDownMB > 0 {
		ui.maxDownEntr.SetText(strconv.FormatInt(cfg.MaxDownMB, 10))
	} else {
		ui.maxDownEntr.SetText("")
	}
	ui.updateAdvancedTitle(ctrl)
}

// updateAdvancedTitle показывает в свёрнутом заголовке то, что уже настроено:
// адрес сервера и порт, без длинного URL (он не влезал в строку).
func (ui *State) updateAdvancedTitle(ctrl *client.Controller) {
	cfg := ctrl.Config()
	text := "Дополнительно"
	if strings.TrimSpace(cfg.Token) != "" || (strings.TrimSpace(cfg.Host) != "" && cfg.Host != "localhost") {
		text += fmt.Sprintf(" · %s:%d", cfg.Host, cfg.Port)
	}
	setButtonTextIfChanged(ui.advancedToggle, text)
}

// setSwitch выставляет переключатель синхронизации молча: таймер обновляет
// положение каждую секунду по факту, а не по желанию.
func (ui *State) setSwitch(on bool) { ui.swSync.SetCheckedSilently(on) }

// startSync запускает синхронизацию по полям вкладки «Подключение»; при
// ошибке возвращает переключатель в положение «выкл».
func (ui *State) startSync(ctrl *client.Controller) {
	host := strings.TrimSpace(ui.hostEntry.Text)
	port := atoiDefault(ui.portEntry.Text, ctrl.Config().Port)
	interval := atoiDefault(ui.intervalEntry.Text, ctrl.Config().Interval)
	if err := ctrl.Start(host, port, cleanPath(ui.dirValue), interval,
		ui.tokenEntry.Text, ui.tlsSwitch.Checked(), ui.fingerprint()); err != nil {
		ui.setSwitch(false)
		dialog.ShowError(err, ui.win)
		return
	}
	ui.refreshStatic(ctrl)
	refreshFolders(ctrl, ui)
}

// saveSettings проверяет ввод и сохраняет настройки. Ошибки показываются
// подписями под полями, а не модальными окнами без имени поля.
func (ui *State) saveSettings(ctrl *client.Controller) {
	cfg := ctrl.Config()
	setFieldError(ui.quietErr, nil)
	setFieldError(ui.limitErr, nil)

	quietFrom, quietTo := "", ""
	if ui.quietSwitch.Checked() {
		var okFrom, okTo bool
		quietFrom, okFrom = normClock(ui.quietFrom.Text)
		quietTo, okTo = normClock(ui.quietTo.Text)
		if !okFrom || !okTo {
			setFieldError(ui.quietErr, fmt.Errorf("Введите время в формате ЧЧ:ММ — например 23:00 и 07:00"))
			ui.quietFrom.ensureVisible()
			return
		}
	}
	limitTxt := strings.TrimSpace(ui.maxDownEntr.Text)
	if limitTxt != "" {
		if n, err := strconv.Atoi(limitTxt); err != nil || n < 0 {
			setFieldError(ui.limitErr, fmt.Errorf("Целое число мегабайт или пусто — без лимита"))
			ui.maxDownEntr.ensureVisible()
			return
		}
	}
	ctrl.SaveSettings(strings.TrimSpace(ui.hostEntry.Text),
		atoiDefault(ui.portEntry.Text, cfg.Port),
		cleanPath(ui.dirValue),
		atoiDefault(ui.intervalEntry.Text, cfg.Interval),
		ui.tokenEntry.Text, ui.tlsSwitch.Checked(), ui.fingerprint(),
		ui.deviceEntry.Text, ui.autoSwitch.Checked(),
		quietFrom, quietTo, int64(atoiDefault(ui.maxDownEntr.Text, 0)))
	// показываем канонический вид введённого («9:5» → «09:05»)
	ui.quietFrom.SetText(quietFrom)
	ui.quietTo.SetText(quietTo)
	ui.refreshStatic(ctrl)
	ui.updateAdvancedTitle(ctrl)
	setTextIfChanged(ui.saveInfo, "Настройки сохранены в "+time.Now().Format("15:04"))
	showIf(ui.saveInfo, true)
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
	var lastSyncSeen string
	ticks := 0
	photoGranted := ctrl.PhotoPermissionGranted()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ticks++
			// Разрешение приходит после ответа в системном диалоге: как только
			// оно появилось, сразу забираем накопившиеся снимки экрана.
			if granted := ctrl.PhotoPermissionGranted(); granted != photoGranted {
				photoGranted = granted
				if granted {
					go ctrl.ImportScreenshots()
				}
			}
			shotAsks := ctrl.PhotoPermissionAsks()
			// Раз в 30 секунд забираем новые снимки экрана в папку синхронизации.
			if ticks == 6 {
				// автопроверка обновления: тихо, баннер появится сам
				ui.checkUpdate(ctrl, false)
			}
			if ticks%30 == 0 {
				go ctrl.ImportScreenshots()
			}
			st := ctrl.Status()
			cfg := ctrl.Config()
			running := ctrl.Running()
			// Хранилище пересчитываем после цикла (метка цикла изменилась), но
			// только пока открыта «Статус» и не чаще раза в минуту — см. refreshStorage.
			if st.LastSync != lastSyncSeen {
				lastSyncSeen = st.LastSync
				if running && ui.pager.Current() == "Статус" {
					refreshStorage(ctrl, ui, false)
				}
			}
			tr := client.Transfers.Snapshot()
			// ленту «Недавние» перечитываем только когда она открыта и пришли
			// новые события — иначе зря перерисовываем список
			feedDirty := ui.pager.Current() == "Лента" && ctrl.EventsDirtyAndReset()

			onlineTxt := "—"
			if running {
				if st.Online {
					onlineTxt = "есть"
				} else if st.ErrMsg != "" {
					onlineTxt = "нет"
				} else {
					onlineTxt = "ждём"
				}
			}
			stateTxt := "остановлена"
			if running {
				switch {
				case st.DirErr != "":
					stateTxt = "папка недоступна для записи"
				case st.Online && st.LastSync != "":
					stateTxt = "работает · цикл " + st.LastSync
				case st.Online:
					stateTxt = "работает"
				case st.ErrMsg != "":
					stateTxt = "проблема связи: " + st.ErrMsg
				default:
					stateTxt = "работает — ждём сервер"
				}
			}
			countTxt := fmt.Sprintf("%d", st.Count)
			up, down := client.Transfers.Totals()

			var logTxt string
			if ctrl.LogDirtyAndReset() {
				lines := ctrl.Logs()
				if len(lines) > 300 {
					lines = lines[len(lines)-300:]
				}
				logTxt = strings.Join(lines, "\n")
				// общий префикс пути выносим из строк: иначе каждая запись
				// занимает три строки на телефоне
				if root := ctrl.Root(); root != "" {
					logTxt = strings.ReplaceAll(logTxt, root, "…")
				}
				lastLog = logTxt
			}

			fyne.Do(func() {
				ui.setSwitch(running)
				setTextIfChanged(ui.stateSub, stateTxt)
				setTextIfChanged(ui.upValue, humanBytes(up))
				setTextIfChanged(ui.downValue, humanBytes(down))
				if photoGranted {
					showIf(ui.shotBox, false)
				} else {
					// после двух отказов диалог больше не появится — ведём в настройки
					if shotAsks >= 2 {
						setButtonTextIfChanged(ui.btnShotPerm, "Открыть настройки приложения")
					}
					showIf(ui.shotBox, true)
				}
				setTextIfChanged(ui.onlineValue, onlineTxt)
				setTextIfChanged(ui.serverValue, eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
				setTextIfChanged(ui.folderValue, cfg.Dir)
				setTextIfChanged(ui.countValue, countTxt)
				if st.DirErr != "" {
					setTextIfChanged(ui.errLabel, "Папка синхронизации недоступна для записи: "+st.DirErr+"\nВыберите другую папку на вкладке «Связь».")
					showIf(ui.errLabel, true)
				} else if st.ErrMsg != "" {
					setTextIfChanged(ui.errLabel, st.ErrMsg)
					showIf(ui.errLabel, true)
				} else {
					showIf(ui.errLabel, false)
				}
				if running {
					ui.btnNow.Enable()
				} else {
					ui.btnNow.Disable()
				}
				if len(tr) == 0 {
					setTextIfChanged(ui.transferText, "Передачи: нет")
					ui.transferBar.SetValue(0)
					showIf(ui.transferBar, false)
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
					setTextIfChanged(ui.transferText, fmt.Sprintf("%s  %s — %d%%%s", arrow, top.Name, top.Pct, extra))
					showIf(ui.transferBar, true)
					ui.transferBar.SetValue(float64(top.Pct) / 100)
				}
				if logTxt != "" {
					ui.logLabel.SetText(lastLog)
				}
				if feedDirty {
					ui.recent.Refresh()
				}
				// куда пишется журнал: полный — в данных приложения, копия — в папке
				if ticks%5 == 0 {
					if txt := logPathText(ctrl); txt != ui.logPathValue {
						ui.logPathValue = txt
						ui.logPathLabel.SetText(txt)
					}
				}
				ui.updateAdvancedTitle(ctrl)
			})
		}
	}
}

// logPathText — подпись на вкладке «Журнал»: где лежит полный журнал и его копия.
func logPathText(ctrl *client.Controller) string {
	return "Журнал: " + filepath.Join(ctrl.Root(), "client.log") +
		"\nКопия (по кнопке) — в папке синхронизации: " + ctrl.LogMirrorPath()
}

// refreshFolders запрашивает у движка список папок первого уровня и строит
// компактные строки: папки с данными — списком, пустые — под раскрывашкой.
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
			var filled, empty []eng.ServerFolderNode
			for _, n := range nodes {
				if n.Files == 0 && n.LocFiles == 0 {
					empty = append(empty, n)
				} else {
					filled = append(filled, n)
				}
			}
			ui.foldersState.SetText(fmt.Sprintf("Папок: %d", len(nodes)))

			objs := make([]fyne.CanvasObject, 0, 4)
			if len(filled) > 0 {
				rows := container.NewVBox()
				for i, n := range filled {
					if i > 0 {
						rows.Add(widget.NewSeparator())
					}
					rows.Add(folderRow(ctrl, ui, n))
				}
				objs = append(objs, cardBox(rows))
			}
			if len(empty) > 0 {
				rows := container.NewVBox()
				for i, n := range empty {
					if i > 0 {
						rows.Add(widget.NewSeparator())
					}
					rows.Add(folderRow(ctrl, ui, n))
				}
				rows.Hide()
				toggle := widget.NewButton(fmt.Sprintf("▸ Ещё %d пустых", len(empty)), nil)
				toggle.Alignment = widget.ButtonAlignLeading
				toggle.Importance = widget.LowImportance
				toggle.OnTapped = func() {
					show := !rows.Visible()
					showIf(rows, show)
					if show {
						setButtonTextIfChanged(toggle, fmt.Sprintf("▾ Ещё %d пустых", len(empty)))
					} else {
						setButtonTextIfChanged(toggle, fmt.Sprintf("▸ Ещё %d пустых", len(empty)))
					}
				}
				objs = append(objs, toggle, cardBox(rows))
			}
			ui.foldersBox.Objects = objs
			ui.foldersBox.Refresh()
		})
	}()
}

// fingerprint возвращает отпечаток без пробелов и переносов строк (поле
// многострочное, а в hex-строке пробелов быть не должно).
func (ui *State) fingerprint() string {
	return strings.Join(strings.Fields(ui.fpEntry.Text), "")
}

// folderRow строит компактную строку папки: имя, статистика и чип режима.
// Раньше каждая папка была карточкой на 150–170 dp с полноширинным Select —
// двенадцать папок занимали четыре экрана.
func folderRow(ctrl *client.Controller, ui *State, n eng.ServerFolderNode) fyne.CanvasObject {
	stats := fmt.Sprintf("сервер %d · здесь %d", n.Files, n.LocFiles)
	if n.New {
		stats += " · новая"
	}
	name := widget.NewLabelWithStyle(n.Path, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	name.SizeName = theme.SizeNameText
	name.Wrapping = fyne.TextWrapBreak
	details := caption(stats)

	chip := widget.NewButton(shortModeTitle(n.Mode), func() {
		showModeDialog(ctrl, ui, n)
	})

	return container.NewBorder(nil, nil, nil, chip, container.NewVBox(name, details))
}

// showModeDialog — выбор режима папки: четыре варианта крупными строками
// вместо выпадающего списка (на телефоне он требует точного попадания).
func showModeDialog(ctrl *client.Controller, ui *State, n eng.ServerFolderNode) {
	var d *dialog.CustomDialog
	rows := container.NewVBox()
	for _, p := range modePairs {
		mode := p.mode
		rows.Add(dialogRow(modeTitle(mode), theme.ConfirmIcon(), func() {
			d.Hide()
			if err := ctrl.SetFolderRule(n.Path, mode); err != nil {
				dialog.ShowError(err, ui.win)
			}
			refreshFolders(ctrl, ui)
		}))
	}
	d = dialog.NewCustom(n.Path, "Отмена", rows, ui.win)
	d.Resize(dialogSize(ui.win))
	d.Show()
}

// ---------- утилиты ----------

// moveFile переносит файл; если переименование невозможно (разные файловые
// системы), копирует и удаляет исходный.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// cleanPath убирает переводы строк, которые может добавить многострочное поле.
func cleanPath(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return strings.TrimSpace(s)
}

// setTextIfChanged обновляет подпись только при изменении текста: SetText
// в Fyne всегда вызывает Refresh, а тикер ходит каждую секунду — на телефоне
// это лишняя перерисовка и расход батареи.
func setTextIfChanged(l *widget.Label, text string) {
	if l.Text != text {
		l.SetText(text)
	}
}

// setButtonTextIfChanged — то же для кнопок.
func setButtonTextIfChanged(b *widget.Button, text string) {
	if b.Text != text {
		b.SetText(text)
	}
}

// showIf показывает или прячет объект только при смене состояния: Show и Hide
// на уже показанном или уже спрятанном объекте тоже дергают канвас.
func showIf(o fyne.CanvasObject, show bool) {
	if show && !o.Visible() {
		o.Show()
	} else if !show && o.Visible() {
		o.Hide()
	}
}

// ---------- проверка ввода ----------

// validClock проверяет «ЧЧ:ММ»: часы 0–23, минуты 0–59.
func validClock(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	h, err1 := strconv.Atoi(s[0:2])
	m, err2 := strconv.Atoi(s[3:5])
	return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

// normClock принимает и доводит до канонического вида: «9:5» → «09:05».
// ok=false — ввести не получилось (мусор).
func normClock(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	parts := strings.Split(s, ":")
	if len(parts) == 2 {
		for i := range parts {
			if len(parts[i]) == 0 || len(parts[i]) > 2 {
				return "", false
			}
			if len(parts[i]) == 1 {
				parts[i] = "0" + parts[i]
			}
		}
		s = parts[0] + ":" + parts[1]
	}
	if !validClock(s) {
		return "", false
	}
	return s, true
}

// clockValidator — валидатор поля «ЧЧ:ММ»: пусто допустимо (окно выключено).
func clockValidator(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if _, ok := normClock(s); !ok {
		return fmt.Errorf("формат ЧЧ:ММ, например 23:00")
	}
	return nil
}

// clockFilter не пускает в поле времени ничего, кроме цифр и одного
// двоеточия; после двух цифр двоеточие вставляется само, длина ≤ «ЧЧ:ММ».
func clockFilter(e *widget.Entry) {
	var b strings.Builder
	wroteColon := false
	for _, r := range e.Text {
		switch {
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ':' && !wroteColon:
			b.WriteRune(r)
			wroteColon = true
		}
	}
	s := b.String()
	if len(s) > 5 {
		s = s[:5]
	}
	// «231» → «23:1»: двоеточие после двух цифр, если его ещё нет
	if len(s) >= 3 && !strings.Contains(s, ":") {
		s = s[:2] + ":" + s[2:]
	}
	if s != e.Text {
		e.SetText(s)
	}
}

// limitValidator — валидатор поля лимита: целое ≥ 0 или пусто.
func limitValidator(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err != nil || n < 0 {
		return fmt.Errorf("целое число мегабайт или пусто")
	}
	return nil
}

// digitsFilter не пускает в поле ничего, кроме цифр (не больше max знаков).
func digitsFilter(e *widget.Entry, max int) {
	var b strings.Builder
	for _, r := range e.Text {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	if s != e.Text {
		e.SetText(s)
	}
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

// pluralFiles склоняет «файл» по русским правилам: 1 файл, 2 файла, 5 файлов, 11 файлов.
func pluralFiles(n int) string {
	switch {
	case n%10 == 1 && n%100 != 11:
		return fmt.Sprintf("%d файл", n)
	case n%10 >= 2 && n%10 <= 4 && (n%100 < 12 || n%100 > 14):
		return fmt.Sprintf("%d файла", n)
	default:
		return fmt.Sprintf("%d файлов", n)
	}
}

// checkUpdate проверяет, нет ли в папке синхронизации APK новее установленного.
// manual — нажали кнопку (тогда показываем ответ диалогом даже без обновления).
func (ui *State) checkUpdate(ctrl *client.Controller, manual bool) {
	go func() {
		v, path := ctrl.CheckUpdate()
		cur := ctrl.AppVersion()
		fyne.Do(func() {
			if cur == "" {
				ui.updateInfo.SetText("Версия приложения не определена (сборка не из APK)")
				return
			}
			if v == "" {
				ui.updateInfo.SetText("Установлена версия " + cur + " — обновлений нет")
				ui.btnInstall.Hide()
				if manual {
					dialog.ShowInformation("Обновление", "Установлена последняя версия "+cur+".", ui.win)
				}
				return
			}
			ui.updateAPK = path
			ui.updateInfo.SetText("Доступно " + v + " (сейчас " + cur + ")")
			ui.btnInstall.Show()
			if manual {
				dialog.ShowConfirm("Доступно обновление "+v,
					"Скачанный APK лежит в папке синхронизации.\nУстановить сейчас?",
					func(ok bool) {
						if !ok {
							return
						}
						go func() {
							if err := camera.OpenFile(path); err != nil {
								fyne.Do(func() { dialog.ShowError(err, ui.win) })
							}
						}()
					}, ui.win)
			}
		})
	}()
}
