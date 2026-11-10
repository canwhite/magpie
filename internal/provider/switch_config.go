package provider

// How soon magpie moves an agent's sign-in off an account that is running
// out.
//
// The rule is the share of the account's window that is still left when
// the sign-in moves: with 15% left there is a window's worth of room for
// the next account to be of use, where a move at the last drop leaves the
// next account nothing. The share is the user's, so it is told in
// ~/.config/magpie/.env (MAGPIE_SWITCH_LEFT) rather than written here.
//
// The windows are told apart by how long they run, because that is what
// the rule is about: a rolling window (Codex's and Claude Code's five
// hours) runs over again and over within the day, so the sign-in moves
// early on it; a long one (a week, a month) is scarce and moves only when
// it is nearly spent. Each kind has a share of its own.
//
// The shares are read when the sign-in's move is decided, so a change in
// .env takes effect at the next look (loginSwitchEvery, five minutes), not
// at the next start.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/edit"
)

// The shares the sign-in's move is decided on when nothing says otherwise:
// the share left when a rolling window's account is moved off, and the
// share left when a long window's account is moved off.
//
// The rolling one is 15: the next account then has a window's worth of
// room to be used in. The long one is 2: a week's allowance is scarce, so
// the account is held until it is nearly spent.
const (
	switchLeftRolling = 15.0
	switchLeftLong    = 2.0
)

// The longest window that still counts as a rolling one: a window that
// runs over again within the day.
const switchRollingSpan = 24 * time.Hour

// The variables the shares are told in.
const (
	switchLeftRollingEnv = "MAGPIE_SWITCH_LEFT"
	switchLeftLongEnv    = "MAGPIE_SWITCH_LEFT_LONG"
)

// switchShare is the shares the sign-in's move is decided on: the share
// of the window left when the account is moved off, and the share left
// when the account it moved off comes back to.
//
// The two never overlap, so the sign-in does not go back and forth at the
// edge: the account comes back only once it has room again, which is
// further from the edge than the share it was moved off at.
type switchShare struct {
	// left is the share of the window left when the account is moved off.
	left float64
	// back is the share of the window left when the account comes back.
	back float64
}

// used is the share of the window used when the account is moved off, and
// when it comes back: the share left, taken from the whole.
func (s switchShare) used() float64 { return 100 - s.left }

// usedBack is the share of the window used when the account comes back.
func (s switchShare) usedBack() float64 { return 100 - s.back }

// switchConfig is the shares the sign-in's move is decided on.
type switchConfig struct {
	rolling switchShare
	long    switchShare
}

// switchConfigCache is the shares read from the .env last, the file it was
// read from and what that file was then: the file is read again when it
// has changed, so a change in it takes effect without a restart.
var switchConfigCache struct {
	sync.Mutex
	path string
	when time.Time
	size int64
	what switchConfig
}

// readSwitchConfig reads the shares the user told in the .env beside
// magpie's own files. The environment is applied over them (withEnv), and
// has the last word.
func readSwitchConfig() switchConfig {
	c := switchConfig{
		rolling: shareOf(switchLeftRolling),
		long:    shareOf(switchLeftLong),
	}
	if v, ok := shareFromFile(switchLeftRollingEnv); ok {
		c.rolling = shareOf(v)
	}
	if v, ok := shareFromFile(switchLeftLongEnv); ok {
		c.long = shareOf(v)
	}
	return c
}

// withEnv is the shares with the environment's over them: a magpie run
// from a terminal can say a different share than the file does, and the
// environment has the last word.
func (c switchConfig) withEnv() switchConfig {
	if v, ok := shareFromEnv(switchLeftRollingEnv); ok {
		c.rolling = shareOf(v)
	}
	if v, ok := shareFromEnv(switchLeftLongEnv); ok {
		c.long = shareOf(v)
	}
	return c
}

// shareOf is the share of a window left at v, and the share it comes back
// at: five points further from the edge, so the two do not overlap and
// the sign-in does not go back and forth at the edge. A share with no room
// for that (one within five points of 100) comes back at the same share,
// which the switch reads as "moves back at once", so the account is held
// instead of flapping.
func shareOf(v float64) switchShare {
	back := v + 5
	if back > 100 {
		back = v
	}
	return switchShare{left: v, back: back}
}

