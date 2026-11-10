package gateway

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/routes"
)

// A subscription past the share left the user told goes behind every
// account with room, and ahead of one all but used up: the gateway's
// requests move at 85% used, not only at 90% and 98%.
func TestSwitchedAccountGoesAfterRoom(t *testing.T) {
	forgetRouting()
	old := allowances
	defer func() { allowances = old }()
	now := time.Now()
	week := 7 * 24 * time.Hour
	share := map[string]provider.Allowance{
		"switched": {
			{Used: 85, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
			{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
		},
		"room": {
			{Used: 10, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
			{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
		},
		"spent": {
			{Used: 99, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
			{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
		},
	}
	allowances = func(string) map[string]provider.Allowance { return share }
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{Account: &provider.Account{Agent: "codex", User: user}}, model: "m", rest: "codex#" + user}
	}
	p := provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex"}}
	cs := []candidate{acct("switched"), acct("room"), acct("spent")}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "codex#room codex#switched codex#spent " {
		t.Fatalf("85%% used: %s, want room, switched, spent", got)
	}

	// under the line, nobody switches
	share["switched"] = provider.Allowance{
		{Used: 84, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
		{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
	}
	if got := restsOf(route(p, cs, "m", provider.Chat)); got != "codex#switched codex#room codex#spent " {
		t.Fatalf("84%% used: %s", got)
	}
}

// A conversation answered on an account past the share moves to one under
// it, its cache there or not: the point of the switch is that the next
// request isn't served past the line.
func TestAChatOnASwitchedAccountMoves(t *testing.T) {
	forgetRouting()
	old := allowances
	defer func() { allowances = old }()
	now := time.Now()
	week := 7 * 24 * time.Hour
	allowances = func(string) map[string]provider.Allowance {
		return map[string]provider.Allowance{
			"a": {
				{Used: 85, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
				{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
			},
			"b": {
				{Used: 10, Span: 5 * time.Hour, Resets: now.Add(time.Hour)},
				{Used: 10, Span: week, Resets: now.Add(2 * 24 * time.Hour)},
			},
		}
	}
	p := provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex"}}
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{ID: "codex", Account: &provider.Account{Agent: "codex", User: user}}, model: "m", rest: "codex#" + user}
	}
	a, b := acct("a"), acct("b")
	cs, wg := weigh(p, []candidate{a, b}, "m", provider.Chat)
	pl := planned{order: make([]Weighed, len(cs))}
	for i, c := range cs {
		pl.order[i] = weighed(c, p, wg, false, provider.Chat)
	}
	// the account past the share is the one that answered last
	at := len(cs) - 1
	if cs[at].rest != "codex#a" || !pl.order[at].Switched || pl.order[0].Switched {
		t.Fatalf("weighed %v, %+v, %+v", restsOf(cs), pl.order[0], pl.order[at])
	}
	h := http.Header{}
	h.Set("x-claude-code-session-id", "switched-at-85")
	body := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	_, _, _, key := affine("t", provider.AffinitySession, false, h, provider.Chat, body, body, cs, pl)
	answered(key, cs[at], 1, Usage{})
	t.Cleanup(func() { sticks.Lock(); delete(sticks.m, key); sticks.Unlock() })
	got, _, aff, _ := affine("t", provider.AffinitySession, false, h, provider.Chat, body, body, cs, pl)
	if got[0].rest != "codex#b" || aff.Why != "switched" || aff.Kept {
		t.Fatalf("went to %s, why %s, kept %v", got[0].rest, aff.Why, aff.Kept)
	}
}

// A conversation moves off an account past the share only when another is
// under it, and isn't resting: with every one past it, its cache is worth
// more than a move.
func TestSwitchedElsewhere(t *testing.T) {
	order := []Weighed{
		{ID: "a", Known: true, Switched: true},
		{ID: "b", Known: true},
	}
	if !switchedElsewhere(order, 0) {
		t.Fatal("a moved off with b under the line")
	}
	if switchedElsewhere(order, 1) {
		t.Fatal("b moved off, though it is under the line")
	}
	order[1].Switched = true
	if switchedElsewhere(order, 0) {
		t.Fatal("a moved off, though every account is past the line")
	}
	order[1] = Weighed{ID: "b", Known: true, Rest: &Rest{}}
	if switchedElsewhere(order, 0) {
		t.Fatal("a moved off to a resting account alone")
	}
	order[1] = Weighed{ID: "b"}
	if switchedElsewhere(order, 0) {
		t.Fatal("a moved off, though b's allowance isn't known")
	}
}

// The move is said once in the log, and only when the head it left was past
// the line: a seat's rest and its who differ for an account, so the match
// is on the rest.
func TestTheSwitchLineIsSaidOnce(t *testing.T) {
	switchedNotes.Lock()
	switchedNotes.m = map[string]string{}
	switchedNotes.Unlock()
	var b bytes.Buffer
	old := log.Writer()
	log.SetOutput(&b)
	t.Cleanup(func() { log.SetOutput(old) })

	p := provider.Provider{ID: "codex-log", Account: &provider.Account{Agent: "codex"}}
	acct := func(user string) candidate {
		return candidate{p: provider.Provider{ID: "codex-log", Account: &provider.Account{Agent: "codex", User: user}}, model: "m", rest: "codex-log#" + user}
	}
	low, room := acct("low"), acct("room")
	cs := []candidate{room, low} // as the routing left them
	seats := []routes.Seat{{Key: low.rest, Switched: true}, {Key: room.rest}}

	noteSwitched(p, low.rest, low.who(), cs, seats, []int{1, 0})
	if got := b.String(); !strings.Contains(got, "moved from codex-log@low, past its switch line, to codex-log@room") {
		t.Fatalf("said %q", got)
	}
	b.Reset()
	noteSwitched(p, low.rest, low.who(), cs, seats, []int{1, 0})
	if b.Len() != 0 {
		t.Fatalf("said the same move twice: %q", b.String())
	}

	// the head that stayed is not a move
	cs = []candidate{low, room}
	seats = []routes.Seat{{Key: low.rest, Switched: true}, {Key: room.rest}}
	noteSwitched(p, low.rest, low.who(), cs, seats, []int{0, 1})
	if b.Len() != 0 {
		t.Fatalf("said a move though the head stayed: %q", b.String())
	}
}
