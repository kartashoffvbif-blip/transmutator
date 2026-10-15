package validation

import (
	"encoding/json"
	"testing"
)

// fromJSON разбирает JSON в значения с обычными типами Go
// (как после transform.ParseJSON + NormalizeValue).
func fromJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func validChat(t *testing.T) any {
	return fromJSON(t, `{
		"model": "gpt-4o-mini",
		"messages": [
			{"role": "system", "content": "ты помощник"},
			{"role": "user", "content": "привет"},
			{"role": "assistant", "content": "здравствуй"},
			{"role": "user", "content": "как дела"}
		]
	}`)
}

func TestValidateValid(t *testing.T) {
	tests := map[string]any{
		"обычный чат": validChat(t),
		"только user": fromJSON(t, `{"messages":[{"role":"user","content":"hi"}]}`),
		"с tool-циклом": fromJSON(t, `{"messages":[
			{"role":"user","content":"погода?"},
			{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"w"}}]},
			{"role":"tool","tool_call_id":"c1","content":"+5"},
			{"role":"assistant","content":"+5 градусов"}
		]}`),
		"неизвестные поля сверху":  fromJSON(t, `{"messages":[{"role":"user","content":"hi"}],"custom":123,"meta":{"a":[1]}}`),
		"сообщение с extra-полями": fromJSON(t, `{"messages":[{"role":"user","content":"hi","name":"x","unknown":true}]}`),
	}
	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Validate(v); err != nil {
				t.Errorf("ожидал успех, получена ошибка: %v", err)
			}
		})
	}
}

func TestValidateInvalid(t *testing.T) {
	tests := map[string]any{
		"корень не объект":      fromJSON(t, `[1,2]`),
		"нет messages":          fromJSON(t, `{"model":"m"}`),
		"messages не массив":    fromJSON(t, `{"messages":"нет"}`),
		"messages пуст":         fromJSON(t, `{"messages":[]}`),
		"нет user-сообщения":    fromJSON(t, `{"messages":[{"role":"system","content":"s"}]}`),
		"assistant до user":     fromJSON(t, `{"messages":[{"role":"assistant","content":"a"},{"role":"user","content":"u"}]}`),
		"сообщение не объект":   fromJSON(t, `{"messages":["str"]}`),
		"нет роли":              fromJSON(t, `{"messages":[{"content":"u"}]}`),
		"роль не строка":        fromJSON(t, `{"messages":[{"role":42}]}`),
		"tool без tool_call_id": fromJSON(t, `{"messages":[{"role":"user","content":"u"},{"role":"tool","content":"r"}]}`),
		"tool с неизвестным id": fromJSON(t, `{"messages":[{"role":"user","content":"u"},{"role":"tool","tool_call_id":"нет","content":"r"}]}`),
		"tool_calls без ответа": fromJSON(t, `{"messages":[{"role":"user","content":"u"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function"}]}]}`),
		"tool_call без id":      fromJSON(t, `{"messages":[{"role":"user","content":"u"},{"role":"assistant","tool_calls":[{"type":"function"}]}]}`),
	}
	for name, v := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Validate(v); err == nil {
				t.Errorf("ожидал ошибку, получен успех")
			}
		})
	}
}

func TestValidateToolOrderMatters(t *testing.T) {
	// tool-ответ раньше объявления вызова — невалидно
	v := fromJSON(t, `{"messages":[
		{"role":"user","content":"u"},
		{"role":"tool","tool_call_id":"c1","content":"r"},
		{"role":"assistant","tool_calls":[{"id":"c1","type":"function"}]}
	]}`)
	if err := Validate(v); err == nil {
		t.Error("tool до assistant с tool_calls должен быть ошибкой")
	}
}

func TestValidateErrorMessageReadable(t *testing.T) {
	err := Validate(fromJSON(t, `{"messages":[]}`))
	if err == nil || err.Error() == "" {
		t.Fatal("ожидал ошибку с текстом")
	}
}
