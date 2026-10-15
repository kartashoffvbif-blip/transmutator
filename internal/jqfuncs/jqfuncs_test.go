package jqfuncs_test

import (
	"context"
	"testing"

	"github.com/itchyny/gojq"

	"github.com/kartashoffvbif-blip/transmutator/internal/jqfuncs"
	"github.com/kartashoffvbif-blip/transmutator/internal/transform"
)

// norm приводит значение к типам, которые ожидает gojq.
func norm(t *testing.T, v any) any {
	t.Helper()
	nv, err := transform.NormalizeValue(v)
	if err != nil {
		t.Fatalf("нормализация: %v", err)
	}
	return nv
}

// runFilter компилирует фильтр с кастомными функциями и возвращает
// ровно один результат либо ошибку.
func runFilter(t *testing.T, filter string, input any) any {
	t.Helper()
	q, err := gojq.Parse(filter)
	if err != nil {
		t.Fatalf("фильтр не парсится: %v", err)
	}
	code, err := gojq.Compile(q, jqfuncs.Options()...)
	if err != nil {
		t.Fatalf("фильтр не компилируется: %v", err)
	}
	iter := code.RunWithContext(context.Background(), norm(t, input))
	v, ok := iter.Next()
	if !ok {
		t.Fatal("фильтр не вернул результатов")
	}
	if err, isErr := v.(error); isErr {
		t.Fatalf("фильтр вернул ошибку: %v", err)
	}
	if v2, ok := iter.Next(); ok {
		if err, isErr := v2.(error); isErr {
			t.Fatalf("фильтр вернул ошибку: %v", err)
		}
		t.Fatalf("фильтр вернул больше одного результата: %v и %v", v, v2)
	}
	return v
}

// runFilterErr — как runFilter, но ожидает ошибку и возвращает её.
func runFilterErr(t *testing.T, filter string, input any) error {
	t.Helper()
	q, _ := gojq.Parse(filter)
	code, err := gojq.Compile(q, jqfuncs.Options()...)
	if err != nil {
		t.Fatalf("фильтр не компилируется: %v", err)
	}
	iter := code.RunWithContext(context.Background(), norm(t, input))
	v, _ := iter.Next()
	if err, isErr := v.(error); isErr {
		return err
	}
	t.Fatalf("ожидалась ошибка, получено: %v", v)
	return nil
}

