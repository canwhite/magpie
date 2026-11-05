package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// teamName is how a ChatGPT account of a team plan is named: its email
// and the plan after it, so two seats of one email read apart
// (codexUser).
const teamName = "work@example.com · Team"

// A ChatGPT account magpie took over from another Codex folder is
// magpie's alone: magpie refreshes it in logins.json, the folder it came
// from is read and never written, and the account may be switched to
// without a second holder of one refresh token.
func TestTakenOverCodexAccountIsMagpiesAlone(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	// multicodex says where its profiles are
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	ForgetAccounts()

	taken, err := TakeOverCodexAccounts(context.Background(), nil)
	if err != nil {
		t.Fatalf("taking the account over: %v", err)
	}
	if len(taken) != 1 || taken[0] != teamName {
		t.Fatalf("taken = %v, want %s", taken, teamName)
	}
	// the account is magpie's, in use, and not kept out of the rotation
	ls := Logins("codex")
	var found *Login
	for i := range ls {
		if ls[i].User == teamName {
			found = &ls[i]
		}
	}
	if found == nil || !found.On {
		t.Fatalf("the taken-over account isn't in use: %+v", ls)
	}
	// the folder it came from is left as it was: magpie reads it, never
	// writes it, so a Codex run there is signed in as it was
	b, err := os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a codexAuth
	if json.Unmarshal(b, &a) != nil || a.Tokens.RefreshToken != "r-work" {
		t.Fatalf("the profile folder was written: %s", b)
	}
	// and magpie may switch to it: the sign-in moves to ~/.codex, and
	// the profile folder is given none, so the two never hold one
	// refresh token
	if err := SwitchLogin("codex", teamName); err != nil {
		t.Fatalf("switching to the taken-over account: %v", err)
	}
	main, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	var m codexAuth
	if json.Unmarshal(main, &m) != nil || m.Tokens.RefreshToken != "r-work" {
		t.Fatalf("~/.codex/auth.json doesn't hold the account: %s", main)
	}
	// and the profile folder is left as it was: it is read, never
	// written, so magpie's copy is the account's one holder
	folder, _ := os.ReadFile(filepath.Join(profile, "auth.json"))
	var f codexAuth
	if json.Unmarshal(folder, &f) != nil || f.Tokens.RefreshToken != "r-work" {
		t.Fatalf("the profile folder's sign-in was changed: %s", folder)
	}
}

// A taken-over account is refreshed in magpie's own copy, never in the
// folder it came from: the two would hold one refresh token, and
// whichever refreshed first would sign the other out.
func TestTakenOverCodexAccountIsRefreshedInMagpiesCopy(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(-time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	ForgetAccounts()
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over: %v", err)
	}

	fresh := fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix()), "n": "fresh"})
	var mu sync.Mutex
	var refreshed []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		refreshed = append(refreshed, body["refresh_token"])
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"id_token": idTok, "access_token": fresh, "refresh_token": "r-work-2"})
	}))
	defer fake.Close()
	oldTok, oldBase := codexTokenURL, CodexBase
	t.Cleanup(func() { codexTokenURL, CodexBase = oldTok, oldBase })
	codexTokenURL, CodexBase = fake.URL, fake.URL+"/backend-api/codex"

	tok, _, err := savedLoginToken(context.Background(), "codex", teamName)
	if err != nil {
		t.Fatalf("the taken-over account's sign-in: %v", err)
	}
	if tok != fresh {
		t.Fatalf("token = %q, want the fresh one", tok)
	}
	// and the folder it came from is left as it was: it is read, never
	// written, so magpie's copy is the account's one holder
	b, _ := os.ReadFile(filepath.Join(profile, "auth.json"))
	var a codexAuth
	if json.Unmarshal(b, &a) != nil || a.Tokens.RefreshToken != "r-work" {
		t.Fatalf("the profile folder's sign-in was written to: %s", b)
	}
}

// The account is taken over once: a second call keeps the one magpie
// has, and the folder it came from is left alone.
func TestTakingOverTwiceKeepsMagpiesCopy(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	ForgetAccounts()
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over: %v", err)
	}
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over twice: %v", err)
	}
	ls := Logins("codex")
	var n int
	for _, l := range ls {
		if l.User == teamName {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the account is listed %d times: %+v", n, ls)
	}
}

