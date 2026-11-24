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
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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

// setDir запоминает выбранную папку и показывает её в форме.
func (st *State) setDir(dir string) {
	st.dirValue = dir
	st.dirLabel.SetText(dir)
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

// tile — плитка «подпись + крупное значение» для сетки 2×2.
func tile(label string, value *widget.Label) fyne.CanvasObject {
	return widget.NewCard("", "", container.NewVBox(caption(label), value))
}

// ---------- состояние интерфейса ----------

// State держит виджеты, которые обновляет таймер.
type State struct {
	Content fyne.CanvasObject

	// статус
	stateTitle   *widget.Label
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
	transferCard *widget.Card
	shotBox      *fyne.Container
	shotLabel    *widget.Label
	btnShotPerm  *widget.Button

	btnNow *widget.Button

	// подключение (поля читает «Запустить»)
	hostEntry     *widget.Entry
	portEntry     *widget.Entry
	tokenEntry    *widget.Entry
	deviceEntry   *widget.Entry
	dirLabel      *widget.Label
	dirValue      string
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
	logLabel     *widget.Label
	logPathLabel *widget.Label
	logPathValue string
	logSaveLabel *widget.Label
	btnSaveLog   *widget.Button

	// файлы
	files *filesPage

	// вкладки
	pager *pager

	win fyne.Window
}

func Build(ctrl *client.Controller, w fyne.Window) *State {
	ui := &State{win: w}
	cfg := ctrl.Config()

	// ===== вкладка «Статус» =====
	// Макет B: главное действие — переключатель «Синхронизация» (как Wi-Fi в
	// настройках телефона), под ним одна крупная кнопка «сейчас», цифры —
	// плитками 2×2. Кнопки и плитки — в высоту пальца.
	ui.serverValue = widget.NewLabel(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
	ui.onlineValue = widget.NewLabel("—")
	ui.folderValue = widget.NewLabel(cfg.Dir)
	ui.countValue = widget.NewLabel("—")
	ui.upValue = widget.NewLabel("0 Б")
	ui.downValue = widget.NewLabel("0 Б")
	// Путь к папке — одна длинная строка без пробелов: переносим её в любом месте,
	// иначе карточка требует ширину больше экрана телефона.
	ui.folderValue.Wrapping = fyne.TextWrapBreak
	for _, l := range []*widget.Label{ui.onlineValue, ui.countValue, ui.upValue, ui.downValue} {
		l.TextStyle = fyne.TextStyle{Bold: true}
		l.SizeName = theme.SizeNameHeadingText
	}

	ui.stateTitle = widget.NewLabelWithStyle("Синхронизация", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	ui.stateTitle.SizeName = theme.SizeNameHeadingText
	ui.stateSub = caption("остановлена")
	ui.swSync = newPillSwitch(false, nil)
	ui.onToggle = func(on bool) {
		if on {
			ui.startSync(ctrl)
		} else {
			ctrl.Stop()
		}
	}
	ui.swSync.OnChanged = ui.onToggle
	// Переключатель держим в окошке своего размера: BorderLayout растянул бы его
	// на всю высоту карточки, и ползунок стал бы огромным кругом.
	swBox := container.New(layout.NewGridWrapLayout(fyne.NewSize(toggleW, toggleH)), ui.swSync)
	syncCard := container.NewPadded(widget.NewCard("", "", container.NewBorder(nil, nil,
		container.NewVBox(ui.stateTitle, ui.stateSub), swBox)))

	ui.btnNow = widget.NewButtonWithIcon("Синхронизировать сейчас", theme.ViewRefreshIcon(), func() { ctrl.SyncNow() })
	ui.btnNow.Importance = widget.HighImportance
	ui.btnNow.Disable()

	tiles := container.NewGridWithColumns(2,
		tile("Связь", ui.onlineValue), tile("Файлов", ui.countValue),
		tile("Отправлено", ui.upValue), tile("Получено", ui.downValue),
	)

	ui.errLabel = widget.NewLabel("")
	ui.errLabel.Wrapping = fyne.TextWrapWord
	ui.errLabel.Importance = widget.DangerImportance
	ui.errLabel.Hide()

	ui.transferText = caption("Передач нет")
	ui.transferBar = widget.NewProgressBar()
	ui.transferBar.Hide()
	transferCard := widget.NewCard("Передачи", "", container.NewVBox(ui.transferText, ui.transferBar))
	ui.transferCard = transferCard
	transferCard.Hide()

	// Снимки экрана создаёт система; пока нет разрешения на чтение фото,
	// они не попадают в папку синхронизации — говорим об этом и даём кнопку.
	ui.shotLabel = caption("Снимки экрана не переносятся в папку: у приложения нет доступа к фото.")
	ui.shotLabel.Importance = widget.WarningImportance
	ui.btnShotPerm = widget.NewButtonWithIcon("Разрешить доступ к фото", theme.MediaPhotoIcon(), func() {
		go ctrl.RequestPhotoAccess()
	})
	ui.btnShotPerm.Importance = widget.HighImportance
	ui.shotBox = container.NewVBox(ui.shotLabel, ui.btnShotPerm)
	ui.shotBox.Hide()

	serverCard := widget.NewCard("", "", container.NewVBox(
		fieldRow("Сервер", ui.serverValue),
		fieldRow("Папка", ui.folderValue),
	))

	statusCard := container.NewVBox(
		heading("POSCloud"),
		syncCard,
		ui.btnNow,
		ui.shotBox,
		tiles,
		serverCard,
		transferCard,
		ui.errLabel,
		container.NewPadded(caption("Синхронизация работает, пока приложение открыто; для фоновой работы держите его в недавних.")),
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
	qrBtn := widget.NewButtonWithIcon("Снимок QR", theme.MediaPhotoIcon(), func() {
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

	// Съёмка системной камерой: снимок сразу ложится в папку синхронизации
	// и, если на нём QR, приложение подключается само.
	shootBtn := widget.NewButtonWithIcon("Сфотографировать", theme.MediaPhotoIcon(), func() {
		ui.shootAndConnect(ctrl)
	})
	shootBtn.Importance = widget.HighImportance

	// Вставить код из буфера — быстрее, чем набирать его вручную.
	pasteBtn := widget.NewButtonWithIcon("Из буфера", theme.ContentPasteIcon(), func() {
		if cb := w.Clipboard(); cb != nil {
			if code := strings.TrimSpace(cb.Content()); code != "" {
				ui.pairEntry.SetText(code)
			}
		}
	})

	quickCard := widget.NewCard("Быстрое подключение", "", container.NewVBox(
		shootBtn,
		container.NewGridWithColumns(2, qrBtn, pasteBtn),
		caption("Сфотографируйте QR камерой: снимок сохранится в «Фото смартфона» внутри папки синхронизации, приложение подключится само."),
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
	ui.dirLabel = widget.NewLabel("")
	ui.dirLabel.Wrapping = fyne.TextWrapBreak
	// Папка выбирается диалогом: путь — только для чтения, длинный путь
	// переносится целиком и не рисует текст за границами поля.
	pickBtn := widget.NewButtonWithIcon("Выбрать папку…", theme.FolderOpenIcon(), func() {
		ui.pickFolder(ctrl)
	})
	ui.intervalEntry = widget.NewEntry()
	ui.intervalEntry.SetPlaceHolder("3")
	ui.tlsCheck = widget.NewCheck("TLS (самоподписанный сертификат)", nil)
	// Отпечаток — 64 hex-символа: в одну строку не влезает, поэтому поле многострочное
	// с переносом в любом месте (минимум две строки).
	ui.fpEntry = widget.NewMultiLineEntry()
	ui.fpEntry.Wrapping = fyne.TextWrapBreak
	ui.fpEntry.SetMinRowsVisible(2)
	ui.fpEntry.SetPlaceHolder("SHA-256")
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
	// Адрес и порт — на одной горизонтали: порт узкий, адрес занимает остальное.
	portBox := container.New(layout.NewGridWrapLayout(fyne.NewSize(96, 46)), ui.portEntry)

	form := container.NewVBox(
		container.NewBorder(nil, nil, nil, formRow("Порт", portBox), formRow("Адрес сервера", ui.hostEntry)),
		formRow("Токен", ui.tokenEntry),
		formRow("Имя устройства", ui.deviceEntry),
		formRow("Папка", container.NewVBox(ui.dirLabel, pickBtn)),
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
				ui.tlsCheck.Checked, ui.fingerprint())
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
			cleanPath(ui.dirValue),
			atoiDefault(ui.intervalEntry.Text, cfg.Interval),
			ui.tokenEntry.Text, ui.tlsCheck.Checked, ui.fingerprint(),
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
			widget.NewCard("", "", container.NewVBox(
				ui.freeCheck,
				container.NewGridWithColumns(2, ui.btnRefreshFld, ui.btnResync),
				ui.foldersState,
			)),
		),
		nil, nil, nil,
		container.NewScroll(ui.foldersBox),
	)

	// ===== вкладка «Журнал» =====
	ui.logLabel = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Monospace: true})
	ui.logLabel.Wrapping = fyne.TextWrapWord
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
	ui.btnSaveLog = widget.NewButtonWithIcon("Сохранить журнал в папку", theme.DocumentSaveIcon(), func() {
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
	clearBtn := widget.NewButtonWithIcon("Очистить журнал", theme.DeleteIcon(), func() {
		ctrl.ClearLogs()
		ui.logLabel.SetText("")
	})
	logCard := container.NewBorder(
		container.NewVBox(heading("Журнал"), clearBtn, ui.btnSaveLog, ui.logSaveLabel, ui.logPathLabel),
		nil, nil, nil,
		container.NewScroll(ui.logLabel),
	)

	// ===== вкладка «Файлы» =====
	ui.files = newFilesPage(ctrl, w)

	// Нижняя панель — своя (см. pager.go): пять стандартных вкладок Fyne на
	// экране телефона не влезают, и «Файлы» с «Журналом» уезжали бы в «…».
	pg := newPager()
	pg.Add("Статус", theme.HomeIcon(), withBottomInset(container.NewScroll(statusCard)))
	pg.Add("Связь", theme.SettingsIcon(), withBottomInset(container.NewScroll(connectCard)))
	pg.Add("Папки", theme.FolderOpenIcon(), withBottomInset(foldersCard))
	pg.Add("Файлы", theme.ListIcon(), withBottomInset(ui.files.object()))
	pg.Add("Журнал", theme.DocumentIcon(), withBottomInset(logCard))
	// дерево файлов перечитываем при открытии страницы
	pg.OnSelect = func(name string) {
		if name == "Файлы" {
			ui.files.Refresh()
		}
	}
	ui.pager = pg
	ui.Content = pg.object()

	ui.refreshStatic(ctrl)
	// Список папок запрашиваем только при настроенном подключении: без токена
	// сервер отвечает отказом, а горутина запроса не должна переживать сборку
	// интерфейса (в тестах это даёт гонку в шейпере текста).
	if ctrl.HasCredentials() {
		refreshFolders(ctrl, ui)
	} else {
		ui.foldersState.SetText("Подключитесь к серверу — здесь появятся папки для выборочной синхронизации.")
	}
	return ui
}

// fieldRow строит пару «подпись — значение» в столбик. Классический Border
// с подписью слева на телефоне сжимает переносимую подпись до нулевой ширины,
// и текст наезжает на значение. Подпись рисуется canvas.Text: у него нет
// внутренних отступов виджета, и строка получается ниже.
func fieldRow(label string, value *widget.Label) fyne.CanvasObject {
	cap := canvas.NewText(label, colMuted)
	cap.TextSize = 12
	cap.TextStyle = fyne.TextStyle{Italic: true}
	return container.NewVBox(cap, value)
}

// formRow строит строку формы для узкого экрана: подпись над полем.
func formRow(label string, obj fyne.CanvasObject) fyne.CanvasObject {
	l := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	l.Wrapping = fyne.TextWrapWord
	return container.NewVBox(l, obj)
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
	ui.tlsCheck.SetChecked(cfg.TLS)
	if cfg.TLS {
		ui.fpEntry.SetText(cfg.TLSFingerprint)
	}
	ui.autoRunCheck.SetChecked(ctrl.AutoRunEnabled())
}

// setSwitch выставляет переключатель синхронизации, не вызывая его обработчик:
// таймер обновляет положение каждую секунду по факту, а не по желанию.
func (ui *State) setSwitch(on bool) {
	ui.swSync.OnChanged = nil
	ui.swSync.SetChecked(on)
	ui.swSync.OnChanged = ui.onToggle
}

// startSync запускает синхронизацию по полям вкладки «Подключение»; при
// ошибке возвращает переключатель в положение «выкл».
func (ui *State) startSync(ctrl *client.Controller) {
	host := strings.TrimSpace(ui.hostEntry.Text)
	port := atoiDefault(ui.portEntry.Text, ctrl.Config().Port)
	interval := atoiDefault(ui.intervalEntry.Text, ctrl.Config().Interval)
	if err := ctrl.Start(host, port, cleanPath(ui.dirValue), interval,
		ui.tokenEntry.Text, ui.tlsCheck.Checked, ui.fingerprint()); err != nil {
		ui.setSwitch(false)
		dialog.ShowError(err, ui.win)
		return
	}
	ui.refreshStatic(ctrl)
	refreshFolders(ctrl, ui)
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
			if ticks%30 == 0 {
				go ctrl.ImportScreenshots()
			}
			st := ctrl.Status()
			cfg := ctrl.Config()
			running := ctrl.Running()
			tr := client.Transfers.Snapshot()

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
				lastLog = logTxt
			}

			fyne.Do(func() {
				ui.setSwitch(running)
				ui.stateSub.SetText(stateTxt)
				ui.upValue.SetText(humanBytes(up))
				ui.downValue.SetText(humanBytes(down))
				if photoGranted {
					ui.shotBox.Hide()
				} else {
					// после двух отказов диалог больше не появится — ведём в настройки
					if shotAsks >= 2 {
						ui.btnShotPerm.SetText("Открыть настройки приложения")
					}
					ui.shotBox.Show()
				}
				ui.onlineValue.SetText(onlineTxt)
				ui.serverValue.SetText(eng.BaseURL(cfg.Host, cfg.Port, cfg.TLS))
				ui.folderValue.SetText(cfg.Dir)
				ui.countValue.SetText(countTxt)
				if st.DirErr != "" {
					ui.errLabel.SetText("Папка синхронизации недоступна для записи: " + st.DirErr + "\nВыберите другую папку на вкладке «Связь».")
					ui.errLabel.Show()
				} else if st.ErrMsg != "" {
					ui.errLabel.SetText(st.ErrMsg)
					ui.errLabel.Show()
				} else {
					ui.errLabel.Hide()
				}
				if running {
					ui.btnNow.Enable()
				} else {
					ui.btnNow.Disable()
				}
				if len(tr) == 0 {
					ui.transferText.SetText("Передач нет")
					ui.transferBar.SetValue(0)
					ui.transferBar.Hide()
					ui.transferCard.Hide()
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
					ui.transferCard.Show()
				}
				if logTxt != "" {
					ui.logLabel.SetText(lastLog)
				}
				// куда пишется журнал: полный — в данных приложения, копия — в папке
				if ticks%5 == 0 {
					if txt := logPathText(ctrl); txt != ui.logPathValue {
						ui.logPathValue = txt
						ui.logPathLabel.SetText(txt)
					}
				}
			})
		}
	}
}

// logPathText — подпись на вкладке «Журнал»: где лежит полный журнал и его копия.
func logPathText(ctrl *client.Controller) string {
	return "Журнал: " + filepath.Join(ctrl.Root(), "client.log") +
		"\nКопия (по кнопке) — в папке синхронизации: " + ctrl.LogMirrorPath()
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

// fingerprint возвращает отпечаток без пробелов и переносов строк (поле
// многострочное, а в hex-строке пробелов быть не должно).
func (ui *State) fingerprint() string {
	return strings.Join(strings.Fields(ui.fpEntry.Text), "")
}

// folderRow строит карточку папки: имя, статистика и выбор режима.
func folderRow(ctrl *client.Controller, ui *State, n eng.ServerFolderNode) fyne.CanvasObject {
	info := fmt.Sprintf("на сервере: %s (%s)", pluralFiles(n.Files), humanBytes(n.Bytes))
	if n.LocFiles > 0 || n.Local {
		info += fmt.Sprintf("\nздесь: %s (%s)", pluralFiles(n.LocFiles), humanBytes(n.LocBytes))
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
const bottomInset = 12

// withBottomInset добавляет пустую полосу под содержимым вкладки.
func withBottomInset(obj fyne.CanvasObject) fyne.CanvasObject {
	spacer := canvas.NewRectangle(colBg)
	spacer.SetMinSize(fyne.NewSize(0, bottomInset))
	return container.NewBorder(nil, spacer, nil, nil, obj)
}

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

// unusedImageImport — image/png и image/jpeg нужны для декодирования снимков.
