package provider

// Taking over a ChatGPT account another Codex folder holds.
//
// Codex keeps one sign-in a folder (codex-rs login/src/auth/storage.rs:
// auth.json, or the keychain item named by the folder), and it rotates the
// refresh token on every refresh. So an account signed in to two folders
// has one refresh token with two holders: whichever refreshes first spends
// it and signs the other out (refresh_token_reused).
//
// multicodex keeps one account a profile (~/multicodex/profiles/work1/
// codex-home and the rest), so its accounts are each signed in to a folder
// of their own. magpie reads them there (codexHomeLogins) and reads their
// quotas, but it does not switch to them: the account is Codex's there, and
// a copy in logins.json would be a second holder of one refresh token.
//
// Taking one over makes the account magpie's alone: magpie keeps the
// sign-in in logins.json and refreshes it there, and the profile folder is
// read, never written. Its file stays as it was, so the user can read it.
//
// What that costs is the user knows: magpie's refresh spends the refresh
// token the profile folder holds, so that file's token is spent from then
// on, and multicodex needs a fresh sign-in for that account
// (multicodex login <name>). That is the price of one holder, and it is
// what was asked for.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/appdir"
)

// codexTakenOver says whether the saved account l is magpie's own, taken
// from another Codex folder: its sign-in is magpie's to refresh, and the
// folder it came from is left alone.
func codexTakenOver(l savedLogin) bool {
	return l.Agent == "codex" && l.Taken != ""
}

// TakeOverCodexAccounts takes the ChatGPT accounts multicodex holds into
// magpie: each profile's sign-in is read, kept in logins.json and
// refreshed there from then on. The accounts the user named, or all of
// them when names is empty.
//
// The profile folders are read, never written: the sign-in stays in the
// file as it was, and the account is magpie's from logins.json on.
func TakeOverCodexAccounts(ctx context.Context, names []string) ([]string, error) {
	if discoveryOff() {
		return nil, errAccountDiscoveryOff
	}
	homes := multicodexHomes()
	if len(homes) == 0 {
		return nil, fmt.Errorf("no multicodex account is found; is multicodex set up in this home folder?")
	}
	want := map[string]bool{}
	for _, n := range names {
		want[strings.ToLower(strings.TrimSpace(n))] = true
	}
	var taken []string
	for dir, name := range homes {
		if len(want) > 0 && !want[strings.ToLower(name)] {
			continue
		}
		l, ok := codexLoginIn(dir)
		if !ok {
			continue
		}
		keepTakenLogin(l, dir)
		taken = append(taken, l.User)
	}
	if len(taken) == 0 {
		return nil, fmt.Errorf("no multicodex account was taken over")
	}
	return taken, nil
}

// multicodexHomes are the Codex folders multicodex keeps its accounts
// in, each with the name multicodex gives the profile.
func multicodexHomes() map[string]string {
	out := map[string]string{}
	for _, dir := range appdir.MulticodexHomes() {
		name := appdir.MulticodexProfileName(dir)
		if name == "" {
			name = appdir.MulticodexProfileOf(dir)
		}
		if name != "" {
			out[dir] = name
		}
	}
	return out
}

// keepTakenLogin keeps a taken-over account in logins.json: the sign-in
// it had, and the folder it came from, so the user knows where it was and
// may sign in there again.
func keepTakenLogin(l savedLogin, dir string) {
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	l.Home = dir
	l.On = true
	l.Seen = time.Now().UTC().Truncate(time.Second)
	// the account is magpie's own: its sign-in is refreshed in
	// logins.json, and the folder it came from is left alone
	l.Taken = dir
	ls = upsertLogin(ls, l)
	_ = writeLogins(ls)
	log.Printf("codex: took %s over from %s; magpie refreshes its sign-in itself, and the folder is left as it was", l.User, dir)
}

// renewTakenCodex refreshes a ChatGPT account magpie took over from
// another Codex folder: the sign-in is refreshed and kept in logins.json,
// and the folder it came from is not written to, so the two never hold one
// refresh token.
//
// stale is the access token the backend just refused, when it refused one
// that had time left (#2973): the sign-in is refreshed all the same,
// unless it was renewed since.
func renewTakenCodex(ctx context.Context, l savedLogin, stale string) (tok, accountID string, err error) {
	var raw map[string]any
	var a codexAuth
	if json.Unmarshal(l.Auth, &raw) != nil || json.Unmarshal(l.Auth, &a) != nil || a.Tokens.AccessToken == "" {
		return "", "", fmt.Errorf("the saved ChatGPT sign-in of %s is unreadable", l.User)
	}
	accountID = a.Tokens.AccountID
	if accountID == "" {
		accountID = claimString(jwtClaims(a.Tokens.IDToken), "https://api.openai.com/auth", "chatgpt_account_id")
	}
	if stale != "" {
		if a.Tokens.AccessToken != stale {
			return a.Tokens.AccessToken, accountID, nil
		}
	} else if exp, _ := jwtClaims(a.Tokens.AccessToken)["exp"].(float64); exp == 0 || time.Until(time.Unix(int64(exp), 0)) > 5*time.Minute {
		return a.Tokens.AccessToken, accountID, nil
	}
	tok, err = codexRefresh(ctx, raw)
	if err != nil {
		return "", "", savedRefreshFailed(l.Agent, l.User, err)
	}
	auth, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return "", "", err
	}
	loginsMu.Lock()
	defer loginsMu.Unlock()
	ls := readLogins()
	for i := range ls {
		if ls[i].Agent == l.Agent && strings.EqualFold(ls[i].User, l.User) {
			ls[i].Auth = auth
			ls[i].Renewed = time.Now().UTC().Truncate(time.Second)
			ls[i].Lapsed = ""
		}
	}
	return tok, accountID, writeLogins(ls)
}
