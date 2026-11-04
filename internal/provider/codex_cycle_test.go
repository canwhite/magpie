package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The ChatGPT accounts turn over as a cycle: the target is the first
// account in the order that has room, so the same account takes over
// again once its five hours reset, and every cycle starts at the same
// place.
func TestCodexAccountsTurnOverAsACycle(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	codexSignIn(t, home, "spare@example.com", "r-spare")
	rememberLogins(true)
	for _, u := range []string{"work@example.com", "spare@example.com", "me@example.com"} {
		if err := SetLoginOn("codex", u, true); err != nil {
			t.Fatal(err)
		}
	}
	// the order the user arranged: work, then spare, then the one it is
	// signed in to
	order := []string{"work@example.com", "spare@example.com", "me@example.com"}
	if err := SetAccountOrder("codex", order); err != nil {
		t.Fatal(err)
	}
	// the five hours of each account
	five := map[string]float64{"acct-work@example.com": 100, "acct-1": 0, "acct-spare@example.com": 0}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acct := r.Header.Get("chatgpt-account-id")
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window": map[string]any{"used_percent": five[acct], "limit_window_seconds": 18000}}})
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
	// arranging the order signed Codex in to the first of it, as it does
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// the account in use is spent: the next in the order takes over
	five["acct-work@example.com"] = 100
	five["acct-spare@example.com"] = 0
	five["acct-1"] = 0
	if to := switched(); to != "spare@example.com" {
		t.Fatalf("switched to %q, want spare@example.com", to)
	}
	if active() != "spare@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// now spare is spent too, so the one after it takes over
	five["acct-spare@example.com"] = 100
	five["acct-1"] = 0
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q, want me@example.com", to)
	}
	if active() != "me@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// and once the five hours of the first in the order reset, it takes
	// over again: the cycle starts at the same place, not at the account
	// the sign-in was on before
	five["acct-work@example.com"] = 0
	five["acct-spare@example.com"] = 100
	five["acct-1"] = 100
	if to := switched(); to != "work@example.com" {
		t.Fatalf("switched to %q, want work@example.com: the cycle starts at the first", to)
	}
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// and when the first is spent again, the cycle goes on from there:
	// the account after it takes over
	five["acct-work@example.com"] = 100
	five["acct-1"] = 0
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q, want me@example.com", to)
	}
	if active() != "me@example.com" {
		t.Fatalf("signed in to %s", active())
	}
}
