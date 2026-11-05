package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The share the sign-in moves off an account at is the user's, and the
// page says the share it is: a share of nothing (0) is one the user may
// tell, and it must not be left out of the page's own words, which would
// have the page fall back to the default.
func TestProvidersSayTheShareTheUserTold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	config := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		share string
		want  float64
	}{
		{"MAGPIE_SWITCH_LEFT=30", 30},
		{"MAGPIE_SWITCH_LEFT=0", 0},
	} {
		if err := os.WriteFile(filepath.Join(config, ".env"), []byte(c.share+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(providersState())
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			SwitchLeft *float64 `json:"switchLeft"`
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.SwitchLeft == nil {
			t.Fatalf("%s: the page is told no share at all: %s", c.share, b)
		}
		if *got.SwitchLeft != c.want {
			t.Fatalf("%s: the page is told %g, want %g", c.share, *got.SwitchLeft, c.want)
		}
	}
}

// The page's own words are read from the same payload, so a share of
// nothing reaches it too: the JSON says 0, not nothing.
func TestProvidersSayAShareOfNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	config := filepath.Join(home, ".config", "magpie")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, ".env"), []byte("MAGPIE_SWITCH_LEFT=0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["switchLeft"]; !ok {
		t.Fatalf("the page is told no share at all: %s", w.Body)
	}
	if got["switchLeft"] != float64(0) {
		t.Fatalf("the page is told %v, want 0", got["switchLeft"])
	}
}
