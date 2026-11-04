package appdir

// The Codex folders multicodex keeps one ChatGPT account each in.
//
// multicodex runs several Codex accounts on one computer, each in a
// folder of its own (CODEX_HOME set to it), so it can run one at a time
// or pick the one with room. It records where each account lives in
// ~/multicodex/config.json, under "profiles": each entry names a profile
// and the "codex_home" folder that profile's Codex runs in.
//
// That file is the authority: multicodex knows where its accounts are,
// and a folder it moves is one magpie follows. The scan below is the
// fallback for a config that can't be read or doesn't say, because a
// failed read means "unknown", not "none".

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// multicodexDir is the folder multicodex keeps its state in, in the
// user's home folder.
func multicodexDir(home string) string { return filepath.Join(home, "multicodex") }

// multicodexProfile is one of multicodex's profiles, as its config
// says it: the name it goes by and the Codex folder its account lives in.
type multicodexProfile struct {
	Name      string `json:"name"`
	CodexHome string `json:"codex_home"`
}

// multicodexConfig is multicodex's own config.json.
type multicodexConfig struct {
	Profiles map[string]multicodexProfile `json:"profiles"`
}

// multicodexHomesIn are the Codex folders multicodex keeps its accounts
// in, in the order its config lists them (by the names it gives them),
// each one that holds a sign-in.
func multicodexHomesIn(home string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) {
		if dir == "" || !filepath.IsAbs(dir) {
			return
		}
		if k := sameDir(dir); !seen[k] {
			seen[k] = true
			out = append(out, dir)
		}
	}
	// the config first: it is where multicodex says its accounts are
	if c, ok := readMulticodexConfig(home); ok {
		names := make([]string, 0, len(c.Profiles))
		for name := range c.Profiles {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			add(c.Profiles[name].CodexHome)
		}
	}
	// then the folders themselves, for a config that says nothing or
	// can't be read: multicodex keeps a profile's Codex folder under
	// profiles/<name>/codex-home
	entries, _ := os.ReadDir(filepath.Join(multicodexDir(home), "profiles"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		add(filepath.Join(multicodexDir(home), "profiles", e.Name(), "codex-home"))
	}
	// a folder Codex never signed in to holds no account
	return slices.DeleteFunc(out, func(dir string) bool { return !codexHomeAt(dir) })
}

// readMulticodexConfig reads multicodex's config.json. ok is false when
// it can't be read or doesn't parse: that means "unknown", not "none".
func readMulticodexConfig(home string) (multicodexConfig, bool) {
	var c multicodexConfig
	b, err := os.ReadFile(filepath.Join(multicodexDir(home), "config.json"))
	if err != nil {
		return c, false
	}
	if json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}

// multicodexProfileName is the name multicodex gives the account in
// home, as its config lists it: "work1". "" when it says nothing.
func multicodexProfileName(home, dir string) string {
	c, ok := readMulticodexConfig(home)
	if !ok {
		return ""
	}
	for name, p := range c.Profiles {
		if p.CodexHome == "" || sameDir(p.CodexHome) != sameDir(dir) {
			continue
		}
		if p.Name != "" {
			return p.Name
		}
		return name
	}
	return ""
}

// multicodexProfileOf is the name multicodex gives the account in one
// of its Codex folders, from the folder's own path: a profile folder is
// profiles/<name>/codex-home, so the name is its parent's.
func multicodexProfileOf(dir string) string {
	if filepath.Base(dir) != "codex-home" {
		return ""
	}
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "" || parent == "." || parent == string(filepath.Separator) {
		return ""
	}
	return strings.TrimSpace(parent)
}

// MulticodexHomes are the Codex folders multicodex keeps its accounts
// in, in the user's home folder.
func MulticodexHomes() []string {
	home, _ := os.UserHomeDir()
	return multicodexHomesIn(home)
}

// MulticodexProfileName is the name multicodex gives the account in one
// of its Codex folders, as its config says it: "work1". "" when it says
// nothing.
func MulticodexProfileName(dir string) string {
	home, _ := os.UserHomeDir()
	return multicodexProfileName(home, dir)
}

// MulticodexProfileOf is the name multicodex gives the account in one
// of its Codex folders, from the folder's own path: a profile folder is
// profiles/<name>/codex-home, so the name is its parent's.
func MulticodexProfileOf(dir string) string { return multicodexProfileOf(dir) }
