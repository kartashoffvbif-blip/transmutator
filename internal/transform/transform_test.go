package transform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kartashoffvbif-blip/transmutator/internal/rules"
)

func rule(filter string, vars map[string]any) rules.Rule {
	return rules.Rule{Key: "k", Filter: filter, Vars: vars, Origin: "test-правило"}
}

func mustApply(t *testing.T, e *Engine, filter string, vars map[string]any, input any) any {
	t.Helper()
	cr, err := CompileRule(rule(filter, vars))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	out, err := e.Apply(context.Background(), cr, input)
	if err != nil {
		t.Fatalf("выполнение: %v", err)
	}
	return out
}

func TestApplySimple(t *testing.T) {
	e, err := New(nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	input := map[string]any{"a": 1, "messages": []any{"x"}}
	got := mustApply(t, e, `.a = .a + 1`, nil, input)
	m := got.(map[string]any)
	if m["a"] != 2 {
		t.Errorf("a = %v, ожидалось 2", m["a"])
	}
}

func TestApplyVariablesPassed(t *testing.T) {
	e, _ := New(nil, time.Second)
	input := map[string]any{"word": "SECRET", "messages": []any{}}
	got := mustApply(t, e, `.word = $replacement`, map[string]any{"replacement": "safe"}, input)
	if got.(map[string]any)["word"] != "safe" {
		t.Errorf("word = %v, ожидалось safe", got.(map[string]any)["word"])
	}
}

func TestApplySequentialRules(t *testing.T) {
	e, _ := New(nil, time.Second)
	input := map[string]any{"log": []any{}}
	first := mustApply(t, e, `.log += ["first"]`, nil, input)
	second := mustApply(t, e, `.log += ["second"]`, nil, first)
	l := second.(map[string]any)["log"].([]any)
	if len(l) != 2 || l[0] != "first" || l[1] != "second" {
		t.Errorf("правила применились не последовательно: %v", l)
	}
}

func TestApplyMultipleResultsIsError(t *testing.T) {
	e, _ := New(nil, time.Second)
	cr, _ := CompileRule(rule(`. , .`, nil))
	if _, err := e.Apply(context.Background(), cr, map[string]any{}); err == nil {
		t.Error("два результата — должна быть ошибка")
	}
}

func TestApplyZeroResultsIsError(t *testing.T) {
	e, _ := New(nil, time.Second)
	cr, _ := CompileRule(rule(`empty`, nil))
	if _, err := e.Apply(context.Background(), cr, map[string]any{}); err == nil {
		t.Error("ноль результатов — должна быть ошибка")
	}
}

func TestApplyFilterError(t *testing.T) {
	e, _ := New(nil, time.Second)
	cr, _ := CompileRule(rule(`error("boom")`, nil))
	_, err := e.Apply(context.Background(), cr, map[string]any{})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if errors.Is(err, ErrTimeout) {
		t.Error("ошибка фильтра не должна классифицироваться как таймаут")
	}
}

func TestApplyTimeout(t *testing.T) {
	e, _ := New(nil, 30*time.Millisecond)
	cr, _ := CompileRule(rule(`reduce range(0; 10000000000) as $i (0; . + $i)`, nil))
	start := time.Now()
	_, err := e.Apply(context.Background(), cr, map[string]any{})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ожидалась ошибка таймаута")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("ожидался ErrTimeout, получено: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("таймаут сработал слишком поздно: %s", elapsed)
	}
}

func TestApplyNonObjectResultPasses(t *testing.T) {
	// не-объект обнаруживает валидация на уровне httpapi;
	// сам движок просто возвращает значение
	e, _ := New(nil, time.Second)
	got := mustApply(t, e, `.messages`, nil, map[string]any{"messages": []any{1}})
	if _, ok := got.([]any); !ok {
		t.Errorf("ожидался массив, получено %v", got)
	}
}

func TestNewCompilesAllRules(t *testing.T) {
	rs := []rules.Rule{
		{Key: "k", Filter: ".", Origin: "ok"},
		{Key: "k", Filter: ".foo |", Origin: "битый"},
	}
	if _, err := New(rs, time.Second); err == nil {
		t.Error("битый фильтр должен ломать создание движка")
	}
}

func TestNewRejectsBadTimeout(t *testing.T) {
	if _, err := New(nil, 0); err == nil {
		t.Error("нулевой таймаут должен быть ошибкой")
	}
}

func TestSelectUsesRuleSemantics(t *testing.T) {
	rs := []rules.Rule{
		{Key: "k", Model: "m", Filter: ".tag = \"km\""},
		{Key: "k", Filter: ".tag = \"k\""},
		{Key: "other", Filter: ".x = 1"},
	}
	e, err := New(rs, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Select("k", "m"); len(got) != 1 || got[0].Rule.Filter != `.tag = "km"` {
		t.Errorf("Select(k, m) = %v", got)
	}
	if got := e.Select("k", ""); len(got) != 1 || got[0].Rule.Filter != `.tag = "k"` {
		t.Errorf("Select(k, \"\") = %v", got)
	}
	if got := e.Select("unknown", ""); len(got) != 0 {
		t.Errorf("Select(unknown) = %v", got)
	}
}

func TestNormalizeValue(t *testing.T) {
	tests := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{"s", "s"},
		{true, true},
		{int64(5), 5},
		{float64(1.5), 1.5},
	}
	for _, tt := range tests {
		got, err := NormalizeValue(tt.in)
		if err != nil {
			t.Errorf("NormalizeValue(%v): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeValue(%v) = %v, ожидалось %v", tt.in, got, tt.want)
		}
	}
	if _, err := NormalizeValue(complex(1, 2)); err == nil {
		t.Error("неподдерживаемый тип должен давать ошибку")
	}
}

func TestParseJSONIntegers(t *testing.T) {
	v, err := ParseJSON([]byte(`{"n": 12345678901234567891, "f": 1.5, "arr": [1, 2]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	// большое целое не должно молча стать float — это json.Number -> int (или float при переполнении)
	switch m["n"].(type) {
	case int, float64:
	default:
		t.Errorf("n имеет неожиданный тип %T", m["n"])
	}
	if m["f"] != 1.5 {
		t.Errorf("f = %v", m["f"])
	}
	if _, err := ParseJSON([]byte(`{`)); err == nil {
		t.Error("битый JSON должен давать ошибку")
	}
}
