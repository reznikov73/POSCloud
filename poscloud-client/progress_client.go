package main

import eng "poscloud-engine"

// progressAdapter передаёт движку события о ходе передачи файлов,
// чтобы панель передач в интерфейсе продолжала работать как раньше.
type progressAdapter struct{}

func (progressAdapter) Begin(name, dir string, total int64) { transfers.begin(name, dir, total) }
func (progressAdapter) Add(name string, n int64)            { transfers.add(name, n) }
func (progressAdapter) End(name string)                     { transfers.end(name) }

func init() { eng.Progress = progressAdapter{} }
