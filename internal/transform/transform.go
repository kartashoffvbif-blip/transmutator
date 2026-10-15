// Package transform — компиляция jq-фильтров правил и их последовательное
// выполнение с таймаутом на базе github.com/itchyny/gojq.
package transform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/itchyny/gojq"

	"github.com/kartashoffvbif-blip/transmutator/internal/jqfuncs"
	"github.com/kartashoffvbif-blip/transmutator/internal/rules"
)

// ErrTimeout — сентинел: выполнение правила прервано по таймауту.
var ErrTimeout = errors.New("превышен таймаут выполнения правила")

// CompiledRule — правило со скомпилированным фильтром и значениями переменных.
type CompiledRule struct {
	Rule   rules.Rule
	code   *gojq.Code
	values []any // значения переменных в порядке их имён
}

// CompileRule парсит и компилирует фильтр правила вместе с переменными
// и кастомными функциями. Вызывается при старте; ошибка компиляции —
// причина не стартовать сервис.
func CompileRule(r rules.Rule) (*CompiledRule, error) {
	q, err := gojq.Parse(r.Filter)
	if err != nil {
		return nil, fmt.Errorf("правило %s: фильтр не парсится: %w", r.Origin, err)
	}
	names := make([]string, 0, len(r.Vars))
	values := make([]any, 0, len(r.Vars))
	for _, name := range sortedVarNames(r.Vars) {
		v, err := NormalizeValue(r.Vars[name])
		if err != nil {
			return nil, fmt.Errorf("правило %s: переменная $%s: %w", r.Origin, name, err)
		}
		names = append(names, "$"+name) // gojq ждёт имена вместе с префиксом $
		values = append(values, v)
	}
	opts := append(jqfuncs.Options(), gojq.WithVariables(names))
	code, err := gojq.Compile(q, opts...)
	if err != nil {
		return nil, fmt.Errorf("правило %s: фильтр не компилируется: %w", r.Origin, err)
	}
	return &CompiledRule{Rule: r, code: code, values: values}, nil
}

// Engine выполняет скомпилированные правила. Все правила выполняются
// строго последовательно, каждое — со своим таймаутом.
type Engine struct {
	timeout  time.Duration
	src      []rules.Rule    // исходные правила; Order совпадает с индексом
	compiled []*CompiledRule // скомпилированные правила в том же порядке
}

// New компилирует все правила. При любой ошибке компиляции возвращает её —
// сервис обязан завершиться на старте.
func New(rs []rules.Rule, timeout time.Duration) (*Engine, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("таймаут правила должен быть больше нуля, получено %s", timeout)
	}
	src := make([]rules.Rule, len(rs))
	copy(src, rs)
	compiled := make([]*CompiledRule, len(src))
	for i := range src {
		// порядок манифеста — это индекс в срезе; важно для стабильной сортировки
		src[i].Order = i
		c, err := CompileRule(src[i])
		if err != nil {
			return nil, err
		}
		compiled[i] = c
	}
	return &Engine{timeout: timeout, src: src, compiled: compiled}, nil
}

// Select выбирает скомпилированные правила по ключу и модели
// (семантика — как в rules.Select).
func (e *Engine) Select(key, model string) []*CompiledRule {
	sel := rules.Select(e.src, key, model)
	out := make([]*CompiledRule, 0, len(sel))
	for _, r := range sel {
		out = append(out, e.compiled[r.Order])
	}
	return out
}

// Apply выполняет один фильтр над значением с таймаутом. Фильтр обязан
// вернуть ровно один результат, иначе — ошибка выполнения правила.
func (e *Engine) Apply(ctx context.Context, c *CompiledRule, input any) (any, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	iter := c.code.RunWithContext(ctx, input, c.values...)

	first, ok := iter.Next()
	if !ok {
		return nil, fmt.Errorf("правило %s: фильтр не вернул ни одного результата", c.Rule.Origin)
	}
	if err := asError(first); err != nil {
		return nil, e.wrapError(c, err, ctx)
	}
	second, ok := iter.Next()
	if ok {
		if err := asError(second); err != nil {
			return nil, e.wrapError(c, err, ctx)
		}
		return nil, fmt.Errorf("правило %s: фильтр вернул больше одного результата", c.Rule.Origin)
	}
	return first, nil
}

func (e *Engine) wrapError(c *CompiledRule, err error, ctx context.Context) error {
	if errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("правило %s: %w", c.Rule.Origin, ErrTimeout)
	}
	return fmt.Errorf("правило %s: %w", c.Rule.Origin, err)
}

func asError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return nil
}

// ParseJSON разбирает JSON-тело запроса, сохраняя точность целых чисел
// (числа без дробной части остаются int, а не float64).
func ParseJSON(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("после JSON-значения есть лишние данные")
	}
	return NormalizeValue(v)
}

// NormalizeValue приводит значения из YAML/JSON к типам, которые
// понимает gojq: int, float64, string, bool, nil, []any, map[string]any.
func NormalizeValue(v any) (any, error) {
	switch t := v.(type) {
	case nil, bool, string, int, float64:
		return t, nil
	case int64:
		return int(t), nil // в конфигах правил такие числа всегда невелики
	case uint64:
		return int(t), nil
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i), nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, fmt.Errorf("некорректное число %q", t.String())
		}
		return f, nil
	case []any:
		out := make([]any, len(t))
		for i, el := range t {
			n, err := NormalizeValue(el)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			n, err := NormalizeValue(val)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case map[any]any: // yaml.v3 может отдать такое для смешанных ключей
		out := make(map[string]any, len(t))
		for k, val := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("ключ %v должен быть строкой", k)
			}
			n, err := NormalizeValue(val)
			if err != nil {
				return nil, err
			}
			out[ks] = n
		}
		return out, nil
	default:
		return nil, fmt.Errorf("неподдерживаемый тип значения %T", v)
	}
}

func sortedVarNames(vars map[string]any) []string {
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
