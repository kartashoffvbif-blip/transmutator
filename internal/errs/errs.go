// Package errs — типизированные ошибки сервиса и их выдача клиенту
// в формате, похожем на ошибки OpenAI.
package errs

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Error — ошибка, которую можно отдать клиенту в стиле OpenAI.
type Error struct {
	Status  int    `json:"-"`       // HTTP-статус
	Type    string `json:"type"`    // например, invalid_request_error
	Code    string `json:"code"`    // например, rule_timeout
	Message string `json:"message"` // человекочитаемое описание
}

func (e *Error) Error() string { return e.Message }

// New создаёт ошибку с явными полями.
func New(status int, typ, code, message string) *Error {
	return &Error{Status: status, Type: typ, Code: code, Message: message}
}

// BodyReadFailed — тело целевого запроса не удалось прочитать.
func BodyReadFailed(cause string) *Error {
	return New(http.StatusInternalServerError, "invalid_request_error", "request_body_read_failed",
		fmt.Sprintf("не удалось прочитать тело запроса: %s", cause))
}

// RuleExecutionFailed — правило завершилось с ошибкой выполнения.
func RuleExecutionFailed(rule, cause string) *Error {
	return New(http.StatusInternalServerError, "transformation_error", "rule_execution_failed",
		fmt.Sprintf("правило %s завершилось с ошибкой: %s", rule, cause))
}

// RuleTimeout — превышен таймаут выполнения правила.
func RuleTimeout(rule string) *Error {
	return New(http.StatusInternalServerError, "transformation_error", "rule_timeout",
		fmt.Sprintf("превышен таймаут выполнения правила %s", rule))
}

// InvalidTransformedRequest — результат преобразования не прошёл валидацию.
func InvalidTransformedRequest(reason string) *Error {
	return New(http.StatusBadRequest, "invalid_request_error", "invalid_transformed_request",
		fmt.Sprintf("результат преобразования некорректен: %s", reason))
}

// UpstreamUnreachable — апстрим недоступен или сетевая ошибка.
func UpstreamUnreachable(cause string) *Error {
	return New(http.StatusBadGateway, "upstream_error", "upstream_unreachable",
		fmt.Sprintf("апстрим недоступен: %s", cause))
}

// WriteJSON отдаёт ошибку клиенту в виде {"error": {...}}.
func WriteJSON(w http.ResponseWriter, e *Error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": e})
}
