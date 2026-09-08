package consolidation

// Regression coverage for the OpenCode MissingSessionID failure:
// session.completed without a compaction summary must still carry the
// conversation identity to the LLM boundary (providers.WithUpstreamSession),
// and must NOT set Options[OptSessionKey] (that option triggers session-resume
// behavior in other providers, e.g. Claude CLI --resume).

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// stubChatCap captures the ctx/req reaching the LLM boundary.
type stubChatCap struct {
	gotCtx context.Context
	gotReq providers.ChatRequest
}

func (s *stubChatCap) Chat(ctx context.Context, _ providers.Provider, req providers.ChatRequest, _ usagecaps.ChatOptions) (*providers.ChatResponse, error) {
	s.gotCtx, s.gotReq = ctx, req
	return &providers.ChatResponse{Content: "stub summary"}, nil
}

func TestEpisodicWorkerHandle_SummarizeCarriesSessionKey(t *testing.T) {
	mockStore := &mockEpisodicStore{existsByID: make(map[string]bool)}
	mockEventBus := newMockDomainEventBus()
	mockSessions := &mockSessionStore{
		history: []providers.Message{{Role: "user", Content: "hello"}},
	}
	stub := &stubChatCap{}

	worker := &episodicWorker{
		store:     mockStore,
		sessions:  mockSessions,
		registry:  testRegistry(&mockProvider{}),
		eventBus:  mockEventBus,
		usageCaps: stub,
	}

	event := eventbus.DomainEvent{
		Type:     eventbus.EventSessionCompleted,
		TenantID: providers.MasterTenantID.String(),
		AgentID:  uuid.New().String(),
		UserID:   "test-user",
		Payload: &eventbus.SessionCompletedPayload{
			SessionKey:      "session-abc",
			CompactionCount: 1,
			Summary:         "", // force the LLM summarize path
			MessageCount:    1,
			TokensUsed:      10,
		},
	}

	if err := worker.Handle(context.Background(), event); err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if stub.gotCtx == nil {
		t.Fatal("expected LLM summarize call, got none")
	}
	if got := providers.UpstreamSessionFromContext(stub.gotCtx); got != "session-abc" {
		t.Errorf("upstream session = %q, want %q", got, "session-abc")
	}
	if _, ok := stub.gotReq.Options[providers.OptSessionKey]; ok {
		t.Errorf("Options must not carry %q (would trigger session-resume in CLI/ACP providers)", providers.OptSessionKey)
	}
	if len(mockStore.created) != 1 || mockStore.created[0].Summary != "stub summary" {
		t.Errorf("expected stored stub summary, got %+v", mockStore.created)
	}
}
