package vault

// Regression coverage for the OpenCode MissingSessionID failure on
// vault.classify / vault.batch_summarize: enrichment batches mix documents
// from many sessions, so the worker attaches a stable synthetic identity
// (vaultEnrichSessionID) that must survive retries/timeouts to the provider.

import (
	"context"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

func TestVaultEnrichSessionID_StablePerTenant(t *testing.T) {
	a := vaultEnrichSessionID("tenant-1")
	if a == "" || a == "tenant-1" {
		t.Fatalf("synthetic session must be namespaced, got %q", a)
	}
	if got := vaultEnrichSessionID("tenant-1"); got != a {
		t.Errorf("not stable: %q vs %q", got, a)
	}
	if got := vaultEnrichSessionID("tenant-2"); got == a {
		t.Errorf("tenant isolation violated: %q", got)
	}
}

// TestCallClassifyWithRetry_PreservesUpstreamSession proves the enrichment
// chokepoint (chatWithRetry: timeouts + retries) forwards the session identity
// to the provider instead of dropping it.
func TestCallClassifyWithRetry_PreservesUpstreamSession(t *testing.T) {
	provider := &mockClassifyProvider{
		responses: []string{`[{"idx":1,"type":"reference","ctx":"test"}]`},
		errors:    []error{nil},
	}
	worker := &EnrichWorker{}

	ctx := providers.WithUpstreamSession(context.Background(), vaultEnrichSessionID("tenant-9"))
	if _, err := worker.callClassifyWithRetry(ctx, provider, "test", "system", "user"); err != nil {
		t.Fatalf("callClassifyWithRetry failed: %v", err)
	}
	if provider.lastCtx == nil {
		t.Fatal("provider saw no context")
	}
	if got := providers.UpstreamSessionFromContext(provider.lastCtx); got != vaultEnrichSessionID("tenant-9") {
		t.Errorf("upstream session at provider = %q, want synthetic batch identity", got)
	}
}
