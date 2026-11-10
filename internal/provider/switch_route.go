package provider

// When the gateway moves its requests off an account.
//
// The share is the user's, told in ~/.config/magpie/.env beside the one the
// sign-in's move is decided on (MAGPIE_SWITCH_LEFT, MAGPIE_SWITCH_LEFT_LONG,
// switch_config.go): a request goes to another account of the pool once the
// one serving it has reached that share of its window. The two moves are
// separate — the sign-in is the agent's own, the requests are the gateway's
// — but they read the same numbers, so the user tells one place.
//
// Only a window that counts the whole account is read: a window naming a
// model (Opus's week) says nothing of the account's other models. Codex is
// read as its sign-in is (codex_fivehour.go): on the five hours alone, the
// week removing it only when the week is spent.

import "time"

// SwitchShareReached says whether requests should move off the account
// whose allowance is a: one of its whole-account windows has reached the
// share the user told for a window that runs as long as it does.
//
// A window whose reset has passed is empty again, whatever share it was
// read at. No window read is unknown, not past the line: the account stays
// where it is.
func SwitchShareReached(agent string, a Allowance, now time.Time) bool {
	if len(a) == 0 {
		return false
	}
	if agent == "codex" {
		return codexSwitchReached(a, now)
	}
	for _, l := range a {
		if l.Model != "" || l.matches != nil {
			continue // one model's window, not the account's
		}
		if !l.Resets.IsZero() && !l.Resets.After(now) {
			continue
		}
		if l.Used >= switchShareFor(l.Span).used() {
			return true
		}
	}
	return false
}

// codexSwitchReached is SwitchShareReached for a ChatGPT account: its five
// hours are what the requests move on, and its week removes it only when
// the week is spent, as the sign-in's move reads them.
func codexSwitchReached(a Allowance, now time.Time) bool {
	for _, l := range a {
		if l.Model != "" || l.matches != nil || l.Span == codexFiveHourSpan {
			continue
		}
		if l.Used >= 100 && (l.Resets.IsZero() || l.Resets.After(now)) {
			return true
		}
	}
	w, ok := codexFiveHourLimit(a)
	if !ok {
		return false
	}
	if !w.Resets.IsZero() && !w.Resets.After(now) {
		return false
	}
	return w.Used >= switchShareFor(codexFiveHourSpan).used()
}

// codexFiveHourLimit is the account's own five-hour window, the fullest of
// them, and whether there is one. It is codexFiveHour for an Allowance: the
// same window by its length first, and its name second.
func codexFiveHourLimit(a Allowance) (Limit, bool) {
	var best Limit
	found := false
	for _, l := range a {
		if l.Model != "" || l.matches != nil || !codexFiveHourLimitWindow(l) {
			continue
		}
		if !found || l.Used > best.Used {
			best, found = l, true
		}
	}
	return best, found
}

func codexFiveHourLimitWindow(l Limit) bool {
	if l.Span == codexFiveHourSpan {
		return true
	}
	if l.Span != 0 {
		return false
	}
	name := WindowCapID(l.Name)
	for _, n := range codexFiveHourNames {
		if name == n {
			return true
		}
	}
	return false
}
