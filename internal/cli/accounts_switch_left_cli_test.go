package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
)

// The share the sign-in moves off an account at is the user's: it is kept
// in the .env beside magpie's own files, and read back from there.
func TestAccountsSwitchLeft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	// the default is said
	out, err := said(t, func() error { return accountsCmd([]string{"accounts", "switch-left"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "15%") {
		t.Fatalf("the share said: %s", out)
	}
	// and the user tells their own
	out, err = said(t, func() error { return accountsCmd([]string{"accounts", "switch-left", "25"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "25") {
		t.Fatalf("the share said: %s", out)
	}
	// it is kept in the .env beside magpie's own files
	b, err := os.ReadFile(filepath.Join(home, ".config", "magpie", ".env"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "MAGPIE_SWITCH_LEFT=25") {
		t.Fatalf("the .env holds: %s", b)
	}
	// and read back
	out, err = said(t, func() error { return accountsCmd([]string{"accounts", "switch-left"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "25%") {
		t.Fatalf("the share said: %s", out)
	}
}

// A share that isn't one is refused, and nothing is written.
func TestAccountsSwitchLeftRefusesWhatIsNotAShare(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	provider.ForgetAccounts()
	t.Cleanup(provider.ForgetAccounts)

	if _, err := said(t, func() error { return accountsCmd([]string{"accounts", "switch-left", "abc"}) }); err == nil {
		t.Fatal("a share that isn't one was taken")
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "magpie", ".env")); err == nil {
		t.Fatal("the .env was written")
	}
}
