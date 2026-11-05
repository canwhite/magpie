package provider

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The share the sign-in moves off an account at is the user's: it is told
// in the .env beside magpie's own files, and the environment has the last
// word.
func TestSwitchLeftIsToldInTheEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(switchLeftRollingEnv, "")
	os.Unsetenv(switchLeftRollingEnv)
	config := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ".env"), []byte(switchLeftRollingEnv+"=30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := currentSwitchConfig()
	if c.rolling.left != 30 {
		t.Fatalf("the rolling share is %g, want 30", c.rolling.left)
	}
	// and the account comes back further from the edge than it was
	// moved off at, so the sign-in does not go back and forth there
	if c.rolling.back != 35 {
		t.Fatalf("the share the account comes back at is %g, want 35", c.rolling.back)
	}
}

// The environment has the last word over the .env file: a magpie run from
// a terminal can say a different share than the file does.
func TestSwitchLeftEnvOverridesTheEnvFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(switchLeftRollingEnv, "40")
	config := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ".env"), []byte(switchLeftRollingEnv+"=30\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := currentSwitchConfig(); c.rolling.left != 40 {
		t.Fatalf("the rolling share is %g, want 40", c.rolling.left)
	}
}

// A share that isn't one is unknown: the default is kept, and the reason
// is said in the log. A failed read means unknown too, never empty.
func TestSwitchLeftThatIsNotAShareKeepsTheDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	for _, v := range []string{"", "abc", "-1", "101", "1e999"} {
		t.Setenv(switchLeftRollingEnv, v)
		c := currentSwitchConfig()
		if c.rolling.left != switchLeftRolling {
			t.Fatalf("%s=%q gave the rolling share %g, want the default %g", switchLeftRollingEnv, v, c.rolling.left, switchLeftRolling)
		}
	}
}

// The windows are told apart by how long they run: a rolling one (Codex's
// and Claude Code's five hours) at the rolling share, a long one (a week,
// a month) at the long share.
func TestSwitchShareIsToldByTheWindowLength(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv(switchLeftRollingEnv, "15")
	t.Setenv(switchLeftLongEnv, "3")
	c := currentSwitchConfig()
	if c.rolling.left != 15 || c.long.left != 3 {
		t.Fatalf("the shares are %+v, want a rolling one of 15 and a long one of 3", c)
	}
	if s := switchShareFor(5 * time.Hour); s.left != 15 {
		t.Fatalf("a five-hour window's share is %g, want 15", s.left)
	}
	if s := switchShareFor(7 * 24 * time.Hour); s.left != 3 {
		t.Fatalf("a week's share is %g, want 3", s.left)
	}
	if s := switchShareFor(30 * 24 * time.Hour); s.left != 3 {
		t.Fatalf("a month's share is %g, want 3", s.left)
	}
}

// A share within five points of the whole has no room for the account to
// come back further from the edge: it comes back at the same share, so the
// sign-in is held rather than flapping at the edge.
func TestSwitchShareNearTheWholeComesBackAtTheSameShare(t *testing.T) {
	s := shareOf(98)
	if s.back != 98 {
		t.Fatalf("the share the account comes back at is %g, want 98", s.back)
	}
}
