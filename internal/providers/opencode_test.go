package providers

// Coverage for OpenCode Go client identification (docs: opencode.ai/docs/go
// → "Where can I use it?"): a dedicated User-Agent and a stable per-conversation
// x-opencode-session header on every request to opencode.ai endpoints.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsOpenCodeAPIBase(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"https://opencode.ai/zen/go/v1", true},
		{"https://opencode.ai", true},
		{"https://www.opencode.ai/zen/go/v1", true},
		{"https://api.opencode.ai/v1", true},
		{"https://OpenCode.AI/zen/go/v1", true},
		{"https://opencode.ai:443/zen/go/v1", true},
		// Domain check only — path/query substrings must not match.
		{"https://proxy.example.com/opencode.ai/v1", false},
		{"https://proxy.example.com/v1?key=opencode.ai", false},
		{"https://notopencode.ai/v1", false},
		{"https://opencode.ai.example.net/v1", false},
		{"http://localhost:11434/v1", false},
		{"", false},
		{"://bad url", false},
	}
	for _, tc := range cases {
		if got := IsOpenCodeAPIBase(tc.raw); got != tc.want {
			t.Errorf("IsOpenCodeAPIBase(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestOpenCodeUserAgent_UsesAppVersion(t *testing.T) {
	prev := appVersion
	t.Cleanup(func() { appVersion = prev })

	SetAppVersion("")
	if got := OpenCodeUserAgent(); got != "goclaw/"+prev {
		t.Errorf("SetAppVersion(\"\") must not clobber version, got %q", got)
	}
	SetAppVersion("v3.2.1")
	if got := OpenCodeUserAgent(); got != "goclaw/v3.2.1" {
		t.Errorf("OpenCodeUserAgent() = %q, want %q", got, "goclaw/v3.2.1")
	}
}

// newOpenCodeJSONServer records the last request's identification headers and
// replies with a minimal OpenAI chat-completions JSON body.
func newOpenCodeJSONServer(t *testing.T) (*httptest.Server, *http.Header) {
	t.Helper()
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &captured
}

func TestOpenAIProvider_OpenCodeHeaders_OnChat(t *testing.T) {
	// httptest host is 127.0.0.1, so identification is force-enabled exactly
	// as auto-detection would do for a real opencode.ai apiBase.
	srv, captured := newOpenCodeJSONServer(t)
	p := NewOpenAIProvider("opencode-go", "oc-key", srv.URL, "glm-5.3-flash").
		WithOpenCodeIdentification(true)
	p.retryConfig.Attempts = 1

	_, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "default:telegram:dm:12345"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get("User-Agent"); got != OpenCodeUserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, OpenCodeUserAgent())
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "default:telegram:dm:12345" {
		t.Errorf("%s = %q, want session key", OpenCodeSessionHeader, got)
	}
	if got := captured.Get("Authorization"); got != "Bearer oc-key" {
		t.Errorf("Authorization = %q, want Bearer oc-key", got)
	}
}

func TestOpenAIProvider_OpenCodeHeaders_OnChatStream(t *testing.T) {
	srv, captured := newOpenCodeJSONServer(t)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	})
	p := NewOpenAIProvider("opencode-go", "oc-key", srv.URL, "glm-5.3-flash").
		WithOpenCodeIdentification(true)
	p.retryConfig.Attempts = 1

	_, err := p.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "session-abc"},
	}, nil)
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "session-abc" {
		t.Errorf("%s = %q, want %q", OpenCodeSessionHeader, got, "session-abc")
	}
}

// Without a conversation key (e.g. one-shot HTTP API calls) only the
// User-Agent is sent; the session header is omitted rather than empty.
func TestOpenAIProvider_OpenCodeHeaders_SessionOmittedWithoutKey(t *testing.T) {
	srv, captured := newOpenCodeJSONServer(t)
	p := NewOpenAIProvider("opencode-go", "oc-key", srv.URL, "glm-5.3-flash").
		WithOpenCodeIdentification(true)
	p.retryConfig.Attempts = 1

	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get("User-Agent"); got != OpenCodeUserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, OpenCodeUserAgent())
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "" {
		t.Errorf("%s = %q, want omitted", OpenCodeSessionHeader, got)
	}
}

func TestOpenAIProvider_OpenCodeHeaders_NotSentElsewhere(t *testing.T) {
	srv, captured := newOpenCodeJSONServer(t)
	p := NewOpenAIProvider("other", "sk", srv.URL, "gpt-test")
	p.retryConfig.Attempts = 1

	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "sess-1"},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "" {
		t.Errorf("%s = %q, want absent for non-OpenCode providers", OpenCodeSessionHeader, got)
	}
	if got := captured.Get("User-Agent"); got == OpenCodeUserAgent() {
		t.Errorf("User-Agent = %q, want default client UA for non-OpenCode providers", got)
	}
}

func TestOpenAIProvider_isOpenCodeEndpoint_AutoDetection(t *testing.T) {
	if !NewOpenAIProvider("go", "k", "https://opencode.ai/zen/go/v1", "m").isOpenCodeEndpoint() {
		t.Error("apiBase https://opencode.ai/zen/go/v1 must be detected as OpenCode")
	}
	if NewOpenAIProvider("go", "k", "https://proxy.example/opencode.ai/v1", "m").isOpenCodeEndpoint() {
		t.Error("path-substring opencode.ai must NOT be detected as OpenCode")
	}
	if !NewOpenAIProvider("x", "k", "http://127.0.0.1:1/v1", "m").WithOpenCodeIdentification(true).isOpenCodeEndpoint() {
		t.Error("explicit override must force detection on")
	}
}

