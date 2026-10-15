package errs

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, RuleTimeout("правило #1"))

	if rec.Code != 500 {
		t.Errorf("статус = %d, ожидался 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("тело не JSON: %s", rec.Body.String())
	}
	if body.Error.Code != "rule_timeout" || body.Error.Type != "transformation_error" || body.Error.Message == "" {
		t.Errorf("неожиданное тело ошибки: %+v", body.Error)
	}
}

func TestPresetErrors(t *testing.T) {
	tests := []struct {
		e         *Error
		status    int
		typ, code string
	}{
		{BodyReadFailed("x"), 500, "invalid_request_error", "request_body_read_failed"},
		{RuleExecutionFailed("r", "c"), 500, "transformation_error", "rule_execution_failed"},
		{RuleTimeout("r"), 500, "transformation_error", "rule_timeout"},
		{InvalidTransformedRequest("r"), 400, "invalid_request_error", "invalid_transformed_request"},
		{UpstreamUnreachable("c"), 502, "upstream_error", "upstream_unreachable"},
	}
	for _, tt := range tests {
		if tt.e.Status != tt.status || tt.e.Type != tt.typ || tt.e.Code != tt.code {
			t.Errorf("%+v", tt.e)
		}
		if tt.e.Error() == "" {
			t.Error("Error() пуст")
		}
	}
}
