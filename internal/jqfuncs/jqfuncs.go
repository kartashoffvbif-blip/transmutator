// Package jqfuncs — кастомные функции, доступные во всех jq-фильтрах правил:
//
//	approx_tokens            — приблизительный вес текста в «попугаях»;
//	count_chars              — количество символов (ведёт себя как approx_tokens);
//	safe_truncate_blocks(N)  — безопасная обрезка истории по блокам вопрос-ответ;
//	validate_openai_request  — минимальная валидация объекта запроса.
//
// Все функции детерминированы, локальны и не имеют побочных эффектов.
package jqfuncs

import (
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf8"

	"github.com/itchyny/gojq"

	"github.com/kartashoffvbif-blip/transmutator/internal/validation"
)

// Options возвращает опции компиляции gojq со всеми кастомными функциями.
func Options() []gojq.CompilerOption {
	return []gojq.CompilerOption{
		gojq.WithFunction("approx_tokens", 0, 0, func(v any, _ []any) any {
			return textWeight(v)
		}),
		gojq.WithFunction("count_chars", 0, 0, func(v any, _ []any) any {
			return textWeight(v)
		}),
		gojq.WithFunction("safe_truncate_blocks", 1, 1, safeTruncateBlocks),
		gojq.WithFunction("validate_openai_request", 0, 0, func(v any, _ []any) any {
			if err := validation.Validate(v); err != nil {
				return fmt.Errorf("validate_openai_request: %w", err)
			}
			return v
		}),
	}
}

// textWeight — приблизительный вес значения в символах (рунах):
//   - строка          — длина в рунах;
//   - массив          — сумма весов элементов;
//   - объект          — текст из полей "text" и "content"
//     (content может быть строкой или массивом частей,
//     текстовые части считаются, изображения и бинарные — нет);
//   - числа, булевы и null — 0.
func textWeight(v any) int {
	switch t := v.(type) {
	case string:
		return utf8.RuneCountInString(t)
	case []any:
		n := 0
		for _, el := range t {
			n += textWeight(el)
		}
		return n
	case map[string]any:
		n := 0
		if s, ok := t["text"].(string); ok {
			n += utf8.RuneCountInString(s)
		}
		switch c := t["content"].(type) {
		case string:
			n += utf8.RuneCountInString(c)
		case []any:
			n += textWeight(c)
		}
		return n
	default:
		return 0
	}
}

// safeTruncateBlocks реализует jq-функцию safe_truncate_blocks(N):
// оставляет префикс до первого сообщения с ролью user (обычно системные
// сообщения) и последние N блоков «вопрос-ответ». Блок начинается с
// сообщения user и включает все следующие assistant/tool до следующего user.
// Пары вопрос-ответ никогда не разрываются, сообщения не изменяются.
func safeTruncateBlocks(v any, args []any) any {
	msgs, ok := v.([]any)
	if !ok {
		return fmt.Errorf("safe_truncate_blocks: вход должен быть массивом сообщений")
	}
	n, ok := toInt(args[0])
	if !ok || n < 0 {
		return fmt.Errorf("safe_truncate_blocks: аргумент должен быть целым числом >= 0")
	}
	first := len(msgs)
	for i, m := range msgs {
		if role, _ := m.(map[string]any)["role"].(string); role == "user" {
			first = i
			break
		}
	}
	prefix := msgs[:first]
	rest := msgs[first:]

	// разбиение остатка на блоки, начинающиеся с user
	var blocks [][]any
	var cur []any
	for _, m := range rest {
		if role, _ := m.(map[string]any)["role"].(string); role == "user" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
			}
			cur = nil
		}
		cur = append(cur, m)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	if n > len(blocks) {
		n = len(blocks)
	}
	kept := blocks[len(blocks)-n:]

	out := make([]any, 0, len(prefix)+len(rest))
	out = append(out, prefix...)
	for _, b := range kept {
		out = append(out, b...)
	}
	return out
}

// toInt приводит число gojq (int, float64 с целым значением или json.Number)
// к int. Значения вроде 4.5 или строки числом не считаются.
func toInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case float64:
		if t == math.Trunc(t) && t >= math.MinInt32 && t <= math.MaxInt32 {
			return int(t), true
		}
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}
