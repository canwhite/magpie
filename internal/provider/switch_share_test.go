package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The share the sign-in moves off an account at is the user's: told in the
// .env beside magpie's own files, it moves the sign-in at the share they
// said, and not at the default.
func TestTheToldShareDecidesWhenTheSignInMoves(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	config := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	// the user says the sign-in moves off with 40% of the five hours
	// left, where the default is 15%
	if err := os.WriteFile(filepath.Join(config, ".env"), []byte("MAGPIE_SWITCH_LEFT=40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	for _, u := range []string{"work@example.com", "me@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetAccountOrder("codex", []string{"work@example.com", "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	// the five hours of the account the sign-in is on
	used := map[string]float64{"acct-1": 0, "acct-work@example.com": 50}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct := r.Header.Get("chatgpt-account-id")
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": used[acct], "limit_window_seconds": 18000}}})
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })

	switched := func() string {
		t.Helper()
		loginUsageCache.Lock()
		loginUsageCache.m = nil
		loginUsageCache.Unlock()
		to, err := SwitchWhenSpent(context.Background(), "codex")
		if err != nil {
			t.Fatal(err)
		}
		return to
	}

	// 50% left is room under the default (15% left) and under the share
	// the user told (40% left): the sign-in stays
	if to := switched(); to != "" {
		t.Fatalf("switched to %q with 50%% left", to)
	}
	// 60% used is 40% left: the share the user told moves the sign-in
	used["acct-work@example.com"] = 60
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q with 40%% left, want me@example.com", to)
	}
}
