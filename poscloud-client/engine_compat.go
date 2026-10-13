package main

// Тонкий слой совместимости: правила папок живут в общем движке poscloud-engine,
// чтобы тот же код использовала и служебная сборка для старых Windows.

import eng "poscloud-engine"

type FolderRule = eng.FolderRule
type folderRules = eng.Rules

const (
	folderBoth = eng.FolderBoth
	folderUp   = eng.FolderUp
	folderDown = eng.FolderDown
	folderOff  = eng.FolderOff
)

func validFolderMode(m string) bool { return eng.ValidFolderMode(m) }

func folderModeTitle(m string) string { return eng.FolderModeTitle(m) }

func normalizeRulePath(p string) (string, bool) { return eng.NormalizeRulePath(p) }

func parseFolderRules(list []FolderRule) folderRules { return eng.ParseRules(list) }
