package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kartashoffvbif-blip/transmutator/internal/proxy"
	"github.com/kartashoffvbif-blip/transmutator/internal/storage"
	"github.com/kartashoffvbif-blip/transmutator/internal/testhelpers"
	"github.com/kartashoffvbif-blip/transmutator/internal/transform"
)

// newService поднимает сервис с данным манифестом правил и тестовым апстримом.
func newService(t *testing.T, rulesManifest string, files map[string]string, timeout time.Duration, upstream *testhelpers.Upstream) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rulesPath := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(rulesPath, []byte(rulesManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	ruleList, err := storage.NewFileStore(rulesPath).Load()
	if err != nil {
		t.Fatalf("правила не загрузились: %v", err)
	}
	engine, err := transform.New(ruleList, timeout)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(upstream.URL() + "/v1/chat/completions")
	h := &Handler{
		Proxy:  proxy.New(u),
		Engine: engine,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Debug:  false,
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(upstream.Close)
	return srv
}

func post(t *testing.T, srv *httptest.Server, auth, body string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest("POST", srv.URL+TargetPath, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(raw)
}

func decode(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("ответ не JSON: %q", body)
	}
	return m
}

func logOf(t *testing.T, body []byte) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("тело апстрима не JSON: %q", body)
	}
	raw, _ := m["log"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, _ := v.(string)
		out = append(out, s)
	}
	return out
}

// chatBody — валидный запрос с n блоками вопрос-ответ и неизвестным полем.
func chatBody(nBlocks int) string {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msgs := []msg{{Role: "system", Content: "sys"}}
	for i := 0; i < nBlocks; i++ {
		msgs = append(msgs, msg{Role: "user", Content: "q"}, msg{Role: "assistant", Content: "a"})
	}
	b, _ := json.Marshal(map[string]any{"model": "gpt-4o-mini", "messages": msgs, "custom_field": "keep-me"})
	return string(b)
}

func TestNonTargetRequestPassedThrough(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	srv := newService(t, "rules:\n  - key: \"k\"\n    filter_inline: \".\"\n", nil, time.Second, up)

	req, _ := http.NewRequest("GET", srv.URL+"/v1/models?x=1", nil)
	req.Header.Set("Authorization", "Bearer k")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	last := up.Last()
	if last == nil || last.Path != "/v1/models" || last.Method != "GET" {
		t.Fatalf("нецелевой запрос не проброшен: %+v", last)
	}
	if last.Header.Get("Authorization") != "Bearer k" {
		t.Error("Authorization не проброшен")
	}
}

func TestTargetRequestNoRulesApplied(t *testing.T) {
	// правило есть ровно одно, для ключа sk-test и модели gpt-4o-mini;
	// все варианты ниже не должны приводить к его применению
	tests := []struct {
		name string
		auth string
	}{
		{"нет заголовка Authorization", ""},
		{"не Bearer", "Basic dXNlcjpwYXNz"},
		{"пустой токен", "Bearer "},
		{"Bearer без токена", "Bearer"},
		{"неизвестный ключ", "Bearer sk-unknown"},
		{"схема в нижнем регистре, ключ неизвестен", "bearer sk-nope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := testhelpers.NewUpstream(nil)
			srv := newService(t,
				"rules:\n  - key: \"sk-test\"\n    model: \"gpt-4o-mini\"\n    filter_inline: '.tag = \"changed\"'\n",
				nil, time.Second, up)

			body := chatBody(2)
			_, respBody := post(t, srv, tt.auth, body)
			last := up.Last()
			if last == nil {
				t.Fatal("запрос не проброшен к апстриму")
			}
			if string(last.Body) != body {
				t.Errorf("тело изменено: %s", last.Body)
			}
			if !strings.Contains(respBody, `"ok":true`) {
				t.Errorf("ответ апстрима не возвращён: %s", respBody)
			}
		})
	}
}

func TestTargetInvalidJSONPassedThrough(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	srv := newService(t, "rules:\n  - key: \"k\"\n    filter_inline: \".tag = 1\"\n", nil, time.Second, up)

	broken := `{"model": "gpt", `
	_, respBody := post(t, srv, "Bearer k", broken)
	last := up.Last()
	if last == nil || string(last.Body) != broken {
		t.Fatalf("битый JSON должен уйти апстриму как есть: %q", last)
	}
	if !strings.Contains(respBody, `"ok":true`) {
		t.Errorf("ответ апстрима не возвращён: %s", respBody)
	}
}

