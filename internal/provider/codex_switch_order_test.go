package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// In order, Codex moves off the account it is on once its five-hour
// allowance has 15% left (codexSwitchLeft), as the gateway still sends
// that one requests first but a turn that started only at the end would
// leave the next account no room (#530); Smart moves it off at the same
// place, for the five hours are what the sign-in's move is decided on
// (codex_fivehour.go). Kept on the first (KeepLogin), it stays however
// used up that is, and moves on again once that is off (#524).
//
// A week at 98% does not move it off an account whose five hours are
// fresh.
func TestCodexLoginOrderedAndKept(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	if err := SetLoginOn("codex", "spare@example.com", true); err != nil {
		t.Fatal(err)
	}
	// the five hours of each account, and its week
	used := map[string]float64{"acct-work@example.com": 80, "acct-1": 0, "acct-spare@example.com": 20}
	week := map[string]float64{"acct-work@example.com": 98, "acct-1": 0, "acct-spare@example.com": 0}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct := r.Header.Get("chatgpt-account-id")
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window":   map[string]any{"used_percent": used[acct], "limit_window_seconds": 18000},
			"secondary_window": map[string]any{"used_percent": week[acct], "limit_window_seconds": 604800}}})
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
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// in order: with 15% of its five hours left it moves on, as Smart
	// does, for the five hours are what the move is decided on
	if err := SetRouting("codex", Ordered); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "" {
		t.Fatalf("in order, moved off an account with 20%% left, to %s", to)
	}
	used["acct-work@example.com"] = 85
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("in order, 15%% left: moved to %q", to)
	}
	// renewed, back on the first
	used["acct-work@example.com"] = 0
	if to := switched(); to != "work@example.com" {
		t.Fatalf("back to %q", to)
	}

	// a week at 98% does not move it off: the five hours are what the
	// move is decided on
	used["acct-work@example.com"] = 80
	week["acct-work@example.com"] = 99
	if to := switched(); to != "" {
		t.Fatalf("moved off an account whose week is at 99%%, to %s", to)
	}
	week["acct-work@example.com"] = 98

	// smart: with 15% left it moves on too
	if err := SetRouting("codex", ""); err != nil {
		t.Fatal(err)
	}
	used["acct-work@example.com"] = 85
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("smart with 15%% left: moved to %q", to)
	}
	used["acct-work@example.com"] = 0
	if to := switched(); to != "work@example.com" {
		t.Fatalf("back to %q", to)
	}

	// kept on the first: it stays, used up or not; the setting outlives a
	// save of the provider's picks
	if err := SetKeepLogin("codex", true); err != nil {
		t.Fatal(err)
	}
	if err := SetRouting("codex", ""); err != nil {
		t.Fatal(err)
	}
	if p, err := Find("codex"); err != nil || !p.KeepLogin {
		t.Fatalf("kept on the first: %+v %v", p, err)
	}
	used["acct-work@example.com"] = 100
	if to := switched(); to != "" {
		t.Fatalf("kept on the first, moved to %s", to)
	}
	if active() != "work@example.com" {
		t.Fatalf("kept on the first, signed in to %s", active())
	}
	// let go: it moves on again
	if err := SetKeepLogin("codex", false); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("let go: moved to %q", to)
	}
	// kept on the first again: back on it at once, used up as it is
	if err := SetKeepLogin("codex", true); err != nil {
		t.Fatal(err)
	}
	if to := switched(); to != "work@example.com" {
		t.Fatalf("kept on the first again: moved to %q", to)
	}
	if to := switched(); to != "" {
		t.Fatalf("kept on the first again, then moved to %s", to)
	}

	// only Codex's and Claude Code's sign-ins are moved
	if err := SetKeepLogin("nonesuch", true); err == nil {
		t.Fatal("kept a provider that isn't there")
	}
}
