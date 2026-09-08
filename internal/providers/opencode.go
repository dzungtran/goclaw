package providers

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// OpenCode Go / Zen endpoints monitor traffic for abuse that degrades service
// for other users (docs: opencode.ai/docs/go → "Where can I use it?").
// Clients must:
//  1. Identify themselves with their own User-Agent (e.g. "my-coding-agent/1.0")
//     instead of the generic Go-http-client/1.1 default.
//  2. Send a stable session ID in the x-opencode-session header per conversation
//     so upstream can optimize routing and prompt caching.
const (
	OpenCodeSessionHeader = "x-opencode-session"
	openCodeHost          = "opencode.ai"
)

// IsOpenCodeAPIBase reports whether rawURL targets opencode.ai or one of its
// subdomains. The check is on the parsed hostname — URLs that merely contain
// "opencode.ai" in a path or query segment (e.g. a reverse proxy) do not match.
func IsOpenCodeAPIBase(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == openCodeHost || strings.HasSuffix(host, "."+openCodeHost)
}

// OpenCodeUserAgent is the client identity sent to OpenCode endpoints,
// e.g. "goclaw/v3.2.0". Falls back to "goclaw/dev" when no build version is set.
func OpenCodeUserAgent() string {
	return "goclaw/" + appVersion
}

type openCodeSessionKey struct{}

// WithUpstreamSession carries the conversation's session key to the transport
// layer so doRequest can attach x-opencode-session. This is the identity
// channel for call sites that build their own ChatRequest outside the agent
// pipeline (background summarizers, hooks, tool-internal LLM calls).
//
// It deliberately does NOT use ChatRequest.Options[OptSessionKey]: that option
// has provider-specific side effects (Claude CLI --resume, ACP continuity)
// which must not trigger for sidecar calls. No-op when sessionKey is empty.
func WithUpstreamSession(ctx context.Context, sessionKey string) context.Context {
	if sessionKey == "" {
		return ctx
	}
	return context.WithValue(ctx, openCodeSessionKey{}, sessionKey)
}

func openCodeSessionFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(openCodeSessionKey{}).(string)
	return v
}

// UpstreamSessionFromContext returns the conversation identity carried by
// WithUpstreamSession ("" when absent). Exported so background workers and
// their tests can observe propagation without reaching the network.
func UpstreamSessionFromContext(ctx context.Context) string {
	return openCodeSessionFromCtx(ctx)
}

// applyOpenCodeHeaders sets the identification headers required by OpenCode.
// The session header is omitted when no conversation key is available.
func applyOpenCodeHeaders(h http.Header, sessionID string) {
	h.Set("User-Agent", OpenCodeUserAgent())
	if sessionID != "" {
		h.Set(OpenCodeSessionHeader, sessionID)
	}
}
