package agent

// Local deployment adapter for the existing AnyRouter Responses channel.
// Enabled explicitly by ARTEX_RESPONSES_COMPAT=anyrouter; other providers keep
// the upstream wire protocol. Credentials and the destination are unchanged.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/transcript"
	"github.com/google/uuid"
)

func installLocalResponsesCompat(c Config, client *http.Client) {
	u, err := url.Parse(c.BaseURL)
	if err != nil || os.Getenv("ARTEX_RESPONSES_COMPAT") != "anyrouter" || c.Format != llm.FormatOpenAIResponses || u.Hostname() != "anyrouter.top" {
		return
	}
	client.Transport = localResponsesTransport{base: client.Transport}
}

type localResponsesTransport struct{ base http.RoundTripper }

func (t localResponsesTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	if !strings.HasSuffix(req.URL.Path, "/responses") || req.Body == nil {
		return base.RoundTrip(req)
	}
	var body map[string]any
	err := json.NewDecoder(req.Body).Decode(&body)
	_ = req.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("local Responses adapter: %w", err)
	}
	if streaming, _ := body["stream"].(bool); !streaming {
		return nil, fmt.Errorf("this AnyRouter channel requires streaming; enable Streaming in the LLM profile")
	}
	sid := transcript.SessionIDFrom(req.Context())
	if sid == "" {
		sid = uuid.NewString()
	}
	turn := uuid.NewString()
	installation := uuid.NewSHA1(uuid.NameSpaceURL, []byte("artex/local-responses-adapter")).String()
	window := sid + ":0"
	meta := map[string]any{"installation_id": installation, "session_id": sid, "thread_id": sid, "turn_id": turn, "window_id": window, "request_kind": "turn", "thread_source": "user", "sandbox": "none", "turn_started_at_unix_ms": time.Now().UnixMilli()}
	metaJSON, _ := json.Marshal(meta)
	if items, ok := body["input"].([]any); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := item["content"].(string); ok {
				typ := "input_text"
				if item["role"] == "assistant" {
					typ = "output_text"
				}
				item["type"] = "message"
				item["content"] = []any{map[string]any{"type": typ, "text": text}}
			}
		}
	}
	if body["tools"] == nil {
		body["tools"] = []any{}
	}
	body["tool_choice"] = "auto"
	body["parallel_tool_calls"] = false
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning == nil {
		reasoning = map[string]any{"effort": "low"}
	}
	reasoning["summary"] = "auto"
	reasoning["context"] = "all_turns"
	body["reasoning"] = reasoning
	body["store"] = false
	body["include"] = []string{"reasoning.encrypted_content"}
	body["prompt_cache_key"] = sid
	body["text"] = map[string]any{"verbosity": "low"}
	body["client_metadata"] = map[string]any{"turn_id": turn, "x-codex-installation-id": installation, "x-codex-window-id": window, "thread_id": sid, "session_id": sid, "x-codex-turn-metadata": string(metaJSON)}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	r := req.Clone(req.Context())
	r.Body = io.NopCloser(bytes.NewReader(encoded))
	r.ContentLength = int64(len(encoded))
	r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(encoded)), nil }
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	r.Header.Set("originator", "codex_exec")
	r.Header.Set("User-Agent", "codex_exec/0.144.1 (Windows; x86_64)")
	r.Header.Set("x-openai-internal-codex-responses-lite", "true")
	r.Header.Set("session-id", sid)
	r.Header.Set("thread-id", sid)
	r.Header.Set("x-client-request-id", sid)
	r.Header.Set("x-codex-window-id", window)
	r.Header.Set("x-codex-turn-metadata", string(metaJSON))
	return base.RoundTrip(r)
}
