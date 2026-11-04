package provider

// The window a ChatGPT account's five-hour quota is in, and what is left
// of it.
//
// Codex reads two windows of a ChatGPT account: the primary, five hours,
// and the secondary, the week (codex-rs's /wham/usage: primary_window and
// secondary_window). A user who runs several accounts has one account's
// five hours used up while another's week is still fresh, and wants the
// accounts to turn over on the five hours: the week's window says
// nothing of the next few hours.
//
// So the sign-in's move is decided on the five-hour window alone, and the
// week is left for what it is: an account whose week is used up is out of
// both, and is passed over whatever its five hours say.
//
// The window is told by its length (five hours) first, and by its name
// ("5 hours", from quotaDurationName) second, because a reading that
// does not say how long the window runs still names it.

import (
	"time"
)

// codexFiveHourSpan is how long a ChatGPT account's five-hour window runs.
const codexFiveHourSpan = 5 * time.Hour

// codexSwitchLeft is the share of the five-hour window that is left when
// the sign-in moves off an account: 15, so the account is moved off at
// 85% used.
//
// A turn that starts only when the account is used up leaves the next
// account no room to be of use: the window that was there when the turn
// began is gone by the time the account is exhausted, and the next
// account's own window has run for nothing. Moving off at 85% leaves the
// next account a window's worth of room to be used in.
//
// It is below codexBackShare, the share the account comes back at, so the
// sign-in does not go back and forth at the edge: an account moved off at
// 85% is back only once it is under codexBackShare, which is further
// from the edge than the share it was moved off at.
const codexSwitchLeft = 15

// codexSwitchShare is the share of the five-hour window past which the
// sign-in moves off an account: 100 - codexSwitchLeft, so 85.
const codexSwitchShare = 100 - codexSwitchLeft

// codexBackShare is the share below which the account the sign-in moved
// off comes back to: under it there is room again. It is above the share
// the account is moved off at (codexSwitchLeft), so the two do not
// overlap.
const codexBackShare = 80

// codexFiveHourNames are the names a five-hour window goes by in a
// reading, in lower case.
var codexFiveHourNames = []string{"5 hours", "5 hour", "5h", "five hours", "five hour"}

// codexFiveHour is the five-hour window of the account's quota, and
// whether there is one. The window with no model named counts every
// model, so it is the account's own five hours (a per-model window is
// one model's, and says nothing of the account's).
func codexFiveHour(q SubscriptionQuota) (QuotaWindow, bool) {
	var best QuotaWindow
	found := false
	for _, w := range q.Windows {
		if w.Aside || w.Model != "" {
			continue
		}
		if !codexFiveHourWindow(w) {
			continue
		}
		// the fullest of them: an account may be told of more than one
		if !found || w.Used > best.Used {
			best, found = w, true
		}
	}
	return best, found
}

// codexFiveHourWindow says whether the window is a five-hour one.
func codexFiveHourWindow(w QuotaWindow) bool {
	if w.Span == codexFiveHourSpan {
		return true
	}
	// a reading that doesn't say how long the window runs still names it
	if w.Span != 0 {
		return false
	}
	name := WindowCapID(w.Name)
	for _, n := range codexFiveHourNames {
		if name == n {
			return true
		}
	}
	return false
}

// codexFiveHourSpent says whether the account's five-hour window is used
// up at share, or so near it that it is spent for the sign-in's move.
//
// A reading that says nothing of the five hours is not a spent one: the
// account is unknown, not out. The week's window is not read here: it
// removes an account of its own accord (codexFiveHourOut).
func codexFiveHourSpent(q SubscriptionQuota, share float64, now time.Time) bool {
	w, ok := codexFiveHour(q)
	if !ok {
		return false
	}
	if resetPassed(w, now) {
		// the window started again, whatever share it was read at
		return false
	}
	return w.Used >= share
}

// codexFiveHourOut says whether the account is out of both windows: its
// week is used up, so no five hours of it would be of any use.
func codexFiveHourOut(q SubscriptionQuota, now time.Time) bool {
	for _, w := range q.Windows {
		if w.Aside || w.Model != "" {
			continue
		}
		if w.Span == codexFiveHourSpan {
			continue
		}
		if w.Used < 100 || resetPassed(w, now) {
			continue
		}
		return true
	}
	return false
}
