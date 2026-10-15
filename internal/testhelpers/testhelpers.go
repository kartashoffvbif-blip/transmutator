// Package testhelpers — вспомогательные утилиты для тестов:
// записывающий тестовый апстрим и конструкторы тел запросов.
package testhelpers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
)

// RecordedRequest — снимок запроса, дошедшего до тестового апстрима.
type RecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

// Upstream — тестовый апстрим, запоминающий все пришедшие запросы.
type Upstream struct {
	Server *httptest.Server

	mu       sync.Mutex
	requests []RecordedRequest
}

// NewUpstream поднимает тестовый апстрим. При h == nil апстрим отвечает
// 200 {"ok":true}.
func NewUpstream(h http.HandlerFunc) *Upstream {
	if h == nil {
		h = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}
	u := &Upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
		}
		u.mu.Lock()
		u.requests = append(u.requests, RecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.Query(),
			Header: r.Header.Clone(),
			Body:   body,
		})
		u.mu.Unlock()
		h(w, r)
	}))
	return u
}

// Requests возвращает копию всех записанных запросов.
func (u *Upstream) Requests() []RecordedRequest {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]RecordedRequest, len(u.requests))
	copy(out, u.requests)
	return out
}

// Last возвращает последний запрос; если запросов не было — nil.
func (u *Upstream) Last() *RecordedRequest {
	rs := u.Requests()
	if len(rs) == 0 {
		return nil
	}
	return &rs[len(rs)-1]
}

// Close закрывает тестовый сервер.
func (u *Upstream) Close() { u.Server.Close() }

// URL возвращает адрес тестового апстрима.
func (u *Upstream) URL() string { return u.Server.URL }