func TestAnthropicProvider_OpenCodeHeaders_OnChat(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("oc-key",
		WithAnthropicBaseURL(srv.URL),
		WithAnthropicOpenCodeIdentification(true),
	)
	p.retryConfig.Attempts = 1

	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "agent-session-9"},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get("User-Agent"); got != OpenCodeUserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, OpenCodeUserAgent())
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "agent-session-9" {
		t.Errorf("%s = %q, want %q", OpenCodeSessionHeader, got, "agent-session-9")
	}
	// Standard Anthropic auth headers must remain intact.
	if got := captured.Get("x-api-key"); got != "oc-key" {
		t.Errorf("x-api-key = %q, want oc-key", got)
	}
}

func TestAnthropicProvider_OpenCodeHeaders_AutoDetection(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"x","type":"message","role":"assistant","content":[],"stop_reason":"end_turn"}`)
	}))
	defer srv.Close()

	p := NewAnthropicProvider("k", WithAnthropicBaseURL(srv.URL))
	p.retryConfig.Attempts = 1
	if _, err := p.Chat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "s1"},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "" {
		t.Errorf("%s = %q, want absent for non-OpenCode baseURL", OpenCodeSessionHeader, got)
	}
}

func TestOpenAIAdapter_OpenCodeHeaders_Mirrored(t *testing.T) {
	p := NewOpenAIProvider("opencode-go", "oc-key", "https://opencode.ai/zen/go/v1", "glm-5.3-flash")
	a := &OpenAIAdapter{provider: p}

	_, headers, err := a.ToRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ToRequest: %v", err)
	}
	if got := headers.Get("User-Agent"); got != OpenCodeUserAgent() {
		t.Errorf("adapter User-Agent = %q, want %q", got, OpenCodeUserAgent())
	}
}

// The context channel carries identity for call sites that build their own
// ChatRequest outside the agent pipeline (background summarizers, hooks,
// tool-internal LLM calls) — no Options required.
func TestOpenAIProvider_OpenCodeHeaders_FromUpstreamSessionCtx(t *testing.T) {
	srv, captured := newOpenCodeJSONServer(t)
	p := NewOpenAIProvider("opencode-go", "oc-key", srv.URL, "glm-5.3-flash").
		WithOpenCodeIdentification(true)
	p.retryConfig.Attempts = 1

	ctx := WithUpstreamSession(context.Background(), "ctx-session-7")
	if _, err := p.Chat(ctx, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "ctx-session-7" {
		t.Errorf("%s = %q, want ctx-carried session", OpenCodeSessionHeader, got)
	}
	if got := captured.Get("User-Agent"); got != OpenCodeUserAgent() {
		t.Errorf("User-Agent = %q, want %q", got, OpenCodeUserAgent())
	}
}

// Explicit pipeline Options win over the context-carried value.
func TestOpenAIProvider_OpenCodeHeaders_OptionsWinOverCtx(t *testing.T) {
	srv, captured := newOpenCodeJSONServer(t)
	p := NewOpenAIProvider("opencode-go", "oc-key", srv.URL, "glm-5.3-flash").
		WithOpenCodeIdentification(true)
	p.retryConfig.Attempts = 1

	ctx := WithUpstreamSession(context.Background(), "ctx-session")
	if _, err := p.Chat(ctx, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
		Options:  map[string]any{OptSessionKey: "opt-session"},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "opt-session" {
		t.Errorf("%s = %q, want Options value to win", OpenCodeSessionHeader, got)
	}
}

func TestAnthropicProvider_OpenCodeHeaders_FromUpstreamSessionCtx(t *testing.T) {
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`))
	}))
	defer srv.Close()

	p := NewAnthropicProvider("oc-key",
		WithAnthropicBaseURL(srv.URL),
		WithAnthropicOpenCodeIdentification(true),
	)
	p.retryConfig.Attempts = 1

	ctx := WithUpstreamSession(context.Background(), "ctx-session-9")
	if _, err := p.Chat(ctx, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := captured.Get(OpenCodeSessionHeader); got != "ctx-session-9" {
		t.Errorf("%s = %q, want ctx-carried session", OpenCodeSessionHeader, got)
	}
}

func TestUpstreamSessionFromContext(t *testing.T) {
	if got := UpstreamSessionFromContext(context.Background()); got != "" {
		t.Errorf("empty ctx = %q, want \"\"", got)
	}
	ctx := WithUpstreamSession(context.Background(), "s-1")
	if got := UpstreamSessionFromContext(ctx); got != "s-1" {
		t.Errorf("wrapped ctx = %q, want %q", got, "s-1")
	}
	// Empty key is a no-op: previous value (here: none) survives.
	empty := WithUpstreamSession(context.Background(), "")
	if got := UpstreamSessionFromContext(empty); got != "" {
		t.Errorf("empty wrap = %q, want \"\"", got)
	}
}