func TestBearerSchemeCaseInsensitive(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	srv := newService(t, "rules:\n  - key: \"sk-test\"\n    filter_inline: '.tag = \"changed\"'\n", nil, time.Second, up)

	_, _ = post(t, srv, "BEARER sk-test", chatBody(1))
	last := up.Last()
	got := decode(t, string(last.Body))
	if got["tag"] != "changed" {
		t.Errorf("правило не применилось при схеме BEARER: %s", last.Body)
	}
}

func TestSelectionPriorityAndStableOrder(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	// порядок в манифесте намеренно перемешан относительно приоритетов
	manifest := `
rules:
  - key: "sk-test"
    model: "gpt-4o-mini"
    priority: 20
    filter_inline: '.log += ["model-p20"]'
  - key: "sk-test"
    priority: 5
    filter_inline: '.log += ["key-p5-first"]'
  - key: "sk-test"
    priority: 5
    filter_inline: '.log += ["key-p5-second"]'
  - key: "sk-test"
    model: "GPT-4O-MINI"
    priority: 0
    filter_inline: '.log += ["other-case"]'
  - key: "sk-other"
    filter_inline: '.log += ["wrong-key"]'
`
	srv := newService(t, manifest, nil, time.Second, up)

	t.Run("точное совпадение ключ+модель — только они", func(t *testing.T) {
		_, _ = post(t, srv, "Bearer sk-test", chatBody(1))
		if got := logOf(t, up.Last().Body); len(got) != 1 || got[0] != "model-p20" {
			t.Errorf("ожидалось только правило ключ+модель, применено: %v", got)
		}
	})

	t.Run("модель в другом регистре — правила только по ключу, по приоритету", func(t *testing.T) {
		// модель запроса не совпадает ни с одним правилом «ключ+модель»
		// (совпадение чувствительно к регистру), поэтому применяются
		// правила только по ключу
		body := strings.Replace(chatBody(1), "gpt-4o-mini", "GPT-4o-mini", 1)
		_, _ = post(t, srv, "Bearer sk-test", body)
		if got := logOf(t, up.Last().Body); len(got) != 2 || got[0] != "key-p5-first" || got[1] != "key-p5-second" {
			t.Errorf("ожидался стабильный порядок p5-first, p5-second; применено: %v", got)
		}
	})

	t.Run("без поля model — правила только по ключу", func(t *testing.T) {
		var m map[string]any
		_ = json.Unmarshal([]byte(chatBody(1)), &m)
		delete(m, "model")
		b, _ := json.Marshal(m)
		_, _ = post(t, srv, "Bearer sk-test", string(b))
		if got := logOf(t, up.Last().Body); len(got) != 2 {
			t.Errorf("ожидались 2 правила по ключу, применено: %v", got)
		}
	})
}

func TestRuleChangesModelNoReselection(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	// правило ключ+модель меняет model на m2; правило только по ключу
	// не должно подхватиться после этого
	manifest := `
rules:
  - key: "sk-test"
    model: "m1"
    priority: 0
    filter_inline: '.model = "m2" | .log += ["model-rule"]'
  - key: "sk-test"
    priority: 10
    filter_inline: '.log += ["key-rule"]'
`
	srv := newService(t, manifest, nil, time.Second, up)

	body := strings.Replace(chatBody(1), "gpt-4o-mini", "m1", 1)
	_, _ = post(t, srv, "Bearer sk-test", body)
	got := decode(t, string(up.Last().Body))
	if got["model"] != "m2" {
		t.Errorf("model = %v, ожидалось m2", got["model"])
	}
	log := logOf(t, up.Last().Body)
	if len(log) != 1 || log[0] != "model-rule" {
		t.Errorf("повторный выбор правил не должен выполняться, применено: %v", log)
	}
}

