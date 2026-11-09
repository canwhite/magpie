package cli

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/provider"
)

// `magpie accounts off codex <email>` takes an account out of use; the
// account Codex is signed in to is paused rather than signed out, so a
// start that keeps only the other accounts can leave the machine's own
// sign-in where it is. `on` puts it back.
func TestAccountsOnOff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	jwt := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, plan, refresh string) map[string]any {
		return map[string]any{
			"auth_mode": "chatgpt",
			"tokens": map[string]any{
				"id_token":      jwt(map[string]any{"email": email, "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": plan, "chatgpt_account_id": "acct-" + email}}),
				"access_token":  jwt(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
				"refresh_token": refresh, "account_id": "acct-" + email,
			},
		}
	}
	// Codex's own sign-in, and one account multicodex holds
	own := filepath.Join(home, ".codex")
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(own, "auth.json"), auth("me@example.com", "plus", "r-me"))
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(profile, "auth.json"), auth("work@example.com", "team", "r-work"))
	writeJSONFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "takeover"}) }); err != nil {
		t.Fatal(err)
	}
	state := func() map[string]bool {
		b, err := os.ReadFile(filepath.Join(home, ".config", "magpie", "logins.json"))
		if err != nil {
			t.Fatal(err)
		}
		var ls []struct {
			User   string `json:"user"`
			On     bool   `json:"on"`
			Paused bool   `json:"paused"`
		}
		if err := json.Unmarshal(b, &ls); err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, l := range ls {
			if strings.HasPrefix(l.User, "me@example.com") {
				out["own-paused"] = l.Paused
			}
			if strings.HasPrefix(l.User, "work@example.com") {
				out["work-on"] = l.On
			}
		}
		return out
	}

	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "off", "codex", "me@example.com"}) }); err != nil {
		t.Fatal(err)
	}
	if s := state(); !s["own-paused"] {
		t.Fatal("off left the own account in use")
	} else if !s["work-on"] {
		t.Fatal("off took the multicodex account out too")
	}

	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "on", "codex", "me@example.com"}) }); err != nil {
		t.Fatal(err)
	}
	if state()["own-paused"] {
		t.Fatal("on left the own account paused")
	}

	// --except-multicodex does the same for every account magpie did not
	// get from multicodex's profiles: the machine's own sign-in here
	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "off", "codex", "--except-multicodex"}) }); err != nil {
		t.Fatal(err)
	}
	if s := state(); !s["own-paused"] {
		t.Fatal("--except-multicodex left the own account in use")
	} else if !s["work-on"] {
		t.Fatal("--except-multicodex took the multicodex account out too")
	}
	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "on", "codex", "--except-multicodex"}) }); err != nil {
		t.Fatal(err)
	}
	if state()["own-paused"] {
		t.Fatal("--except-multicodex on left the own account paused")
	}
}

// With no Codex account known, --except-multicodex refuses rather than
// saying every account is multicodex's: a start must not serve whatever
// the machine is signed in to when nothing could be told apart.
func TestAccountsExceptMulticodexWithoutAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "off", "codex", "--except-multicodex"}) }); err == nil {
		t.Fatal("--except-multicodex passed with no account to tell apart")
	}
}
