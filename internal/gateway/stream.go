package gateway

import (
        "bufio"
        "bytes"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "strings"

        "github.com/agent-room-alkl/subport/internal/model"
)

func StreamUpstream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
        p, err := ProviderFor(a.Provider)
        if err != nil {
                return 0, RelayError{Err: err, FirstByteSent: false}
        }
        return p.Stream(a, req, w)
}

func (s *Scheduler) RelayStream(req ChatRequest, w http.ResponseWriter) (Result, error) {
        var lastErr error
        for attempt := 0; attempt < MaxAttempts; attempt++ {
                acct, ok := s.Pick(attempt)
                if !ok {
                        continue
                }
                tokens, err := StreamUpstream(acct, req, w)
                status := "200"
                if err != nil {
                        status = "failed"
                }
                log.Printf("stream attempt=%d account=%s provider=%s -> %s", attempt, acct.ID, acct.Provider, status)
                if err == nil {
                        return Result{Account: acct, Tokens: tokens, Attempts: attempt + 1}, nil
                }
                lastErr = err
                if re, ok := err.(RelayError); ok && re.FirstByteSent {
                        return Result{Account: acct, Attempts: attempt + 1, Tokens: tokens}, err
                }
        }
        if lastErr == nil {
                lastErr = fmt.Errorf("no healthy account")
        }
        return Result{}, lastErr
}

func ensureSSEHeaders(w http.ResponseWriter) {
        h := w.Header()
        if h.Get("Content-Type") == "" {
                h.Set("Content-Type", "text/event-stream")
        }
        h.Set("Cache-Control", "no-cache")
        h.Set("Connection", "keep-alive")
}

func (mockProvider) Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
        reply, err := (mockProvider{}).Call(a, req)
        if err != nil {
                return 0, err
        }
        ensureSSEHeaders(w)
        flusher, _ := w.(http.Flusher)
        first := false
        write := func(s string) error {
                n, err := io.WriteString(w, s)
                if err != nil {
                        return err
                }
                if n > 0 {
                        first = true
                }
                if flusher != nil {
                        flusher.Flush()
                }
                return nil
        }
        parts := []string{"Subport ", "demo ", reply.Content}
        for _, part := range parts {
                payload, _ := json.Marshal(map[string]any{
                        "choices": []any{map[string]any{"delta": map[string]string{"content": part}}},
                })
                if err := write("data: " + string(payload) + "\n\n"); err != nil {
                        return 0, RelayError{Err: err, FirstByteSent: first}
                }
        }
        _ = write("data: [DONE]\n\n")
        return reply.Tokens, nil
}

func (openAIProvider) Stream(a model.Account, req ChatRequest, w http.ResponseWriter) (int64, error) {
        msgs := make([]openAIChatMessage, 0, len(req.Messages))
        for _, m := range req.Messages {
                msgs = append(msgs, openAIChatMessage{Role: m.Role, Content: m.Content})
        }
        body, err := json.Marshal(openAIRequest{Model: req.Model, Messages: msgs, Stream: true})
        if err != nil {
                return 0, RelayError{Err: err, FirstByteSent: false}
        }
        url := strings.TrimRight(a.BaseURL, "/") + "/v1/chat/completions"
        httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
        if err != nil {
                return 0, RelayError{Err: err, FirstByteSent: false}
        }
        httpReq.Header.Set("Content-Type", "application/json")
        httpReq.Header.Set("Accept", "text/event-stream")
        key := credentialFor(a.Provider)
        if key != "" {
                httpReq.Header.Set("Authorization", "Bearer "+key)
        }
        resp, err := upstreamClient.Do(httpReq)
        if err != nil {
                return 0, RelayError{Err: err, FirstByteSent: false}
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 300 {
                var out openAIResponse
                _ = json.NewDecoder(resp.Body).Decode(&out)
                msg := fmt.Sprintf("upstream status %d", resp.StatusCode)
                if out.Error != nil && out.Error.Message != "" {
                        msg = fmt.Sprintf("%s: %s", msg, redactSecrets(out.Error.Message, key))
                }
                return 0, RelayError{Err: fmt.Errorf("%s", msg), FirstByteSent: false}
        }
        ensureSSEHeaders(w)
        flusher, _ := w.(http.Flusher)
        firstByte := false
        var tokens int64
        reader := bufio.NewReader(resp.Body)
        for {
                line, readErr := reader.ReadString('\n')
                if len(line) > 0 {
                        n, werr := io.WriteString(w, line)
                        if n > 0 {
                                firstByte = true
                                if flusher != nil {
                                        flusher.Flush()
                                }
                        }
                        if werr != nil {
                                return tokens, RelayError{Err: werr, FirstByteSent: firstByte}
                        }
                        trimmed := strings.TrimRight(line, "\r\n")
                        if strings.HasPrefix(trimmed, "data:") {
                                data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
                                if data != "" && data != "[DONE]" {
                                        var frame struct {
                                                Usage *struct {
                                                        TotalTokens int64 `json:"total_tokens"`
                                                } `json:"usage"`
                                        }
                                        if json.Unmarshal([]byte(data), &frame) == nil && frame.Usage != nil && frame.Usage.TotalTokens > 0 {
                                                tokens = frame.Usage.TotalTokens
                                        }
                                }
                        }
                }
                if readErr != nil {
                        if readErr == io.EOF {
                                if !firstByte {
                                        return 0, RelayError{Err: fmt.Errorf("upstream closed before any byte"), FirstByteSent: false}
                                }
                                return tokens, nil
                        }
                        return tokens, RelayError{Err: fmt.Errorf("stream truncated: %v", redactSecrets(readErr.Error(), key)), FirstByteSent: firstByte}
                }
        }
}