func TestApproxTokens(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{"строка", "привет", 6},
		{"пустая строка", "", 0},
		{"число", 42, 0},
		{"булево", true, 0},
		{"null", nil, 0},
		{"массив строк", []any{"привет", "мир"}, 9},
		{"массив строк и объектов с text", []any{"привет", map[string]any{"text": "мир"}}, 9},
		{"объект с text", map[string]any{"type": "text", "text": "мир"}, 3},
		{"объект с content-строкой", map[string]any{"role": "user", "content": "мир"}, 3},
		{"объект с картинкой без текста", map[string]any{"type": "image_url", "image_url": map[string]any{"url": "x"}}, 0},
		{"мультимодальный контент: только текстовые части", []any{
			map[string]any{"type": "text", "text": "ab"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:..."}},
		}, 2},
		{"вложенный массив", []any{[]any{"a", "bc"}}, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runFilter(t, "approx_tokens", tt.input)
			if got != tt.want {
				t.Errorf("approx_tokens(%v) = %v, ожидалось %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestCountCharsSameAsApproxTokens(t *testing.T) {
	input := map[string]any{"content": []any{map[string]any{"text": "привет"}}}
	if runFilter(t, "count_chars", input) != 6 {
		t.Errorf("count_chars = %v, ожидалось 6", runFilter(t, "count_chars", input))
	}
}

func TestSafeTruncateBlocks(t *testing.T) {
	// роли: system, user, assistant, user, assistant, tool, user, assistant
	msgs := []any{
		map[string]any{"role": "system", "content": "sys"},
		map[string]any{"role": "user", "content": "q1"},
		map[string]any{"role": "assistant", "content": "a1"},
		map[string]any{"role": "user", "content": "q2"},
		map[string]any{"role": "assistant", "content": "a2"},
		map[string]any{"role": "tool", "tool_call_id": "c1", "content": "t2"},
		map[string]any{"role": "user", "content": "q3"},
		map[string]any{"role": "assistant", "content": "a3"},
	}
	cases := []struct {
		name   string
		filter string
		want   []string // роли в ожидаемом порядке
	}{
		{"оставить 1 последний блок", "safe_truncate_blocks(1)", []string{"system", "user", "assistant"}},
		{"оставить 2 блока", "safe_truncate_blocks(2)", []string{"system", "user", "assistant", "tool", "user", "assistant"}},
		{"оставить 0 — только префикс", "safe_truncate_blocks(0)", []string{"system"}},
		{"N больше числа блоков — всё", "safe_truncate_blocks(99)", []string{"system", "user", "assistant", "user", "assistant", "tool", "user", "assistant"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runFilter(t, tc.filter, msgs)
			arr, ok := got.([]any)
			if !ok {
				t.Fatalf("результат не массив: %v", got)
			}
			if len(arr) != len(tc.want) {
				t.Fatalf("длина = %d, ожидалась %d", len(arr), len(tc.want))
			}
			for i, want := range tc.want {
				role := arr[i].(map[string]any)["role"]
				if role != want {
					t.Errorf("сообщение #%d: роль %v, ожидалась %q", i, role, want)
				}
			}
		})
	}
}

func TestSafeTruncateBlocksPreservesMessages(t *testing.T) {
	msgs := []any{
		map[string]any{"role": "user", "content": "q", "custom_field": 7},
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": "a"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "u"}},
		}},
	}
	got := runFilter(t, "safe_truncate_blocks(1)", msgs)
	arr := got.([]any)
	if len(arr) != 2 {
		t.Fatalf("длина = %d, ожидалась 2", len(arr))
	}
	if arr[0].(map[string]any)["custom_field"] != 7 {
		t.Error("неизвестное поле сообщения потеряно")
	}
	content := arr[1].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Errorf("мультимодальный контент изменён: %v", content)
	}
}

func TestSafeTruncateBlocksNoUser(t *testing.T) {
	// ни одного user — весь массив сохраняется как префикс
	msgs := []any{
		map[string]any{"role": "system", "content": "s"},
		map[string]any{"role": "assistant", "content": "a"},
	}
	got := runFilter(t, "safe_truncate_blocks(0)", msgs)
	if len(got.([]any)) != 2 {
		t.Errorf("без user-сообщений массив должен сохраниться целиком, получено %v", got)
	}
}

func TestSafeTruncateBlocksErrors(t *testing.T) {
	if err := runFilterErr(t, "safe_truncate_blocks(1)", "не массив"); err == nil {
		t.Error("вход не массив — должна быть ошибка")
	}
	if err := runFilterErr(t, "safe_truncate_blocks(-1)", []any{}); err == nil {
		t.Error("отрицательный аргумент — должна быть ошибка")
	}
	if err := runFilterErr(t, "safe_truncate_blocks(1.5)", []any{}); err == nil {
		t.Error("нецелый аргумент — должна быть ошибка")
	}
	if err := runFilterErr(t, `safe_truncate_blocks("x")`, []any{}); err == nil {
		t.Error("строка вместо числа — должна быть ошибка")
	}
}

func TestValidateOpenAIRequestFunc(t *testing.T) {
	valid := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "hi"}}}
	got := runFilter(t, "validate_openai_request", valid)
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("валидный запрос должен вернуться без изменений, получено %v", got)
	}

	invalid := map[string]any{"messages": []any{}}
	if err := runFilterErr(t, "validate_openai_request", invalid); err == nil {
		t.Error("пустой messages — должна быть ошибка")
	}

	// удобно использовать в середине пайплайна
	chained := runFilter(t, ".messages |= map(.content = \"x\") | validate_openai_request | .ok = true", valid)
	if chained.(map[string]any)["ok"] != true {
		t.Errorf("фильтр после validate_openai_request не выполнился: %v", chained)
	}
}
