package provider

import (
	"testing"
	"time"
)

// The share the gateway moves its requests off an account at is the user's,
// told in the same .env the sign-in's move reads: 15% left by default, and
// what they said instead when they said it.
func TestTheToldShareDecidesWhenRequestsMove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MAGPIE_SWITCH_LEFT", "")
	t.Setenv("MAGPIE_SWITCH_LEFT_LONG", "")
	now := time.Now()
	codex := func(used float64) Allowance {
		return Allowance{{Used: used, Span: codexFiveHourSpan, Resets: now.Add(time.Hour)}}
	}

	// the default: 15% left is 85% used
	if SwitchShareReached("codex", codex(84.9), now) {
		t.Fatal("84.9% used moved the requests at the default share")
	}
	if !SwitchShareReached("codex", codex(85), now) {
		t.Fatal("85% used didn't move the requests at the default share")
	}

	// the user says 10% left: 90% used
	t.Setenv("MAGPIE_SWITCH_LEFT", "10")
	if SwitchShareReached("codex", codex(89.9), now) {
		t.Fatal("89.9% used moved the requests at a 10% share")
	}
	if !SwitchShareReached("codex", codex(90), now) {
		t.Fatal("90% used didn't move the requests at a 10% share")
	}
}

func TestSwitchShareReadsWholeAccountWindowsOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MAGPIE_SWITCH_LEFT", "")
	t.Setenv("MAGPIE_SWITCH_LEFT_LONG", "")
	now := time.Now()
	resets := now.Add(time.Hour)

	tests := []struct {
		name  string
		agent string
		a     Allowance
		want  bool
	}{
		{"a model's window is not the account's", "claude", Allowance{
			{Used: 95, Model: "opus", Span: 7 * 24 * time.Hour, Resets: resets},
			{Used: 10, Span: 5 * time.Hour, Resets: resets},
		}, false},
		{"a whole window at the line moves", "claude", Allowance{
			{Used: 85, Span: 5 * time.Hour, Resets: resets},
		}, true},
		{"a whole long window at its line moves", "claude", Allowance{
			{Used: 98, Span: 7 * 24 * time.Hour, Resets: resets},
		}, true},
		{"a whole long window under its line stays", "claude", Allowance{
			{Used: 97.9, Span: 7 * 24 * time.Hour, Resets: resets},
		}, false},
		{"a reset passed is empty again", "claude", Allowance{
			{Used: 90, Span: 5 * time.Hour, Resets: now.Add(-time.Minute)},
		}, false},
		{"nothing read is unknown, not past the line", "claude", Allowance{}, false},
		{"an account with no name is read by its window's span", "someone", Allowance{
			{Used: 85, Span: 5 * time.Hour, Resets: resets},
		}, true},

		// Codex reads its five hours alone: the week says nothing of the
		// next few hours, and removes the account only when it is spent.
		{"codex's week at 90% doesn't move it", "codex", Allowance{
			{Used: 90, Span: 7 * 24 * time.Hour, Resets: resets},
			{Used: 10, Span: codexFiveHourSpan, Resets: resets},
		}, false},
		{"codex's week spent moves it whatever its five hours", "codex", Allowance{
			{Used: 100, Span: 7 * 24 * time.Hour, Resets: resets},
			{Used: 0, Span: codexFiveHourSpan, Resets: resets},
		}, true},
		{"codex's five hours at the line move it", "codex", Allowance{
			{Used: 10, Span: 7 * 24 * time.Hour, Resets: resets},
			{Used: 85, Span: codexFiveHourSpan, Resets: resets},
		}, true},
		{"codex's five hours under the line stay", "codex", Allowance{
			{Used: 10, Span: 7 * 24 * time.Hour, Resets: resets},
			{Used: 84.9, Span: codexFiveHourSpan, Resets: resets},
		}, false},
		{"codex's five hours by name, without a span", "codex", Allowance{
			{Used: 85, Span: 0, Name: "5 hours", Resets: resets},
		}, true},
		{"codex with no five hours read is unknown", "codex", Allowance{
			{Used: 90, Span: 7 * 24 * time.Hour, Resets: resets},
		}, false},
		{"codex's five hours reset is empty again", "codex", Allowance{
			{Used: 90, Span: codexFiveHourSpan, Resets: now.Add(-time.Minute)},
		}, false},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			if got := SwitchShareReached(c.agent, c.a, now); got != c.want {
				t.Fatalf("SwitchShareReached(%s) = %v, want %v", c.agent, got, c.want)
			}
		})
	}
}
