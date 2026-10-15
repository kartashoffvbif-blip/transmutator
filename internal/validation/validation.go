// Package validation — минимальная валидация результата преобразования.
// Работает над значениями, приведёнными к типам gojq/JSON
// (map[string]any, []any, string, int, float64, bool, nil).
package validation

import (
	"fmt"
	"sort"
	"strings"
)

// Validate проверяет объект запроса по минимальному набору правил:
//   - корень — объект;
//   - messages — непустой массив;
//   - каждое сообщение — объект со строковой ролью;
//   - есть хотя бы одно сообщение с ролью user;
//   - нет assistant до первого user;
//   - tool-сообщения ссылаются на ранее объявленные tool_calls;
//   - все tool_calls закрыты tool-ответами.
//
// Неизвестные поля игнорируются. Функция не изменяет вход.
func Validate(v any) error {
	root, ok := v.(map[string]any)
	if !ok {
		return fmt.Errorf("корень запроса должен быть объектом")
	}
	rawMsgs, ok := root["messages"]
	if !ok {
		return fmt.Errorf(`поле "messages" обязательно`)
	}
	msgs, ok := rawMsgs.([]any)
	if !ok {
		return fmt.Errorf(`поле "messages" должно быть массивом`)
	}
	if len(msgs) == 0 {
		return fmt.Errorf(`поле "messages" не должно быть пустым`)
	}

	// первый проход: структура сообщений и позиция первого user
	firstUser := -1
	for i, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			return fmt.Errorf("сообщение #%d должно быть объектом", i)
		}
		rawRole, has := msg["role"]
		if !has {
			return fmt.Errorf("у сообщения #%d отсутствует поле role", i)
		}
		role, ok := rawRole.(string)
		if !ok {
			return fmt.Errorf("роль сообщения #%d должна быть строкой", i)
		}
		if role == "user" && firstUser < 0 {
			firstUser = i
		}
	}
	if firstUser < 0 {
		return fmt.Errorf("должно быть хотя бы одно сообщение с ролью user")
	}
	// второй проход: семантика ролей и tool-вызовов
	declared := map[string]bool{} // tool_call_id, объявленные assistant-ами ранее
	pending := map[string]bool{}  // объявленные, но ещё не закрытые tool-ответом
	for i, m := range msgs {
		msg := m.(map[string]any)
		role, _ := msg["role"].(string)
		switch role {
		case "assistant":
			if i < firstUser {
				return fmt.Errorf("сообщение #%d с ролью assistant стоит до первого сообщения с рольой user", i)
			}
			rawCalls, has := msg["tool_calls"]
			if !has || rawCalls == nil {
				continue
			}
			calls, ok := rawCalls.([]any)
			if !ok {
				return fmt.Errorf(`поле "tool_calls" в сообщении #%d должно быть массивом`, i)
			}
			for _, c := range calls {
				call, ok := c.(map[string]any)
				if !ok {
					return fmt.Errorf("элемент tool_calls в сообщении #%d должен быть объектом", i)
				}
				id, ok := call["id"].(string)
				if !ok || id == "" {
					return fmt.Errorf("элемент tool_calls в сообщении #%d должен иметь строковый идентификатор id", i)
				}
				declared[id] = true
				pending[id] = true
			}
		case "tool":
			id, ok := msg["tool_call_id"].(string)
			if !ok || id == "" {
				return fmt.Errorf(`tool-сообщение #%d должно иметь строковое поле "tool_call_id"`, i)
			}
			if !declared[id] {
				return fmt.Errorf("tool-сообщение #%d ссылается на необъявленный ранее tool_call_id %q", i, id)
			}
			delete(pending, id)
		}
	}
	if len(pending) > 0 {
		ids := make([]string, 0, len(pending))
		for id := range pending {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return fmt.Errorf("незакрытые tool_calls без tool-ответа: %s", strings.Join(ids, ", "))
	}
	return nil
}