// The quota of a taken-over account is read, so the rotation knows when
// its 5-hour window is spent.
func TestTakenOverCodexAccountReadsQuota(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	resetLoginUsage(t)
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	ForgetAccounts()
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over: %v", err)
	}

	fresh := fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())})
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/backend-api/wham/usage" {
			io.WriteString(w, `{"plan_type":"team","rate_limit":{"allowed":true,"primary_window":{"used_percent":100,"limit_window_seconds":18000,"reset_after_seconds":600}}}`)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"id_token": idTok, "access_token": fresh, "refresh_token": "r-work-2"})
	}))
	defer fake.Close()
	oldTok, oldBase := codexTokenURL, CodexBase
	t.Cleanup(func() { codexTokenURL, CodexBase = oldTok, oldBase })
	codexTokenURL, CodexBase = fake.URL, fake.URL+"/backend-api/codex"

	// the reading is asked for the first time now, with the fake vendor
	// answering
	resetLoginUsage(t)
	q := LoginUsage(context.Background(), "codex")[teamName]
	if q.Error != "" || len(q.Windows) == 0 {
		t.Fatalf("no quota for the taken-over account: %+v", q)
	}
	if q.Windows[0].Used != 100 {
		t.Fatalf("the 5-hour window reads %g, want 100: %+v", q.Windows[0].Used, q.Windows)
	}
}

// A taken-over account comes back to the folder it came from when magpie
// signs the agent in to another account, with the credentials magpie
// refreshed for it: a Codex run there is signed in again, and needs no
// fresh sign-in. That is the whole point of the handover.
func TestTakenOverCodexAccountLeavesItsFolderAlone(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	before, err := os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	ForgetAccounts()
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over: %v", err)
	}
	// magpie signs the agent in to it: the sign-in moves to ~/.codex
	if err := SwitchLogin("codex", teamName); err != nil {
		t.Fatalf("switching to the taken-over account: %v", err)
	}
	// and the profile folder is left as it was: it is read, never
	// written, so magpie's copy is the account's one holder
	after, err := os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the profile folder was written to:\n%s", after)
	}
	// and signs the agent back to the account it was on: the folder is
	// still as it was
	if err := SwitchLogin("codex", "me@example.com"); err != nil {
		t.Fatalf("switching back: %v", err)
	}
	after, err = os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the profile folder was written to on the way back:\n%s", after)
	}
	// one holder: ~/.codex holds the other account's sign-in
	main, _ := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	var m codexAuth
	if json.Unmarshal(main, &m) != nil || m.Tokens.RefreshToken != "r" {
		t.Fatalf("~/.codex/auth.json doesn't hold the account it is on: %s", main)
	}
}

// A taken-over account keeps its mark when the two records of it are
// folded into one: the account magpie took over, and the one read back
// out of the folder it came from. Without the mark the account is held
// in its folder again, and the sign-in cannot move to it.
func TestTakenOverCodexAccountKeepsItsMarkThroughTheFold(t *testing.T) {
	home := signIn(t)
	t.Setenv("CODEX_HOME", "")
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	idTok := fakeJWT(map[string]any{"email": "work@example.com",
		"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}})
	writeFile(t, filepath.Join(profile, "auth.json"), map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{"id_token": idTok,
			"access_token":  fakeJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "r-work", "account_id": "acct-work"},
	})
	writeFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	ForgetAccounts()
	if _, err := TakeOverCodexAccounts(context.Background(), nil); err != nil {
		t.Fatalf("taking the account over: %v", err)
	}
	// the account is read out of its folder again, as magpie looks at
	// the folders every so often: the two records of it fold into one
	// (dedupeLogins), and the mark has to survive the fold
	rememberLogins(true)
	ls := Logins("codex")
	var found *Login
	for i := range ls {
		if ls[i].User == teamName {
			found = &ls[i]
		}
	}
	if found == nil {
		t.Fatalf("the account isn't listed: %+v", ls)
	}
	// and the sign-in may still move to it: the folder holds the
	// sign-in as it was, but the account is magpie's
	if err := SwitchLogin("codex", teamName); err != nil {
		t.Fatalf("switching to the taken-over account: %v", err)
	}
}

// The fold of two records of one account keeps the take-over mark: the
// account magpie took over, and the one read back out of the folder it
// came from, are one account, and the mark is what tells them apart.
func TestTakenOverMarkSurvivesTheFold(t *testing.T) {
	auth := json.RawMessage(`{"tokens":{"id_token":"h.eyJlbWFpbCI6IndvcmsxQGV4YW1wbGUuY29tIiwiY2hhdGdwdF9hY2NvdW50X2lkIjoiYWNjdC13b3JrIn0.sig","account_id":"acct-work"}}`)
	ls := []savedLogin{
		{Agent: "codex", User: "work@example.com", Auth: auth, Taken: "/home/.cx/work", On: true, Seen: time.Now().Add(-time.Minute)},
		{Agent: "codex", User: "work@example.com", Auth: auth, Home: "/home/.cx/work", Seen: time.Now()},
	}
	out := dedupeLogins(ls)
	if len(out) != 1 {
		t.Fatalf("folded into %d records", len(out))
	}
	if out[0].Taken == "" {
		t.Fatalf("the take-over mark was lost: %+v", out[0])
	}
}
