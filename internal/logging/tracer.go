// Трассировка запросов: в debug-режиме каждая обработка целевого
// запроса пишется в trace_dir тремя обычными файлами — исходное тело,
// преобразованное тело и построчный diff. Файлы содержат «чистый»
// JSON без экранирования, в отличие от текстовых логов slog.
package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Tracer пишет артефакты одного преобразования в отдельные файлы.
// Безопасен для параллельных запросов: имя файла уникализируется
// счётчиком и меткой времени.
type Tracer struct {
	dir string
	mu  sync.Mutex
	seq uint64
}

// NewTracer создаёт Tracer, записывающий файлы в dir. При dir == ""
// трассировка выключена (все методы no-op).
func NewTracer(dir string) *Tracer {
	return &Tracer{dir: dir}
}

// Enabled сообщает, включена ли трассировка.
func (t *Tracer) Enabled() bool { return t != nil && t.dir != "" }

// WriteTransform сохраняет исходное тело, преобразованное и их diff
// в файлы вида 20060102-150405.000001-{request,transformed,diff}.*.
// Возвращает базовое имя (без суффикса) для ссылки из лога.
func (t *Tracer) WriteTransform(original, transformed []byte) (base string, err error) {
	if !t.Enabled() {
		return "", nil
	}
	t.mu.Lock()
	t.seq++
	seq := t.seq
	t.mu.Unlock()

	base = time.Now().Format("20060102-150405") + fmt.Sprintf(".%06d", seq)
	write := func(suffix, ext, data string) error {
		return os.WriteFile(
			filepath.Join(t.dir, base+"-"+suffix+"."+ext),
			[]byte(data), 0o644)
	}
	if err = write("request", "json", pretty(original)); err != nil {
		return base, err
	}
	if err = write("transformed", "json", pretty(transformed)); err != nil {
		return base, err
	}
	if err = write("diff", "patch", strings.Join(Diff(pretty(original), pretty(transformed)), "\n")); err != nil {
		return base, err
	}
	return base, nil
}

func pretty(raw []byte) string {
	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return string(raw)
	}
	return indented.String()
}
