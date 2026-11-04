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

// `magpie accounts takeover` takes the ChatGPT accounts multicodex holds
// into magpie: each profile's sign-in is read and kept in logins.json, and
// the multicodex folder is left as it was.
func TestAccountsTakeover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	profile := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	jwt := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]any{
			"id_token":      jwt(map[string]any{"email": "work@example.com", "https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "team", "chatgpt_account_id": "acct-work"}}),
			"access_token":  jwt(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-work", "account_id": "acct-work",
		},
	}
	writeJSONFile(t, filepath.Join(profile, "auth.json"), auth)
	writeJSONFile(t, filepath.Join(home, "multicodex/config.json"), map[string]any{
		"version":  1,
		"profiles": map[string]any{"work1": map[string]any{"name": "work1", "codex_home": profile}},
	})
	before, err := os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	out, err := said(t, func() error { return accountsCmd([]string{"accounts", "takeover"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "took") || !strings.Contains(out, "work@example.com") {
		t.Fatalf("the takeover said: %s", out)
	}
	// the folder is left as it was
	after, err := os.ReadFile(filepath.Join(profile, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("the profile's sign-in was written to:\n%s", after)
	}
	// and magpie keeps the account
	b, err := os.ReadFile(filepath.Join(home, ".config", "magpie", "logins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "work@example.com") {
		t.Fatalf("magpie keeps no such account: %s", b)
	}
}

// writeJSONFile writes v as JSON at path.
func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