func TestVarsPassedToFilter(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := `
rules:
  - key: "sk-test"
    filter_inline: |
      .messages = (.messages | safe_truncate_blocks($keep_last))
      | .word = $word
    vars:
      keep_last: 1
      word: "SECRET"
`
	srv := newService(t, manifest, nil, time.Second, up)
	_, _ = post(t, srv, "Bearer sk-test", chatBody(3))
	last := up.Last()
	got := decode(t, string(last.Body))
	if got["word"] != "SECRET" {
		t.Errorf("строковая переменная не передана: %v", got["word"])
	}
	msgs := got["messages"].([]any)
	// system + один блок вопрос-ответ
	if len(msgs) != 3 {
		t.Errorf("safe_truncate_blocks($keep_last=1) дал %d сообщений, ожидалось 3: %v", len(msgs), msgs)
	}
}

func TestFilterFileUsed(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_file: \"filters/mark.jq\"\n"
	files := map[string]string{"filters/mark.jq": `.from_file = true`}
	srv := newService(t, manifest, files, time.Second, up)

	_, _ = post(t, srv, "Bearer sk-test", chatBody(1))
	if decode(t, string(up.Last().Body))["from_file"] != true {
		t.Error("фильтр из внешнего файла не применился")
	}
}

func TestUnknownFieldsAndMultimodalPreserved(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: '.messages = (.messages | safe_truncate_blocks(1))'\n"
	srv := newService(t, manifest, nil, time.Second, up)

	body := `{
		"model": "gpt-4o-mini",
		"custom_top": {"a": [1,2]},
		"messages": [
			{"role": "system", "content": "sys", "custom_msg": 7},
			{"role": "user", "content": "первый вопрос"},
			{"role": "assistant", "content": "ок"},
			{"role": "user", "content": [
				{"type": "text", "text": "картинка?"},
				{"type": "image_url", "image_url": {"url": "data:image/png;base64,AAAA"}}
			]},
			{"role": "assistant", "content": "вот"}
		]
	}`
	_, _ = post(t, srv, "Bearer sk-test", body)
	got := decode(t, string(up.Last().Body))
	if got["custom_top"] == nil {
		t.Error("неизвестное верхнеуровневое поле потеряно")
	}
	msgs := got["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("ожидался system + 1 блок, получено %d", len(msgs))
	}
	first := msgs[1].(map[string]any)
	parts := first["content"].([]any)
	if len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
		t.Errorf("мультимодальный контент изменён: %v", parts)
	}
	if msgs[0].(map[string]any)["custom_msg"] != 7.0 {
		t.Error("неизвестное поле сообщения потеряно")
	}
}

func TestRuleFailuresNotForwarded(t *testing.T) {
	tests := []struct {
		name       string
		filter     string
		wantStatus int
		wantCode   string
	}{
		{"несколько результатов", ". , .", 500, "rule_execution_failed"},
		{"ноль результатов", "empty", 500, "rule_execution_failed"},
		{"ошибка фильтра", `error("boom")`, 500, "rule_execution_failed"},
		{"не объект", ".messages", 400, "invalid_transformed_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := testhelpers.NewUpstream(nil)
			manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: |\n      " + tt.filter + "\n"
			srv := newService(t, manifest, nil, time.Second, up)

			resp, body := post(t, srv, "Bearer sk-test", chatBody(1))
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("статус = %d, ожидался %d; тело: %s", resp.StatusCode, tt.wantStatus, body)
			}
			errObj := decode(t, body)["error"].(map[string]any)
			if errObj["code"] != tt.wantCode {
				t.Errorf("code = %v, ожидался %q", errObj["code"], tt.wantCode)
			}
			if errObj["message"] == "" {
				t.Error("message пуст")
			}
			if up.Last() != nil {
				t.Error("запрос не должен форвордиться при ошибке правила")
			}
		})
	}
}

func TestRuleTimeoutNotForwarded(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: |\n      reduce range(0; 10000000000) as $i (0; . + $i)\n"
	srv := newService(t, manifest, nil, 50*time.Millisecond, up)

	resp, body := post(t, srv, "Bearer sk-test", chatBody(1))
	if resp.StatusCode != 500 {
		t.Fatalf("статус = %d, ожидался 500; тело: %s", resp.StatusCode, body)
	}
	errObj := decode(t, body)["error"].(map[string]any)
	if errObj["code"] != "rule_timeout" {
		t.Errorf("code = %v, ожидался rule_timeout", errObj["code"])
	}
	if up.Last() != nil {
		t.Error("запрос не должен форвордиться при таймауте")
	}
}

