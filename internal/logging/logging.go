// Package logging — простая настройка структурированного логирования
// на основе log/slog. В обычном режиме логируются только ошибки,
// в режиме debug — всё, включая тела запросов.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// New создаёт логгер. При debug=true уровень логирования опускается
// до Debug, иначе остаётся на уровне Error.
func New(debug bool) *slog.Logger {
	level := slog.LevelError
	if debug {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// Diff возвращает простое построчное различие двух текстов: общие
// начало и конец отбрасываются, различающаяся середина помечается
// "-" (было) и "+" (стало). Используется только в debug-режиме.
func Diff(a, b string) []string {
	as := strings.Split(a, "\n")
	bs := strings.Split(b, "\n")
	pre := 0
	for pre < len(as) && pre < len(bs) && as[pre] == bs[pre] {
		pre++
	}
	suf := 0
	for suf < len(as)-pre && suf < len(bs)-pre && as[len(as)-1-suf] == bs[len(bs)-1-suf] {
		suf++
	}
	out := make([]string, 0, len(as)+len(bs))
	for _, l := range as[pre : len(as)-suf] {
		out = append(out, "- "+l)
	}
	for _, l := range bs[pre : len(bs)-suf] {
		out = append(out, "+ "+l)
	}
	return out
}