// shareFromEnv is the share the environment tells, and whether it said
// one that can be read.
func shareFromEnv(name string) (float64, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(v) == "" {
		// a variable set to nothing says nothing: it is not a share of
		// zero, which would move the sign-in off every account at once
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 || f > 100 {
		log.Printf("provider: %s=%q isn't a share of a window (0 to 100); the default is kept", name, v)
		return 0, false
	}
	return f, true
}

// shareFromFile is the share the .env beside magpie's own files tells, and
// whether it said one that can be read. A file that can't be read is
// unknown, not empty: the default is kept.
func shareFromFile(name string) (float64, bool) {
	path := switchEnvPath()
	v, ok := edit.GetEnvFile(path, name)
	if !ok {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 || f > 100 {
		log.Printf("provider: %s=%q in %s isn't a share of a window (0 to 100); the default is kept", name, v, path)
		return 0, false
	}
	return f, true
}

// switchShareFor is the share the sign-in's move is decided on for a
// window that runs for span: a rolling one (Codex's and Claude Code's
// five hours) or a long one (a week, a month).
func switchShareFor(span time.Duration) switchShare {
	c := currentSwitchConfig()
	if span > 0 && span <= switchRollingSpan {
		return c.rolling
	}
	return c.long
}

// SwitchLeftRolling is the share of a rolling window (Codex's and Claude
// Code's five hours) left when the sign-in moves off an account, as the
// user told it. The page says the share it is, not one of its own.
func SwitchLeftRolling() float64 { return currentSwitchConfig().rolling.left }

// SwitchLeftLong is the share of a long window (a week, a month) left
// when the sign-in moves off an account, as the user told it.
func SwitchLeftLong() float64 { return currentSwitchConfig().long.left }

// SetSwitchLeft tells the share of a window left when the sign-in moves
// off an account, in the .env beside magpie's own files. kind is
// "rolling" for a window that runs over again within the day (Codex's and
// Claude Code's five hours) and "long" for one that doesn't (a week, a
// month).
//
// The share is the user's, so it is told where they can change it: in the
// file, not in a setting of magpie's own.
func SetSwitchLeft(kind string, share float64) error {
	name, err := switchLeftName(kind)
	if err != nil {
		return err
	}
	if share < 0 || share > 100 {
		return fmt.Errorf("a share of a window is between 0 and 100, not %g", share)
	}
	path := switchEnvPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return edit.SetEnvFile(path, edit.KV{Path: name, Value: strconv.FormatFloat(share, 'f', -1, 64)})
}

// switchLeftName is the variable a window of kind tells its share in.
func switchLeftName(kind string) (string, error) {
	switch kind {
	case "rolling", "":
		return switchLeftRollingEnv, nil
	case "long":
		return switchLeftLongEnv, nil
	}
	return "", fmt.Errorf("%q isn't a window kind: rolling or long", kind)
}

// SetSwitchLeftIn tells the share in the .env at path too, when that file
// already names it, and says whether it did. A gateway started by `make
// serve` gets the project's .env in its environment, which has the last
// word, so a share told beside magpie's own files alone would not reach
// it. A .env that doesn't name the share is left alone.
func SetSwitchLeftIn(path, kind string, share float64) (bool, error) {
	name, err := switchLeftName(kind)
	if err != nil {
		return false, err
	}
	if share < 0 || share > 100 {
		return false, fmt.Errorf("a share of a window is between 0 and 100, not %g", share)
	}
	if _, ok := edit.GetEnvFile(path, name); !ok {
		return false, nil
	}
	return true, edit.SetEnvFile(path, edit.KV{Path: name, Value: strconv.FormatFloat(share, 'f', -1, 64)})
}

// SwitchLeftPath is the file the shares are told in, for the command to
// name.
func SwitchLeftPath() string { return switchEnvPath() }

// currentSwitchConfig is the shares the user told.
//
// The environment is read every time (a lookup is cheap), and the .env is
// read again when the file has changed since it was read last, so a
// change in it takes effect at once.
func currentSwitchConfig() switchConfig {
	path := switchEnvPath()
	info, err := os.Stat(path)
	when, size := time.Time{}, int64(0)
	if err == nil {
		when, size = info.ModTime(), info.Size()
	}
	switchConfigCache.Lock()
	defer switchConfigCache.Unlock()
	if switchConfigCache.path == path && switchConfigCache.when.Equal(when) && switchConfigCache.size == size {
		return switchConfigCache.what.withEnv()
	}
	switchConfigCache.what = readSwitchConfig()
	switchConfigCache.path, switchConfigCache.when, switchConfigCache.size = path, when, size
	return switchConfigCache.what.withEnv()
}

// switchEnvPath is the .env beside magpie's own files, where the shares
// are told.
func switchEnvPath() string {
	return filepath.Join(appdir.Config(), ".env")
}
