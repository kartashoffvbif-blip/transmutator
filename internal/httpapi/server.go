// Package httpapi — главный HTTP-обработчик сервиса: решает, является ли
// запрос целевым, применяет цепочку правил и пересылает запрос на апстрим.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kartashoffvbif-blip/transmutator/internal/errs"
	"github.com/kartashoffvbif-blip/transmutator/internal/logging"
	"github.com/kartashoffvbif-blip/transmutator/internal/proxy"
	"github.com/kartashoffvbif-blip/transmutator/internal/transform"
	"github.com/kartashoffvbif-blip/transmutator/internal/validation"
)

// TargetPath — путь целевого запроса, к которому применяются правила.
const TargetPath = "/v1/chat/completions"

// Handler — основной обработчик. Все зависимости внедряются извне.
type Handler struct {
	Proxy  *proxy.Proxy
	Engine *transform.Engine
	Logger *slog.Logger
	Debug  bool
}

// ServeHTTP маршрутизирует запрос: целевой (POST /v1/chat/completions)
// идёт через преобразование, всё остальное — прозрачный проброс.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost && r.URL.Path == TargetPath {
		h.handleTarget(w, r)
		return
	}
	h.Proxy.PassThrough(w, r)
}

func (h *Handler) handleTarget(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		h.Logger.Error("не удалось прочитать тело запроса", "err", err)
		errs.WriteJSON(w, errs.BodyReadFailed(err.Error()))
		return
	}

	// невалидный JSON пробрасывается апстриму без изменений
	if !json.Valid(raw) {
		h.debug("тело не является валидным JSON — проброс без изменений", "body", string(raw))
		h.Proxy.ForwardBody(w, r, raw)
		return
	}
	input, err := transform.ParseJSON(raw)
	if err != nil {
		// практически недостижимо после json.Valid
		h.debug("JSON не разобрался — проброс без изменений", "err", err.Error())
		h.Proxy.ForwardBody(w, r, raw)
		return
	}

	key := bearerKey(r)
	// поле model берётся только из исходного тела, до любых преобразований
	chain := h.Engine.Select(key, stringModel(input))
	if len(chain) == 0 {
		h.debug("правила не найдены — проброс без изменений",
			"key_known", key != "", "model", stringModel(input))
		h.Proxy.ForwardBody(w, r, raw)
		return
	}

	// последовательное применение правил; любое правило выполняется
	// с таймаутом, при ошибке запрос дальше не уходит
	current := input
	for _, cr := range chain {
		res, err := h.Engine.Apply(r.Context(), cr, current)
		if err != nil {
			if errors.Is(err, transform.ErrTimeout) {
				h.Logger.Error("таймаут выполнения правила", "rule", cr.Rule.Origin, "err", err)
				errs.WriteJSON(w, errs.RuleTimeout(cr.Rule.Origin))
			} else {
				h.Logger.Error("ошибка выполнения правила", "rule", cr.Rule.Origin, "err", err)
				errs.WriteJSON(w, errs.RuleExecutionFailed(cr.Rule.Origin, err.Error()))
			}
			return
		}
		current = res
	}

	// валидация результата: объект + минимальные проверки структуры
	if _, ok := current.(map[string]any); !ok {
		const msg = "результат преобразования не является объектом"
		h.Logger.Error(msg)
		errs.WriteJSON(w, errs.InvalidTransformedRequest(msg))
		return
	}
	if verr := validation.Validate(current); verr != nil {
		h.Logger.Error("результат не прошёл валидацию", "err", verr)
		errs.WriteJSON(w, errs.InvalidTransformedRequest(verr.Error()))
		return
	}

	out, err := json.Marshal(current)
	if err != nil {
		h.Logger.Error("не удалось сериализовать результат", "err", err)
		errs.WriteJSON(w, errs.RuleExecutionFailed("сериализация результата", err.Error()))
		return
	}
	h.debugTransform(chain, raw, out)
	h.Proxy.ForwardBody(w, r, out)
}

// debugTransform логирует в debug-режиме выбранную цепочку правил,
// исходный и преобразованный запросы, их разницу и метрики до/после.
func (h *Handler) debugTransform(chain []*transform.CompiledRule, original, transformed []byte) {
	if !h.Debug {
		return
	}
	names := make([]string, len(chain))
	for i, cr := range chain {
		names[i] = cr.Rule.Origin
	}
	h.Logger.Debug("преобразование выполнено",
		"rules", names,
		"original_body", string(original),
		"transformed_body", string(transformed),
		"diff", strings.Join(logging.Diff(prettyJSON(original), prettyJSON(transformed)), "\n"),
		"original_bytes", len(original),
		"transformed_bytes", len(transformed),
		"original_chars", utf8.RuneCount(original),
		"transformed_chars", utf8.RuneCount(transformed),
	)
}

func (h *Handler) debug(msg string, args ...any) {
	if h.Debug {
		h.Logger.Debug(msg, args...)
	}
}

// bearerKey извлекает ключ из "Authorization: Bearer <key>".
// Схема сравнивается без учёта регистра; пустой или не-Bearer заголовок
// означает неизвестный ключ ("").
func bearerKey(r *http.Request) string {
	const scheme = "bearer"
	s := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(s) <= len(scheme) {
		return ""
	}
	if !strings.EqualFold(s[:len(scheme)], scheme) {
		return ""
	}
	return strings.TrimSpace(s[len(scheme):])
}

// stringModel возвращает поле "model" из корня запроса, если это строка.
func stringModel(v any) string {
	if root, ok := v.(map[string]any); ok {
		if m, ok := root["model"].(string); ok {
			return m
		}
	}
	return ""
}

func prettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw) // не JSON — возвращаем как есть
	}
	return buf.String()
}
