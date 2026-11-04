package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The sign-in's move is decided on the five-hour window: an account
// whose week is fresh but whose five hours are used up is spent, and one
// whose five hours are fresh is not, whatever its week says. The week
// still removes an account of its own accord: it is out of both then.
func TestCodexSwitchesOnTheFiveHourWindow(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	// the account the sign-in is on (work@example.com): its five hours
	// are used up, its week is fresh. the spare (me@example.com): its
	// five hours are fresh, its week is nearly used up
	if err := SetLoginOn("codex", "me@example.com", true); err != nil {
		t.Fatal(err)
	}
	used := map[string]map[string]float64{
		"acct-1":                {"five": 5, "week": 99},
		"acct-work@example.com": {"five": 100, "week": 10},
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := used[r.Header.Get("chatgpt-account-id")]
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window":   map[string]any{"used_percent": u["five"], "limit_window_seconds": 18000},
			"secondary_window": map[string]any{"used_percent": u["week"], "limit_window_seconds": 604800}}})
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

	// the sign-in moves to the account whose five hours are fresh, though
	// its week is nearly used up: the week is not what turns the accounts
	// over
	if to := switched(); to != "me@example.com" {
		t.Fatalf("switched to %q, want me@example.com", to)
	}
	if active() != "me@example.com" {
		t.Fatalf("signed in to %s", active())
	}

	// and the other way round: the account whose five hours are fresh
	// stays, though its week is nearly used up. The sign-in went back to
	// the account it came from, as it has room again.
	used["acct-work@example.com"] = map[string]float64{"five": 0, "week": 99}
	used["acct-1"] = map[string]float64{"five": 100, "week": 5}
	if to := switched(); to != "work@example.com" {
		t.Fatalf("switched to %q, want work@example.com", to)
	}
	if active() != "work@example.com" {
		t.Fatalf("signed in to %s", active())
	}
}

// An account whose week is used up is out of both, and is passed over
// however fresh its five hours are.
func TestCodexSkipsAnAccountOutOfItsWeek(t *testing.T) {
	home := signIn(t) // me@example.com, acct-1
	rememberLogins(true)
	codexSignIn(t, home, "work@example.com", "r-work")
	rememberLogins(true)
	// the sign-in's account: its five hours are used up. the spare: its
	// five hours are fresh, its week is used up, so it is out of both
	if err := SetLoginOn("codex", "me@example.com", true); err != nil {
		t.Fatal(err)
	}
	used := map[string]map[string]float64{
		"acct-1":                {"five": 100, "week": 10},
		"acct-work@example.com": {"five": 0, "week": 100},
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := used[r.Header.Get("chatgpt-account-id")]
		json.NewEncoder(w).Encode(map[string]any{"plan_type": "plus", "rate_limit": map[string]any{
			"primary_window":   map[string]any{"used_percent": u["five"], "limit_window_seconds": 18000},
			"secondary_window": map[string]any{"used_percent": u["week"], "limit_window_seconds": 604800}}})
	}))
	defer fake.Close()
	old := CodexBase
	CodexBase = fake.URL + "/backend-api/codex"
	t.Cleanup(func() { CodexBase = old })

	loginUsageCache.Lock()
	loginUsageCache.m = nil
	loginUsageCache.Unlock()
	to, err := SwitchWhenSpent(context.Background(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	if to != "" {
		t.Fatalf("switched to %q, want nothing: the spare is out of its week", to)
	}
}

// A reading that says nothing of the five hours is not a spent one: the
// account is unknown, not out.
func TestCodexUnknownFiveHourIsNotSpent(t *testing.T) {
	q := SubscriptionQuota{Windows: []QuotaWindow{{Name: "5 hours", Used: 100, Span: 5 * time.Hour}}}
	if !codexFiveHourSpent(q, 98, time.Now()) {
		t.Fatal("the five hours at 100% are not spent")
	}
	fresh := SubscriptionQuota{Windows: []QuotaWindow{{Name: "5 hours", Used: 10, Span: 5 * time.Hour}}}
	if codexFiveHourSpent(fresh, 98, time.Now()) {
		t.Fatal("the five hours at 10% are spent")
	}
	unknown := SubscriptionQuota{Windows: []QuotaWindow{{Name: "weekly", Used: 100, Span: 7 * 24 * time.Hour}}}
	if codexFiveHourSpent(unknown, 98, time.Now()) {
		t.Fatal("an account with no five-hour window is spent")
	}
	// a window whose reset has passed started again, whatever share it
	// was read at
	past := time.Now().Add(-time.Minute)
	reset := SubscriptionQuota{Windows: []QuotaWindow{{Name: "5 hours", Used: 100, Span: 5 * time.Hour, ResetsAt: &past}}}
	if codexFiveHourSpent(reset, 98, time.Now()) {
		t.Fatal("a five-hour window whose reset passed is spent")
	}
}
