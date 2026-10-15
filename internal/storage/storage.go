// Package storage — хранилище правил. Интерфейс Store позволяет позже
// заменить файловую реализацию (FileStore) на SQLite или другую БД,
// не трогая остальной код.
package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/kartashoffvbif-blip/transmutator/internal/rules"
	"github.com/kartashoffvbif-blip/transmutator/internal/transform"
)

// Store — источник правил. Загрузка выполняется один раз при старте,
// горячей перезагрузки нет.
type Store interface {
	// Load загружает и полностью валидирует правила (структура,
	// переменные, компиляция фильтров). Любая ошибка здесь —
	// причина не стартовать сервис.
	Load() ([]rules.Rule, error)
}

// FileStore — файловое хранилище: YAML-манифест правил,
// фильтры могут лежать во внешних .jq-файлах.
type FileStore struct {
	path string // путь к манифесту правил
}

// NewFileStore создаёт файловое хранилище по пути к манифесту.
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// manifestRule — правило в формате YAML-манифеста.
type manifestRule struct {
	Key          string         `yaml:"key"`
	Model        string         `yaml:"model"`
	Priority     *int           `yaml:"priority"`
	FilterFile   string         `yaml:"filter_file"`
	FilterInline *string        `yaml:"filter_inline"`
	Vars         map[string]any `yaml:"vars"`
}

type manifest struct {
	Rules []manifestRule `yaml:"rules"`
}

// validVarName — имена переменных должны быть обычными jq-идентификаторами.
var validVarName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Load читает манифест, валидирует каждое правило и проверяет,
// что все фильтры парсятся и компилируются.
func (s *FileStore) Load() ([]rules.Rule, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл правил %q: %w", s.path, err)
	}
	var m manifest
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // опечатки в полях правил — ошибка, а не игнор
	if err := dec.Decode(&m); err != nil {
		if err == io.EOF {
			// пустой файл: правил нет, сервис работает чистым прокси
			return nil, nil
		}
		return nil, fmt.Errorf("файл правил %q не является корректным YAML: %w", s.path, err)
	}

	base := filepath.Dir(s.path)
	out := make([]rules.Rule, 0, len(m.Rules))
	for i, mr := range m.Rules {
		r, err := s.buildRule(base, i, mr)
		if err != nil {
			return nil, fmt.Errorf("правило #%d в %s: %w", i+1, s.path, err)
		}
		out = append(out, r)
	}
	// полная проверка: каждый фильтр обязан компилироваться с переменными
	for _, r := range out {
		if _, err := transform.CompileRule(r); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *FileStore) buildRule(base string, idx int, mr manifestRule) (rules.Rule, error) {
	if strings.TrimSpace(mr.Key) == "" {
		return rules.Rule{}, fmt.Errorf("key обязателен и не может быть пустым")
	}
	filter, origin, err := s.resolveFilter(base, mr)
	if err != nil {
		return rules.Rule{}, err
	}
	vars, err := buildVars(mr.Vars)
	if err != nil {
		return rules.Rule{}, err
	}
	r := rules.Rule{
		Key:    mr.Key,
		Model:  mr.Model, // пустая строка — правило только по ключу
		Filter: filter,
		Vars:   vars,
		Origin: fmt.Sprintf("#%d (key=%q, model=%q, приоритет=%d, %s)", idx+1, mr.Key, mr.Model, prio(mr), origin),
	}
	if mr.Priority != nil {
		r.Priority = *mr.Priority
	}
	return r, nil
}

// resolveFilter гарантирует, что указан ровно один источник фильтра,
// и возвращает его текст и человекочитаемое происхождение.
func (s *FileStore) resolveFilter(base string, mr manifestRule) (filter, origin string, err error) {
	switch {
	case mr.FilterFile != "" && mr.FilterInline != nil:
		return "", "", fmt.Errorf("указаны и filter_file, и filter_inline — должно быть ровно одно")
	case mr.FilterFile != "":
		path := mr.FilterFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path) // относительно расположения файла правил
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", fmt.Errorf("не удалось прочитать файл фильтра %q: %w", path, err)
		}
		return string(raw), "filter_file " + path, nil
	case mr.FilterInline != nil:
		return *mr.FilterInline, "filter_inline", nil
	default:
		return "", "", fmt.Errorf("не указан ни filter_file, ни filter_inline")
	}
}

func buildVars(raw map[string]any) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]any, len(raw))
	for name, v := range raw {
		if !validVarName.MatchString(name) {
			return nil, fmt.Errorf("имя переменной %q не является валидным идентификатором", name)
		}
		nv, err := transform.NormalizeValue(v)
		if err != nil {
			return nil, fmt.Errorf("переменная $%s: %w", name, err)
		}
		out[name] = nv
	}
	return out, nil
}

func prio(mr manifestRule) int {
	if mr.Priority != nil {
		return *mr.Priority
	}
	return 0
}
