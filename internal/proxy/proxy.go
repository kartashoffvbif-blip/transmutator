// Package proxy — пересылка запросов на апстрим и стриминг ответов
// обратно клиенту без изменений.
package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kartashoffvbif-blip/transmutator/internal/errs"
)

// hopHeaders — заголовки уровня соединения, которые не пересылаются
// между клиентом и апстримом.
var hopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// Proxy пересылает запросы. Upstream — полный целевой URL для целевого
// запроса (POST /v1/chat/completions); для остальных запросов берутся
// только схема и хост апстрима.
type Proxy struct {
	Upstream *url.URL
	Client   *http.Client
}

// New создаёт прокси. Сжатие отключено, чтобы ответы апстрима
// возвращались клиенту байт-в-байт.
func New(upstream *url.URL) *Proxy {
	return &Proxy{
		Upstream: upstream,
		Client: &http.Client{
			Transport: &http.Transport{DisableCompression: true},
		},
	}
}

// PassThrough пересылает нецелевой запрос как есть: метод, путь, query,
// заголовки и тело — на схему и хост апстрима. Тело стримится без
// полного чтения.
func (p *Proxy) PassThrough(w http.ResponseWriter, r *http.Request) {
	target := *p.Upstream
	target.Path = r.URL.Path
	target.RawPath = r.URL.RawPath
	target.RawQuery = r.URL.RawQuery
	p.forward(w, r, &target, r.Body, r.ContentLength)
}

// ForwardBody отправляет целевой запрос на полный upstream_url с новым телом.
// Query-параметры исходного запроса сохраняются, исходный Authorization
// и остальные заголовки пересылаются как есть. body == nil означает
// «отправить исходное тело без изменений».
func (p *Proxy) ForwardBody(w http.ResponseWriter, r *http.Request, body []byte) {
	target := *p.Upstream
	if r.URL.RawQuery != "" {
		if target.RawQuery == "" {
			target.RawQuery = r.URL.RawQuery
		} else {
			target.RawQuery += "&" + r.URL.RawQuery
		}
	}
	var reader io.Reader
	var length int64
	if body != nil {
		reader = bytes.NewReader(body)
		length = int64(len(body))
	} else {
		reader = r.Body
		length = r.ContentLength
	}
	p.forward(w, r, &target, reader, length)
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, target *url.URL, body io.Reader, contentLength int64) {
	req := r.Clone(r.Context())
	req.URL = target
	req.Host = ""       // Host определяется по URL апстрима
	req.RequestURI = "" // обязательно сбросить для исходящего запроса
	req.TransferEncoding = nil
	req.Body = io.NopCloser(body)
	req.ContentLength = contentLength
	req.Header = filteredHeaders(r.Header)

	resp, err := p.Client.Do(req)
	if err != nil {
		// если клиент ушёл, отдавать ему 502 уже некому
		if r.Context().Err() == nil {
			errs.WriteJSON(w, errs.UpstreamUnreachable(err.Error()))
		}
		return
	}
	defer resp.Body.Close()
	p.copyResponse(w, resp)
}

// copyResponse копирует статус, заголовки и тело ответа апстрима
// клиенту без изменений; данные стримятся с флешами (важно для SSE).
func (p *Proxy) copyResponse(w http.ResponseWriter, resp *http.Response) {
	h := w.Header()
	for k, vv := range resp.Header {
		if isHopHeader(k) {
			continue
		}
		h[k] = vv
	}
	w.WriteHeader(resp.StatusCode)

	rc := http.NewResponseController(w)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return // клиент ушёл
			}
			_ = rc.Flush()
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			return // читаем дальше некому — прерываем копирование
		}
	}
}

// filteredHeaders копирует заголовки запроса, отбрасывая заголовки
// уровня соединения, перечисленные в Connection, и Content-Length
// (он будет пересчитан по новому телу).
func filteredHeaders(src http.Header) http.Header {
	dst := src.Clone()
	for _, tok := range dst.Values("Connection") {
		for _, name := range strings.Split(tok, ",") {
			dst.Del(strings.TrimSpace(name))
		}
	}
	for _, h := range hopHeaders {
		dst.Del(h)
	}
	dst.Del("Content-Length")
	return dst
}

func isHopHeader(name string) bool {
	for _, h := range hopHeaders {
		if strings.EqualFold(name, h) {
			return true
		}
	}
	return false
}
