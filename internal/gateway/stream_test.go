package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agent-room-alkl/subport/internal/model"
)

// Mid-stream cut after early frames: client saw them, FirstByteSent true, only one account tried.
func TestOpenAIStreamMidCutSetsFirstByteAndNoFailover(t *testing.T) {
	var hits atomic.Int32
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"})]}\n\n")
		fl.Flush()
		// Abrupt close by hijacking-like behavior: panic is wrong; instead
		// we close the underlying connection via http.ErrAbortHandler pattern.
		// Easiest portable cut: return without finishing and force connection close.
		if hj, ok := w.(http.Hijacker); ok {
			conn, _, err := hj.Hijack()
			if err == nil {
				_ = conn.Close()
				return
			}
		}
	}))
	defer stub.Close()

	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{
		{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true},
		{ID: "a2", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true},
	})
	_, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec)
	body := rec.Body.String()
	if !strings.Contains(body, "hello") {
		t.Fatalf("client missing early frame: %q err=%v", body, err)
	}
	if err == nil {
		t.Fatal("expected mid-stream error")
	}
	re, ok := err.(RelayError)
	if !ok || !re.FirstByteSent {
		t.Fatalf("want RelayError FirstByteSent=true, got %T %v", err, err)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d want 1 (no failover after first byte)", hits.Load())
	}
}

// Early frame must be readable by the client while the upstream is still blocked.
func TestOpenAIStreamEarlyFrameBeforeUpstreamFinishes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseOnce := func() { once.Do(func() { close(release) }) }
	defer releaseOnce()

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"early\"})]}\n\n")
		fl.Flush()
		select {
		case <-release:
		case <-time.After(3 * time.Second):
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"late\"})]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: {\"usage\":{\"total_tokens\":99})\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer stub.Close()

	pr, pw := io.Pipe()
	w := &flushWriter{w: pw}
	s := NewScheduler([]model.Account{
		{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true},
	})

	errCh := make(chan error, 1)
	go func() {
		_, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, w)
		_ = pw.Close()
		errCh <- err
	}()

	buf := make([]byte, 0, 512)
	tmp := make([]byte, 128)
	deadline := time.Now().Add(2 * time.Second)
	sawEarly := false
	for time.Now().Before(deadline) && !sawEarly {
		n, err := pr.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if strings.Contains(string(buf), "early") {
				sawEarly = true
				releaseOnce()
			}
		}
		if err == io.EOF {
			break
		}
	}
	if !sawEarly {
		releaseOnce()
		t.Fatalf("did not observe early frame while upstream blocked; got %q", string(buf))
	}
	// drain
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("RelayStream err: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RelayStream did not finish")
	}
}

type flushWriter struct {
	w      io.Writer
	header http.Header
}

func (f *flushWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}
func (f *flushWriter) Write(p []byte) (int, error) { return f.w.Write(p) }
func (f *flushWriter) WriteHeader(int)             {}
func (f *flushWriter) Flush()                      {}

func TestOpenAIStreamEOFWithoutDoneIsBroken(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"half a sen\"}}]}\n\n")
		fl.Flush()
		// clean HTTP end, no [DONE]
	}))
	defer stub.Close()
	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true}})
	res, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec)
	if err == nil {
		t.Fatal("expected incomplete stream error")
	}
	re, ok := err.(RelayError)
	if !ok || !re.FirstByteSent {
		t.Fatalf("want FirstByteSent RelayError, got %T %v", err, err)
	}
	if !strings.Contains(rec.Body.String(), "half a sen") {
		t.Fatalf("client missing partial content: %q", rec.Body.String())
	}
	if res.Tokens <= 0 {
		t.Fatalf("tokens fallback should be >0 from delta content, got %d", res.Tokens)
	}
}

func TestOpenAIStreamRequestsIncludeUsage(t *testing.T) {
	var gotBody openAIRequest
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: {\"usage\":{\"total_tokens\":12}}\n\n")
		fl.Flush()
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer stub.Close()
	rec := httptest.NewRecorder()
	s := NewScheduler([]model.Account{{ID: "a1", Provider: "openai", BaseURL: stub.URL, Priority: 1, Healthy: true}})
	res, err := s.RelayStream(ChatRequest{Stream: true, Model: "gpt-4o"}, rec)
	if err != nil {
		t.Fatal(err)
	}
	if gotBody.StreamOptions == nil || !gotBody.StreamOptions.IncludeUsage {
		t.Fatalf("missing stream_options.include_usage: %+v", gotBody.StreamOptions)
	}
	if res.Tokens != 12 {
		t.Fatalf("tokens=%d want 12 from usage frame", res.Tokens)
	}
}
