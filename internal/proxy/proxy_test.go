package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kartashoffvbif-blip/transmutator/internal/testhelpers"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestPassThroughPreservesRequest(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	defer up.Close()
	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.PassThrough(w, r)
	}))
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/v1/models?limit=5&x=1", strings.NewReader(`{"a":1}`))
	req.Header.Set("Authorization", "Bearer sk-123")
	req.Header.Set("X-Custom", "custom-value")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	last := up.Last()
	if last == nil {
		t.Fatal("запрос не дошёл до апстрима")
	}
	// путь и query — исходные, а не от upstream_url
	if last.Path != "/v1/models" {
		t.Errorf("путь = %q, ожидался /v1/models", last.Path)
	}
	if last.Query.Get("limit") != "5" || last.Query.Get("x") != "1" {
		t.Errorf("query потеряны: %v", last.Query)
	}
	if last.Method != "DELETE" {
		t.Errorf("метод = %q", last.Method)
	}
	if last.Header.Get("Authorization") != "Bearer sk-123" {
		t.Errorf("Authorization не проброшен: %q", last.Header.Get("Authorization"))
	}
	if last.Header.Get("X-Custom") != "custom-value" {
		t.Errorf("X-Custom не проброшен")
	}
	if string(last.Body) != `{"a":1}` {
		t.Errorf("тело изменено: %q", last.Body)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("ответ апстрима не возвращён: %q", body)
	}
}

func TestForwardBodyReplacesBodyAndKeepsHeaders(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	defer up.Close()
	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.ForwardBody(w, r, []byte(`{"transformed":true}`))
	}))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/chat/completions?api-version=2024", strings.NewReader(`{"original":true}`))
	req.Header.Set("Authorization", "Bearer sk-keep-me")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	last := up.Last()
	// целевой запрос уходит на полный upstream_url
	if last.Path != "/v1/chat/completions" {
		t.Errorf("путь = %q", last.Path)
	}
	if last.Query.Get("api-version") != "2024" {
		t.Errorf("query-параметры не сохранены: %v", last.Query)
	}
	if string(last.Body) != `{"transformed":true}` {
		t.Errorf("тело = %q", last.Body)
	}
	if last.Header.Get("Authorization") != "Bearer sk-keep-me" {
		t.Errorf("Authorization не проброшен")
	}
	if last.Header.Get("Content-Length") == "" {
		t.Error("Content-Length должен быть пересчитан")
	}
}

func TestUpstreamUnreachableReturns502(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	urlStr := up.URL()
	up.Close() // сразу гасим апстрим

	p := New(mustURL(t, urlStr+"/v1/chat/completions"))
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "http://local/v1/chat/completions", strings.NewReader(`{}`))
	p.ForwardBody(rec, req, []byte(`{}`))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("статус = %d, ожидался 502", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"upstream_unreachable"`) {
		t.Errorf("тело ошибки не в формате OpenAI: %s", rec.Body.String())
	}
}

func TestUpstreamErrorStatusPassedThrough(t *testing.T) {
	up := testhelpers.NewUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"upstream broke"}}`))
	})
	defer up.Close()

	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))
	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "http://local/v1/chat/completions", strings.NewReader(`{}`))
	p.ForwardBody(rec, req, []byte(`{}`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("статус = %d, ожидался 500", rec.Code)
	}
	if rec.Header().Get("X-Upstream") != "yes" {
		t.Error("заголовки апстрима не проброшены")
	}
	if !strings.Contains(rec.Body.String(), "upstream broke") {
		t.Errorf("тело ошибки апстрима изменено: %s", rec.Body.String())
	}
}

func TestSSEStreaming(t *testing.T) {
	// апстрим отдаёт SSE-события с задержками; прокси должен отдавать их
	// клиенту по мере появления, не буферизуя весь ответ
	release := make(chan struct{})
	up := testhelpers.NewUpstream(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		_, _ = w.Write([]byte("data: chunk1\n\n"))
		_ = rc.Flush()
		<-release // держим ответ открытым
		_, _ = w.Write([]byte("data: chunk2\n\n"))
		_ = rc.Flush()
	})
	defer up.Close()

	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest("POST", "http://local/x", nil)
		p.ForwardBody(w, req, []byte(`{}`))
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q", got)
	}

	buf := make([]byte, 64)
	n, err := io.ReadAtLeast(resp.Body, buf[:len("data: chunk1")], len("data: chunk1"))
	if err != nil {
		t.Fatalf("первый чанк не получен до закрытия стрима: %v", err)
	}
	if string(buf[:n]) != "data: chunk1" {
		t.Errorf("первый чанк = %q", buf[:n])
	}
	close(release)
	rest, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(rest), "chunk2") {
		t.Errorf("второй чанк не получен: %q", rest)
	}
}

func TestHopHeadersStripped(t *testing.T) {
	up := testhelpers.NewUpstream(nil)
	defer up.Close()

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "http://local/x", strings.NewReader(`{}`))
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Keep-Alive", "timeout=5")
	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))
	p.ForwardBody(rec, req, []byte(`{}`))

	last := up.Last()
	for _, h := range []string{"Connection", "Upgrade", "Keep-Alive"} {
		if last.Header.Get(h) != "" {
			t.Errorf("hop-заголовок %s переслан апстриму", h)
		}
	}
}

func TestClientDisconnectDoesNotWrite502(t *testing.T) {
	// клиент ушёл до ответа — 502 писать некому, не должно быть паники
	up := testhelpers.NewUpstream(nil)
	defer up.Close()
	p := New(mustURL(t, up.URL()+"/v1/chat/completions"))

	rec := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "http://local/x", nil)
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	cancel()
	p.ForwardBody(rec, req, []byte(`{}`))
	// главное — не паника; запись в отменённый ответ просто игнорируется
}
