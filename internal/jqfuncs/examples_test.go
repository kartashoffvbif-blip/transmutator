package jqfuncs_test

import (
	"testing"
)

// TestReadmeExamples прогоняет примеры фильтров из README,
// чтобы документация не расходилась с реальностью.
func TestReadmeExamples(t *testing.T) {
	chat := map[string]any{
		"model": "m",
		"messages": []any{
			map[string]any{"role": "system", "content": "sys prompt"},
			map[string]any{"role": "user", "content": "hello SECRET world"},
			map[string]any{"role": "assistant", "content": "hi"},
		},
		"tools": []any{
			map[string]any{"name": "cheap_tool", "description": "d1"},
			map[string]any{"name": "expensive_tool", "description": "d2"},
		},
	}

	t.Run("обрезать историю", func(t *testing.T) {
		got := runFilter(t, ".messages = (.messages | safe_truncate_blocks(5))", chat)
		if len(got.(map[string]any)["messages"].([]any)) != 3 {
			t.Error("история обрезана неверно")
		}
	})

	t.Run("заменить системный промпт, если есть", func(t *testing.T) {
		filter := `
([.messages[] | select(.role == "system")] | length) as $n
| .messages =
    if $n > 0
    then [.messages[] | if .role == "system" then .content = "новый промпт" else . end]
    else [{"role":"system","content":"новый промпт"}] + .messages
    end`
		got := runFilter(t, filter, chat).(map[string]any)
		msgs := got["messages"].([]any)
		if msgs[0].(map[string]any)["content"] != "новый промпт" || len(msgs) != 3 {
			t.Errorf("замена системного промпта: %v", msgs)
		}
	})

	t.Run("дописать системный промпт, если нет", func(t *testing.T) {
		filter := `
([.messages[] | select(.role == "system")] | length) as $n
| .messages =
    if $n > 0
    then [.messages[] | if .role == "system" then .content = "новый промпт" else . end]
    else [{"role":"system","content":"новый промпт"}] + .messages
    end`
		noSys := map[string]any{"messages": []any{map[string]any{"role": "user", "content": "u"}}}
		got := runFilter(t, filter, noSys).(map[string]any)
		msgs := got["messages"].([]any)
		if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
			t.Errorf("дописывание системного промпта: %v", msgs)
		}
	})

	t.Run("очистить описания инструментов", func(t *testing.T) {
		got := runFilter(t, ".tools |= map(if has(\"description\") then .description = \"\" else . end)", chat)
		tools := got.(map[string]any)["tools"].([]any)
		for _, tl := range tools {
			if d := tl.(map[string]any)["description"]; d != "" {
				t.Errorf("описание не очищено: %v", d)
			}
		}
	})

	t.Run("удалить инструмент по имени", func(t *testing.T) {
		got := runFilter(t, ".tools |= map(select(.name != \"expensive_tool\"))", chat)
		tools := got.(map[string]any)["tools"].([]any)
		if len(tools) != 1 || tools[0].(map[string]any)["name"] != "cheap_tool" {
			t.Errorf("инструмент не удалён: %v", tools)
		}
	})

	t.Run("заменить слово в сообщениях", func(t *testing.T) {
		got := runFilter(t, ".messages |= map(if (.content | type) == \"string\" then .content |= gsub(\"SECRET\"; \"REDACTED\") else . end)", chat)
		msgs := got.(map[string]any)["messages"].([]any)
		if msgs[1].(map[string]any)["content"] != "hello REDACTED world" {
			t.Errorf("слово не заменено: %v", msgs[1])
		}
	})

	t.Run("префикс к системному сообщению", func(t *testing.T) {
		got := runFilter(t, ".messages |= map(if .role == \"system\" then .content = \"важно: \" + .content else . end)", chat)
		msgs := got.(map[string]any)["messages"].([]any)
		if msgs[0].(map[string]any)["content"] != "важно: sys prompt" {
			t.Errorf("префикс не добавлен: %v", msgs[0])
		}
	})
}
