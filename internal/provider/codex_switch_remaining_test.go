package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The sign-in moves on when the account has 15% of its five-hour quota
// left, not when it is used up: the accounts turn over on the five hours,
// and a turn that starts only at the end leaves the next account with no
// room to be of use.
func TestCodexSwitchesWithFifteenPercentLeft(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	for _, u := range []string{"work@example.com", "me@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	// the sign-in is on the account the order puts first: work
	if err := SetAccountOrder("codex", []string{"work@example.com", "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	// the five hours of each account: the one in use has 20% left (80%
	// used), the other is fresh
	used := map[string]float64{"acct-1": 0, "acct-work@example.com": 80}
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
	active := func() string {
		t.Helper()
		for _, l := range Logins("codex") {
			if l.Active {
				return l.User
			}
		}
		return ""
	}

	// 20% left is still room: the sign-in stays
	if to := switched(); to != "" {
		t.Fatalf("switched to %q with 20%% left", to)
	}
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// 15% left: the account is moved off, and the next one in the order
	// takes over
	used["acct-work@example.com"] = 85
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q with 15%% left, want me@example.com", to)
	}
	if active() != "me@example.com" {
		t.Fatalf("signed in to %s", active())
	}
}

// The account the sign-in was moved off comes back once its five hours
// have room again, and not before: the threshold it was moved off at and
// the one it comes back at must not overlap, or the sign-in would go back
// and forth at the edge.
func TestCodexComesBackOnlyWhenThereIsRoom(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	for _, u := range []string{"work@example.com", "me@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	// the sign-in is on work@example.com, the first in the order, and
	// its five hours are down to 15%
	if err := SetAccountOrder("codex", []string{"work@example.com", "me@example.com"}); err != nil {
		t.Fatal(err)
	}
	used := map[string]float64{"acct-1": 0, "acct-work@example.com": 85}
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

	// moved off the first, at 15% left
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q, want me@example.com", to)
	}
	// it does not come back while its five hours are still low: the share
	// it comes back at is below the one it was moved off at
	used["acct-work@example.com"] = 89
	if to := switched(); to != "" {
		t.Fatalf("switched back to %q while the first is still low", to)
	}
	// and it does come back once they are fresh
	used["acct-work@example.com"] = 10
	if to := switched(); to != "work@example.com" {
		t.Fatalf("switched to %q, want work@example.com", to)
	}
}