func TestValidationFailuresNotForwarded(t *testing.T) {
	tests := []struct {
		name   string
		filter string
	}{
		{"нет ни одного user", `.messages = [{"role":"assistant","content":"a"}]`},
		{"tool без tool_call", `.messages = [{"role":"user","content":"u"},{"role":"tool","tool_call_id":"nope","content":"r"}]`},
		{"tool_calls без ответа", `.messages = [{"role":"user","content":"u"},{"role":"assistant","tool_calls":[{"id":"c1","type":"function"}]}]`},
		{"assistant до user", `.messages = [{"role":"assistant","content":"a"},{"role":"user","content":"u"}]`},
		{"пустой messages", `.messages = []`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			up := testhelpers.NewUpstream(nil)
			manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: |\n      " + tt.filter + "\n"
			srv := newService(t, manifest, nil, time.Second, up)

			resp, body := post(t, srv, "Bearer sk-test", chatBody(1))
			if resp.StatusCode != 400 {
				t.Fatalf("статус = %d, ожидался 400; тело: %s", resp.StatusCode, body)
			}
			errObj := decode(t, body)["error"].(map[string]any)
			if errObj["code"] != "invalid_transformed_request" {
				t.Errorf("code = %v", errObj["code"])
			}
			if errObj["message"] == "" {
				t.Error("message пуст")
			}
			if up.Last() != nil {
				t.Error("запрос не должен форвордиться при ошибке валидации")
			}
		})
	}
}

func TestValidToolLoopPasses(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: '.messages = (.messages | safe_truncate_blocks(1))'\n"
	srv := newService(t, manifest, nil, time.Second, up)

	body := `{
		"model": "m",
		"messages": [
			{"role": "user", "content": "погода?"},
			{"role": "assistant", "tool_calls": [{"id": "c1", "type": "function", "function": {"name": "w"}}]},
			{"role": "tool", "tool_call_id": "c1", "content": "+5"},
			{"role": "user", "content": "спасибо"}
		]
	}`
	resp, bodyOut := post(t, srv, "Bearer sk-test", body)
	if resp.StatusCode != 200 {
		t.Fatalf("валидный tool-цикл не прошёл: %d %s", resp.StatusCode, bodyOut)
	}
}

func TestAuthorizationForwardedToUpstream(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	srv := newService(t, "rules:\n  - key: \"sk-test\"\n    filter_inline: \".\"\n", nil, time.Second, up)

	_, _ = post(t, srv, "Bearer sk-test", chatBody(1))
	if got := up.Last().Header.Get("Authorization"); got != "Bearer sk-test" {
		t.Errorf("Authorization до апстрима = %q", got)
	}
}

func TestQueryParamsPreservedOnTargetForward(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	srv := newService(t, "rules:\n  - key: \"sk-test\"\n    filter_inline: \".\"\n", nil, time.Second, up)

	req, _ := http.NewRequest("POST", srv.URL+TargetPath+"?stream=true&beta=x", strings.NewReader(chatBody(1)))
	req.Header.Set("Authorization", "Bearer sk-test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	last := up.Last()
	if last.Path != "/v1/chat/completions" {
		t.Errorf("целевой запрос уходит не на upstream_url: %q", last.Path)
	}
	if last.Query.Get("stream") != "true" || last.Query.Get("beta") != "x" {
		t.Errorf("query-параметры потеряны: %v", last.Query)
	}
}

func TestUpstreamErrorPassedThrough(t *testing.T) {
	up := testhelpers.NewUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	})
	srv := newService(t, "rules: []\n", nil, time.Second, up)

	resp, body := post(t, srv, "Bearer k", chatBody(1))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("статус апстрима изменён: %d", resp.StatusCode)
	}
	if !strings.Contains(body, "rate limited") {
		t.Errorf("тело ошибки апстрима изменено: %s", body)
	}
}

