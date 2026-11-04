package appdir

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The Codex folders multicodex keeps its accounts in are found where its
// own config says they are, and by the scan when it says nothing.
func TestMulticodexHomesIn(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	signedIn := func(rel string) string {
		dir := filepath.Join(home, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	work1 := signedIn("multicodex/profiles/work1/codex-home")
	work2 := signedIn("multicodex/profiles/work2/codex-home")
	// a profile Codex never signed in to holds no account
	if err := os.MkdirAll(filepath.Join(home, "multicodex/profiles/work3/codex-home"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the config says where the accounts are
	config := filepath.Join(home, "multicodex", "config.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"profiles":{
		"work1":{"name":"work1","codex_home":"`+work1+`"},
		"work2":{"name":"work2","codex_home":"`+work2+`"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got := multicodexHomesIn(home)
	want := []string{work1, work2}
	if !slices.Equal(got, want) {
		t.Fatalf("multicodexHomesIn = %v, want %v", got, want)
	}
	// and they are Codex folders of the user's, beside the main one
	all := CodexHomesIn(home)
	for _, d := range want {
		if !slices.Contains(all, d) {
			t.Fatalf("CodexHomesIn = %v, want it to hold %s", all, d)
		}
	}
}

// A config that can't be read means "unknown", not "none": the folders
// are found by the scan instead.
func TestMulticodexHomesWithoutAConfig(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	dir := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// a config that doesn't parse is no better than one that isn't there
	if err := os.WriteFile(filepath.Join(home, "multicodex/config.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := multicodexHomesIn(home)
	if len(got) != 1 || got[0] != dir {
		t.Fatalf("multicodexHomesIn = %v, want %v", got, []string{dir})
	}
}

// The name multicodex gives a profile is the name its config says, or
// the folder's own.
func TestMulticodexProfileName(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	dir := filepath.Join(home, "multicodex/profiles/work1/codex-home")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := multicodexProfileName(home, dir); got != "" {
		t.Fatalf("multicodexProfileName = %q, want none", got)
	}
	if got := multicodexProfileOf(dir); got != "work1" {
		t.Fatalf("multicodexProfileOf = %q, want work1", got)
	}
	if got := multicodexProfileOf(filepath.Join(home, ".codex")); got != "" {
		t.Fatalf("multicodexProfileOf = %q, want none", got)
	}
}
