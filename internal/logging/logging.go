// Package logging — настройка структурированного логирования на основе
// log/slog. В обычном режиме логируются только ошибки, в режиме debug —
// всё. При заданном log_file логи дублируются в файл.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New создаёт логгер. При debug=true уровень логирования опускается
// до Debug, иначе остаётся на уровне Error. Если logFile не пуст,
// логи пишутся одновременно в stdout и в этот файл (дозапись).
// Возвращённый closer закрывает файл; для чисто stdout-логгера он nil.
func New(debug bool, logFile string) (*slog.Logger, io.Closer, error) {
	level := slog.LevelError
	if debug {
		level = slog.LevelDebug
	}
	out := io.Writer(os.Stdout)
	var closer io.Closer
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, err
		}
		out = io.MultiWriter(os.Stdout, f)
		closer = f
	}
	h := slog.NewTextHandler(out, &slog.HandlerOptions{Level: level})
	return slog.New(h), closer, nil
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