func TestUpstreamUnreachable(t *testing.T) {
	dead := testhelpers.NewUpstream(nil)
	deadURL := dead.URL()
	dead.Close()

	// прямой прогон обработчика с мёртвым апстримом
	u, _ := url.Parse(deadURL + "/v1/chat/completions")
	h := &Handler{
		Proxy:  proxy.New(u),
		Engine: engineOrDie(t, "rules: []\n", time.Second),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, body := post(t, srv, "Bearer k", chatBody(1))
	if resp.StatusCode != 502 {
		t.Fatalf("статус = %d, ожидался 502; тело: %s", resp.StatusCode, body)
	}
	errObj := decode(t, body)["error"].(map[string]any)
	if errObj["code"] != "upstream_unreachable" {
		t.Errorf("code = %v", errObj["code"])
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestStreamingResponseProxied(t *testing.T) {
	release := make(chan struct{})
	up := testhelpers.NewUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		_, _ = w.Write([]byte("data: one\n\n"))
		_ = rc.Flush()
		<-release
		_, _ = w.Write([]byte("data: two\n\n"))
		_ = rc.Flush()
	})
	srv := newService(t, "rules: []\n", nil, time.Second, up)

	req, _ := http.NewRequest("POST", srv.URL+TargetPath, strings.NewReader(chatBody(1)))
	req.Header.Set("Authorization", "Bearer k")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// первый чанк должен прийти до закрытия апстрима
	head := make([]byte, len("data: one"))
	if _, err := io.ReadFull(resp.Body, head); err != nil {
		t.Fatalf("первый чанк не получен до конца стрима: %v", err)
	}
	if string(head) != "data: one" {
		t.Errorf("первый чанк = %q", head)
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(rest, []byte("data: two")) {
		t.Errorf("хвост стрима потерян: %q", rest)
	}
}

// engineOrDie собирает движок из манифеста для тестов без временных файлов правил.
func engineOrDie(t *testing.T, manifest string, timeout time.Duration) *transform.Engine {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	ruleList, err := storage.NewFileStore(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	e, err := transform.New(ruleList, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// newLoggedService — сервис с перехватом логов для проверки режимов логирования.
func newLoggedService(t *testing.T, level slog.Level, debug bool, manifest string, up *testhelpers.Upstream) (*httptest.Server, *lockedBuffer) {
	t.Helper()
	var buf lockedBuffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))
	u, _ := url.Parse(up.URL() + "/v1/chat/completions")
	h := &Handler{
		Proxy:  proxy.New(u),
		Engine: engineOrDie(t, manifest, time.Second),
		Logger: logger,
		Debug:  debug,
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	t.Cleanup(up.Close)
	return srv, &buf
}

func TestDebugLoggingShowsOriginalTransformedAndDiff(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: '.messages = (.messages | safe_truncate_blocks(1))'\n"
	srv, buf := newLoggedService(t, slog.LevelDebug, true, manifest, up)

	_, _ = post(t, srv, "Bearer sk-test", chatBody(3))
	logs := buf.String()
	for _, want := range []string{
		"original_body",    // исходный запрос
		"transformed_body", // преобразованный запрос
		"diff",             // разница
		"rules",            // выбранная цепочка правил
		"original_bytes",   // метрики до/после
		"transformed_bytes",
		"original_chars",
		"transformed_chars",
		"- ", // в diff есть удалённые строки
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("в debug-логах нет %q; лог: %s", want, logs)
		}
	}
}

func TestNormalModeLogsOnlyErrors(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	manifest := "rules:\n  - key: \"sk-test\"\n    filter_inline: '.messages = (.messages | safe_truncate_blocks(1))'\n"
	srv, buf := newLoggedService(t, slog.LevelError, false, manifest, up)

	// успешное преобразование — в обычном режиме логов быть не должно
	_, _ = post(t, srv, "Bearer sk-test", chatBody(2))
	if got := buf.String(); got != "" {
		t.Errorf("обычный режим не должен логировать успешные запросы: %s", got)
	}

	// ошибка правила логируется на уровне Error
	broken := strings.ReplaceAll(manifest, "safe_truncate_blocks(1)", "error(\"boom\")")
	up2 := testhelpers.NewUpstream(nil)
	srv2, buf2 := newLoggedService(t, slog.LevelError, false, broken, up2)
	resp, body := post(t, srv2, "Bearer sk-test", chatBody(1))
	if resp.StatusCode != 500 {
		t.Fatalf("статус = %d; тело: %s", resp.StatusCode, body)
	}
	if !strings.Contains(buf2.String(), "ошибка выполнения правила") {
		t.Errorf("ошибка правила не залогирована: %s", buf2.String())
	}
}

// lockedBuffer — потокобезопасный буфер для сбора логов.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
