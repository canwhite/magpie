# Plan: Codex multi-account rotation across multicodex profiles

> **User's request (restated)**: The machine runs five signed-in ChatGPT/Codex
> accounts — four managed by multicodex (`work1`–`work4`, each in its own
> `CODEX_HOME` under `~/multicodex/profiles/<name>/codex-home`) and one the
> machine signs in on its own (`~/.codex`, a weekly-quota account). The user
> wants magpie to manage all five as one pool: when a 5-hour quota is used
> up, move to the next; the local account is used last, only when the four
> others have nothing left; and when the 5-hour quotas reset, go back to
> `work1`–`work4` (starting at `work1`). The user confirmed: magpie takes
> full ownership of the accounts; the local account goes last; and the
> 5-hour window is the window that drives rotation.
>
> **What this plan answers**: How magpie learns about the five accounts, what
> it has to change so it can rotate between them on a 5-hour window, and
> which parts of magpie already do most of this.
>
> Goal: magpie knows all five ChatGPT accounts, rotates Codex's sign-in
> between the four 5-hour accounts as their 5-hour windows fill, keeps the
> local weekly account for last, and returns to `work1` when the 5-hour
> windows reset.

## Context

The accounts on this machine, as they stand today:

| Account | Plan | Sign-in lives in | 5-hour window | Weekly window |
| --- | --- | --- | --- | --- |
| `enda@doinggroup.com` (work1) | team | `~/multicodex/profiles/work1/codex-home/auth.json` | yes | yes |
| `onlinefirst001@gmail.com` (work2) | team | `~/multicodex/profiles/work2/codex-home/auth.json` | yes | yes |
| `doingluning@gmail.com` (work3) | team | `~/multicodex/profiles/work3/codex-home/auth.json` | yes | yes |
| `starchsyrupmachine@gmail.com` (work4) | team | `~/multicodex/profiles/work4/codex-home/auth.json` | yes | yes |
| `yongye.zhengyy@gmail.com` (local) | self_serve_business_prolite | `~/.codex/auth.json` | yes | yes (the one the user means by "weekly") |

All five share one `chatgpt_account_id` (`e776f742-…`) but have distinct
`chatgpt_user_id`s, so each is its own ChatGPT user with its own allowance.

### What magpie already does

Most of the requested behaviour exists already, and the plan is mostly about
pointing it at the right accounts rather than writing a new rotator.

- **Many accounts per agent.** `internal/provider/logins.go` keeps a
  `logins.json` of saved accounts per agent (`savedLogin`), each with its
  own `Auth` blob. `Logins(agent)` lists them; `On` puts an account in use
  beside the agent's own.
- **Switching the sign-in.** `SwitchLogin` / `switchSavedLogin`
  (`internal/provider/logins.go:1141`) writes a saved account's credentials
  into the agent's own store (`~/.codex/auth.json` for Codex) and saves the
  ones it replaced, so one refresh token has exactly one holder.
- **Automatic failover.** `KeepOnAnAccountWithRoom`
  (`internal/provider/codex_switch.go`) runs every `loginSwitchEvery`
  (5 minutes) and calls `SwitchWhenSpent` → `NextLogin`, which moves the
  sign-in to the next account with room once the current one is spent at
  `SpentShare` (98%). This is the "used up → switch" behaviour asked for.
- **Reading the quota.** `codexWindowsRenewing` asks `/wham/usage` and
  returns the primary (5-hour) and secondary (weekly) windows
  (`internal/provider/subscription_usage.go:948`). `NoteCodexLimits`
  updates the reading from the `x-codex-*` headers each reply carries, so
  the cap holds the account from the next turn on.
- **Gateway failover.** A request for a Codex model goes through
  `codex/<model>` when more than one account is on (`codexAccounts`,
  `internal/gateway/codex_backend.go:712`), and routing sends it to the
  first account with room, then the next.
- **Ordering.** `SetAccountOrder` (`internal/provider/account_order.go`)
  sets the order routing and the sign-in follow, by dragging in the GUI or
  by `magpie accounts` — the order the user wants (`work1`→`work4`→local).

### The gap

`appdir.CodexHomes()` (`internal/appdir/codex.go`) finds Codex folders by
scanning the home folder for names starting with `codex`, plus one folder
down inside them. It finds `~/.codex` and would find `~/.codex-work` or
`~/.codex-profiles/b`. It does **not** find
`~/multicodex/profiles/work1/codex-home`: the folder is named `multicodex`,
its child is `profiles`, and the profile's home is two levels down under a
name (`codex-home`) that does not start with `codex`.

So today magpie sees only the local account, and the four team accounts are
invisible to it.

Two more facts shape the plan:

- **One refresh token, one holder.** Codex rotates the refresh token on
  every refresh. `switchSavedLogin` refuses to sign an agent in to an
  account that another Codex folder still holds
  (`codexHeldIn` → "is signed in to Codex in %s"), and `renewSaved`
  refreshes such an account *in that folder* instead of in a copy. If
  magpie takes a copy of work1's `auth.json` while multicodex's Codex also
  holds it, the two will sign each other out on the next refresh.
- **`codexHomeLogins` already knows how to read other folders.**
  `internal/provider/logins.go:741` reads each other Codex folder's
  `auth.json` and records the account with `Home` set — the account is
  listed and its quota is read, but magpie will not switch to it and does
  not refresh it in place; it refreshes it where Codex keeps it. That is
  exactly the "magpie only reads, multicodex stays the authority" mode.

## Goal

magpie holds all five accounts as one pool for Codex, with:

1. `work1`→`work2`→`work3`→`work4`→local as the routing and sign-in order.
2. Rotation driven by the **5-hour** window: an account whose 5-hour window
   is spent is passed over; the weekly window only removes an account
   entirely (it is then out of both).
3. The local account used **last**, only when all four team accounts have
   no 5-hour room left.
4. When the 5-hour windows reset, the sign-in returns to `work1` — the first
   in the order — without the user touching anything.
5. No refresh token with two holders, ever.

## Plan

### 1. Find the multicodex profiles (discovery)

Teach `appdir.CodexHomes()` about multicodex's layout, so the four team
accounts are found the same way `~/.codex-work` is found today.

- Read `~/multicodex/config.json` (the file `multicodex status` reads) and
  take each profile's `codex_home` from it. That is the authoritative list:
  multicodex knows where its profiles live, and magpie should not
  re-guess.
- Fall back to scanning `~/multicodex/profiles/*/codex-home` when the
  config can't be read — a failed read means "unknown", not "none"
  (code standards).
- Keep the scan as the last resort, so a layout magpie doesn't know still
  finds its accounts.
- The profiles are the *other* Codex folders: `CodexHomes()` returns
  `~/.codex` first (the local account), then the four profiles. The order
  that matters for rotation is the account order in `logins.json`, not this
  list, so the discovery order only has to be stable.

Source: `internal/appdir/codex.go`, new file `internal/appdir/multicodex.go`.

### 2. Decide who owns each account's sign-in (the refresh-token rule)

This is the decision that shapes everything else, and it is a **product**
decision, not a code detail: an account's refresh token has exactly one
holder at a time.

**Decided: magpie owns the four team accounts, and does not write back.**
magpie copies each profile's `auth.json` into `logins.json` and refreshes
the copy there. The profile's own `auth.json` is **not** written to again:
the refresh token in it is spent by magpie's refresh, and the file stays
as it was, for the user to read. `multicodex status` therefore shows those
accounts as they were, and `multicodex` needs a fresh sign-in
(`multicodex login <name>`) to use them again.

What that rules out, and why it is the right call:

- **No write-back.** A refresh writes only to `logins.json`. The profile
  folders are read, never written, so magpie never puts a token in a file
  another tool also writes. This is the same rule the codebase already
  keeps for `~/.codex`: one file holds a refresh token at a time.
- **The profile's file is left as it is, not emptied.** The takeover does
  not touch it: magpie reads the account out of it and keeps its own copy.
  A user who wants the account back in multicodex signs in there again.
- **A refresh in magpie spends the profile's token.** That is the cost of
  one holder, and it is the user's call to make (this plan).

**How it is done.** The account is marked as taken over
(`savedLogin.Taken`), and three readers answer to that mark:
`codexHeldIn` does not treat it as held in its old folder (so magpie may
switch to it), `codexHomeLogins` does not read it back out of that folder
(so it is not taken back), and `renewSaved` refreshes it in `logins.json`
through `renewTakenCodex` (never in the folder). `upsertLogin` keeps the
mark, and a new mark wins over an old one.

So the four team accounts are magpie's from `logins.json` on, refreshed
there, and switched to from there — `switchSavedLogin`'s normal path,
which refuses to sign an agent in to an account another folder still holds.
For that refusal not to apply, the profile folder must no longer hold the
sign-in: the takeover empties it, after copying it aside. That is the one
write to a profile folder, and it is a write that *removes* a token rather
than replacing one, so no two holders of one token ever exist.

### 3. Make the 5-hour window the rotation window

`NextLogin` (`internal/provider/codex_switch.go:145`) currently treats a
window as "spent" when *any* account-wide window is at or past the share
(`usedPast` checks every `QuotaWindow` with no model). For a team account
that means the weekly window can push it out of the rotation even while its
5-hour window is fresh — the opposite of what the user asked for.

- Add a window selector to the "is this account spent" test used for
  switching: the account is spent when its **5-hour** window is spent.
  The weekly window still removes the account when it is at 100%, because
  then the account really has nothing left.
- Keep this scoped to Codex's own sign-in switching. The gateway's routing
  (`Allowance.For`, `usedPast` in the gateway's fallback path) is a
  separate decision and is left alone unless the user asks for it.
- The account's windows are named by the vendor's reading:
  `quotaDurationName` gives "5 hours" and "7 days" / "weekly"
  (`internal/provider/subscription_usage.go`). `WindowCapID` already
  lowercases and trims a window name for exactly this kind of lookup, and
  `AccountCapsOf` reads a per-window cap by that name. The 5-hour window is
  found by span (5h) first and by name second.

Source: `internal/provider/codex_switch.go` (`usedPast`, `NextLogin`),
`internal/provider/subscription_usage.go` (window naming).

### 4. Order the accounts: `work1`→`work2`→`work3`→`work4`→local

The order is `logins.json`'s `Order` field per account
(`internal/provider/logins.go`), set by `SetAccountOrder` / `arrangeLoginsAs`.

- The order is the user's, not a code constant: set it once in the GUI's
  Accounts page (drag) or by CLI, and it is saved.
- The local account goes last in that order.
- `sortLogins` puts accounts with `Order == 0` after those with an order,
  alphabetically among themselves — so accounts not yet ordered land at the
  end. That is the right default for the local account, but `work1`–`work4`
  need explicit orders.
- `arrangeLoginsAs` refuses to order an account that is neither `Active`
  nor `On`, so the four team accounts must be **on** (`On`) before they
  can be dragged into place. `addLogin` sets `On` for a second account
  automatically; the import path sets it too.

### 5. Get the accounts into magpie

`magpie accounts takeover` (new: `internal/cli/accounts_cli.go` →
`provider.TakeOverCodexAccounts`, `internal/provider/codex_takeover.go`)
takes the accounts multicodex holds into magpie: each profile's sign-in is
read, kept in `logins.json` and refreshed there from then on.

- The profile folders are read, never written: the sign-in stays in the
  file as it was. `multicodex` needs a fresh sign-in for those accounts
  afterwards (`multicodex login <name>`), which the command says before it
  happens.
- The account is marked as taken over (`savedLogin.Taken`), so:
  - `codexHeldIn` does not treat it as held in its old folder, and magpie
    may switch to it;
  - `codexHomeLogins` does not read it back out of that folder and take it
    back;
  - `renewSaved` refreshes it in `logins.json` (`renewTakenCodex`), never
    in the folder, so the two never hold one refresh token.
- The local account is the agent's own sign-in; magpie reads it from
  `~/.codex` (`liveLogin("codex")`). It must be **on** and ordered last.
- Each taken-over account is `On`, so all five are in use and routing can
  reach any of them.

### 6. Return to `work1` when the 5-hour windows reset

**Already the case, once the accounts are ordered.** `NextLogin`
(`internal/provider/codex_switch.go:145`) walks the accounts that are on
(`spares`) in the order the user arranged them — `Logins(agent)` gives
them in that order (`sortLogins` by `savedLogin.Order`) — and takes the
first that has room. So the accounts turn over as a cycle on their own:
when work1's five hours reset, work1 is the first in the order with room,
so it takes over, and the cycle starts at the same place every time.

- The order is the user's: `SetAccountOrder`
  (`internal/provider/account_order.go`) writes it, by a drag in the
  Accounts page or by the CLI. work1→work2→work3→work4→local is one such
  order.
- `NextLogin`'s "back to the first" branch (`first != nil && first.On`,
  with `first` the account magpie left and that has room again) covers the
  one hop: work1 out → work2, and work1 back when it has room.
- The two together are the cycle: the spares walk takes the next one with
  room while the first is spent, and the "back" branch brings the first
  back once its five hours reset.
- **No new rotator.** `TestCodexAccountsTurnOverAsACycle`
  (`internal/provider/codex_cycle_test.go`) holds the order: it fails when
  the walk is reversed, and passes as it is.

Source: `internal/provider/codex_switch.go` (`NextLogin`,
`SwitchWhenSpent`), `internal/provider/logins.go` (`loginReturn`,
`sortLogins`), `internal/provider/account_order.go` (`SetAccountOrder`).

### 7. Make the 5-hour windows readable at the right time

The rotation needs the 5-hour window of every account, fresh enough to act
on:

- `Allowances(agent)` (`internal/provider/routing.go:500`) reads every
  account's windows at most once a minute, and `KeepOnAnAccountWithRoom`
  asks every 5 minutes. That is enough for a 5-hour window.
- A reply's own headers are the freshest source (`NoteCodexLimits`), and
  they update the account's windows as each request goes out — this is what
  keeps the cap from being overshot (#1295). For accounts that are *not*
  being sent requests, only the `/wham/usage` reading moves them.
- An account that is out of the rotation still gets read (its reading is
  what says it is out again when the window resets).

No new reading path is needed; the existing one covers all five accounts
once they are in `logins.json`.

### 8. The local account last, and the gateway's order

The gateway's routing order for the accounts is the same `Order`
(`internal/provider/account_order.go`, `SetAccountOrder` → `arrangeLoginsAs`
→ `savedLogin.Order`, read by `sortLogins`). The local account being last
in that order is what makes it "used last" for the gateway too.

- `NextLogin`'s spares are `Logins(agent)` entries with `On`, in order, so
  the sign-in also moves down the order.
- The gateway's own candidate order for accounts is the provider's account
  order (`internal/gateway/fallback.go`, `candidate.rank`), which follows
  the same list.

### 9. Make it visible and controllable in the GUI

The Accounts page already lists the accounts, their quotas and their order
(`internal/gui/assets/app.js`, the `accounts` card), and the order is a
drag. What is missing for this use:

- The 5-hour window's name should be legible as the window the rotation
  follows. `quotaDurationName` already produces "5 hours"; the GUI shows
  the window's name as the vendor's reading gives it.
- The account's own name (`magpie accounts alias codex <email> <name>`,
  `SetAccountName`) is how the user tells work1 from work2 in a list of
  five. Setting "work1"…"work4" on the four team accounts makes the page
  readable.

### 10. What the Codex app and CLI see

- The Codex CLI reads `~/.codex/auth.json`. When magpie switches, it writes
  the new account's credentials there — the CLI picks it up on its next
  start. A Codex session already running keeps the account it started with.
- The Codex **app** (ChatGPT desktop) and the Codex background
  app-server keep their own sign-in and show a stale account until they are
  quit and opened again (`CodexAppStale`, `noteCodexSwitch`,
  `internal/provider/codex_daemon.go`). magpie tells the user; it does not
  kill their app.

## Think — Debug Methodology

**Where to log.** At the boundaries, not in the business logic:

- `internal/provider/logins.go` — `rememberLogins` (which accounts were
  found, from where), `switchSavedLogin` (what was written where), and
  `codexName` (which name an account is kept under).
- `internal/appdir/codex.go` — `CodexHomesIn` (which folders were found and
  which were skipped, and why).
- `internal/provider/codex_switch.go` — `NextLogin` (which window was
  spent, which account was chosen, why the previous one was passed over).
- `internal/provider/subscription_usage.go` — `codexWindowsRenewing` (the
  vendor's raw reading, the window names and spans it produced).
- `internal/provider/logins_on.go` — `renewSaved` (a refresh: which folder
  it was written back to, which lock held it).

**What to read first.** The source, then the vendor's reading, then docs.

- The account's windows are whatever `/wham/usage` says today. Print them
  before assuming a window's name or span.
- `codexWindow.window()` (`internal/provider/subscription_usage.go`) is where
  the vendor's seconds become magpie's `Span`, `ResetsAt` and `Name`. The
  5-hour window's identity comes from there.

**How to localize.** Upstream first:

1. Is the account *found*? (`Logins("codex")`, `magpie accounts codex`)
2. Is its quota *read*? (`magpie quota --json`, `GET /api/login/usage`)
3. Is it *in use*? (`On` in `logins.json`)
4. Is the *order* right? (`Order` in `logins.json`)
5. Is the *switch* happening? (`SwitchWhenSpent`'s log line)

**What to grep for.** `[DEBUG-` is not this codebase's convention; magpie
uses `log.Printf` with the subsystem's name first (`"codex: ..."`,
`"logins: ..."`). The lines that matter are the ones named above.

**The failure to expect.** A refresh token with two holders. It shows up as
the vendor refusing a refresh ("it has been used since"), or one Codex
signing the other out. `sameRefresh` and `codexHeldIn` are the guards; a
new path that copies an `auth.json` out of a folder Codex still holds
breaks that guard. This is the reason step 2 exists.

## Do — Verification Strategy

**Build and static analysis** (the acceptance criteria in
[code standards](../code-standards.md#acceptance-criteria)):

```sh
gofmt -l internal/appdir internal/provider   # prints nothing
go vet ./...
GOOS=linux go vet -tags nogui ./...
GOOS=windows go vet ./...
go build
GOOS=linux go build -tags nogui
GOOS=windows go build
go test -tags nogui ./...
```

**Tests that fail without the change** (each one named for the guarantee it
holds):

- `internal/appdir`: a test that a multicodex `config.json`'s
  `codex_home` entries are found by `CodexHomesIn`, and that a config that
  can't be read falls back to the scan rather than reading as "no
  profiles". Red before the change, green after.
- `internal/provider`: a test that a Codex account whose 5-hour window is
  spent but whose weekly window is fresh is passed over (not switched to),
  and one whose 5-hour window is fresh is chosen. Red before.
- `internal/provider`: a test that the rotation's next target is the first
  account in the order with 5-hour room, so the sign-in returns to `work1`
  after its window resets.
- `internal/provider`: a test that the local account, last in the order, is
  chosen only when no team account has room.
- `internal/provider`: a test that a refresh of a team account writes the
  refreshed `auth.json` back to its profile folder, and that the write is
  atomic (never a truncated file).
- `internal/gateway`: a test that a request for a Codex model goes to the
  first account with 5-hour room, in the order.

**Runtime verification** (in a sandbox HOME, never the real `~/.codex`):

- `magpie accounts codex --json` lists all five, with the right plans and
  windows.
- `magpie quota --json` shows each account's 5-hour and weekly windows.
- `magpie accounts switch codex <email>` moves the sign-in, and
  `~/.codex/auth.json` holds the new account's credentials afterwards.
- The profile folders' `auth.json` are still valid after a refresh (parse
  them, check the refresh token is the fresh one).
- A real request through the gateway lands on the account with room
  (`magpie` traces).

**Done on this machine.** The takeover was run against the real profiles
and the order set, with a copy of each profile's `auth.json` kept aside
first:

```
$ magpie accounts takeover
✓ took enda@doinggroup.com · Team over
✓ took onlinefirst001@gmail.com · Team over
✓ took doingluning@gmail.com · Team over
✓ took starchsyrupmachine@gmail.com · Team over

$ magpie accounts order codex enda@doinggroup.com onlinefirst001@gmail.com \
    doingluning@gmail.com starchsyrupmachine@gmail.com yongye.zhengyy@gmail.com
1. enda@doinggroup.com · Team
2. onlinefirst001@gmail.com · Team
3. doingluning@gmail.com · Team
4. starchsyrupmachine@gmail.com · Team
5. yongye.zhengyy@gmail.com

$ magpie accounts codex
● codex   enda@doinggroup.com · Team      · team  5h 0% ↻4h59m  7d 15% ↻5d20h Tue 13:59
○ codex   onlinefirst001@gmail.com · Team · team  5h 0% ↻4h59m  7d 0% ↻6d23h Nov 11 17:20
○ codex   doingluning@gmail.com · Team    · team  5h 0% ↻4h59m  7d 1% ↻5d21h Tue 14:23
○ codex   starchsyrupmachine@gmail.com    · team  5h 0% ↻4h59m  7d 67% ↻23h18m tomorrow 16:39
● codex   yongye.zhengyy@gmail.com        · self_serve_business_prolite  7d 20% ↻6d0h Tue 18:07
```

All five accounts' quotas are read, the order is the one asked for, and
each profile folder's `auth.json` is byte-for-byte what it was before
(diffed against the copy kept aside).

**The released app strips the mark.** The running `magpie serve` is the
released v0.1.1561, which does not know the `taken` field and writes
`logins.json` without it, so the takeover has to be run with the new
build until this is released. The order and the accounts survive; the
mark does not.

**Logic correctness** — every execution path walked by hand:

1. All five accounts on, none spent: the sign-in stays on `work1`.
2. `work1`'s 5-hour window at 100%, `work2`–`work4` fresh: the sign-in
   moves to `work2`; `work1` stays in the list and is not forgotten.
3. `work1`–`work4` all at 100% on the 5-hour window: the sign-in moves to
   the local account.
4. `work1`'s 5-hour window resets, the sign-in is on the local account:
   the sign-in returns to `work1`.
5. A team account's *weekly* window at 100% while its 5-hour window is
   fresh: it is passed over (out of both), not used.
6. A refresh happens: exactly one file holds the new refresh token.
7. Two magpies run at once: only one refreshes; the other takes the fresh
   token (`codexMu` / `savedTokenMu`).

**GUI tests in both engines** (a change to `internal/gui/assets`):

```sh
make test-ui
BROWSER=webkit node --test internal/gui/tests/…
```

Plus the i18n suites in Chinese, English, `gui-zh-tw`, `gui-ja` and `gui-de`,
and the whole GUI suite, not only the accounts page's.

## Adjust — Rollback and Global Scan

**Rollback.**

- The account list is `logins.json`; `magpie accounts forget codex <email>`
  removes a saved account, and the user's own sign-in is only hidden
  (`side_logins.go`), never deleted.
- The profile folders' `auth.json` are restored by signing in again in
  multicodex (`multicodex login <name>`) — which is why step 2 writes back
  rather than leaving a stale token behind. **A copy of each profile's
  `auth.json` should be kept before the first takeover**, so a rollback
  does not depend on the vendor accepting a spent token.
- A single change of behaviour is a single flag to flip: the 5-hour
  window selector in step 3. With it off, the old "any window" rule
  applies.

**Global scan** — the same pattern elsewhere, so no latent twin is left:

- The 5-hour vs weekly distinction is made in more than one place:
  `usedPast` (switching), `usedUp`/`BackAt` (`codex_switch.go`), the
  gateway's own "is this account spent" test
  (`internal/gateway/fallback.go`, `capHold`, `HoldCaps`), and
  `Allowance.For` / `Allowance.heldUp` (`internal/provider/routing.go`).
  Each is a decision about a different thing; each is checked so the
  change is scoped to the sign-in switch and the others keep their
  meaning.
- `codexOtherHomes` / `codexHomeLogins` / `codexHeldIn` / `renewSaved` are
  the four places that know about "an account in another Codex folder".
  The write-back in step 2 goes through the same locks all four use.
- `appdir.CodexHomes` is read by `internal/sessions` (`CodexDirs`) too, so
  the four team accounts' sessions start being counted in the Requests
  page. That is a **visible change** and is named here, not left to be
  discovered.
- `internal/agent`'s Codex wiring reads `CODEX_HOME`; a Codex launched with
  `CODEX_HOME` set to a profile folder keeps that profile's sign-in, which
  is the one magpie writes back to.

**Backwards compatibility.**

- No file format changes: `logins.json` already holds the accounts and
  their order; `providers.json` is untouched.
- A machine with no multicodex is unaffected: the discovery finds nothing
  and behaves as today.
- A machine with multicodex but where the user wants multicodex to keep
  the accounts is unaffected *if* step 2's write-back is implemented as
  "refresh where the account is held" (decision (b)) rather than "copy into
  magpie" (decision (a)).

## The share the sign-in moves off at is the user's

The plan wrote the share down: an account is moved off with 15% of its
five-hour window left, so the next account has a window's worth of room to
be used in. That number is the user's, so it is not written in the code:

- **Where it is told.** `~/.config/magpie/.env`, as
  `MAGPIE_SWITCH_LEFT` for a window that runs over again within the day
  (Codex's and Claude Code's five hours) and `MAGPIE_SWITCH_LEFT_LONG` for
  one that doesn't (a week, a month). The environment has the last word
  over the file. `magpie accounts switch-left [<share>] [rolling|long]`
  says the shares and sets them.
- **The defaults.** 15% of a rolling window left, 2% of a long one.
- **How the windows are told apart.** By how long they run, not by name:
  a window that runs over again within the day is a rolling one, a longer
  one is a long one. So the rule reaches every subscription that has
  windows, not only Codex's.
- **The account comes back further from the edge.** 5 points beyond the
  share it was moved off at, so the sign-in does not go back and forth
  there. A share within 5 points of the whole comes back at the same share
  (it is held rather than flapping).
- **A change takes effect at once.** The `.env` is read again when the file
  has changed, and the sign-in's move is decided every five minutes.

`internal/provider/switch_config.go` holds it;
`internal/provider/switch_config_test.go` and
`internal/provider/switch_share_test.go` hold the guarantees, and the page
says the share the user told rather than one of its own.

## Verified on 2026-11-05
The plan is built. Each step was checked against the source and against the
machine it was built for:

| Step | Where it lives | Checked |
| --- | --- | --- |
| 1. The multicodex profiles are found | `internal/appdir/multicodex.go` | `TestMulticodexHomesIn`, `TestMulticodexHomesWithoutAConfig`; on the machine, `~/.codex` and the four profiles are found, each under the name multicodex gives it |
| 2. One holder of a refresh token | `internal/provider/codex_takeover.go` | `TestTakenOverCodexAccountIsMagpiesAlone`, `TestTakenOverCodexAccountIsRefreshedInMagpiesCopy`, `TestTakenOverCodexAccountLeavesItsFolderAlone` |
| 3. The 5-hour window drives the rotation | `internal/provider/codex_fivehour.go`, `spentAt` | `TestCodexSwitchesWithFifteenPercentLeft`, `TestCodexComesBackOnlyWhenThereIsRoom` |
| 4. The order is the user's | `internal/provider/account_order.go`, `magpie accounts order` | The order on the machine is work1, work2, work3, work4, local |
| 5. The accounts are in magpie | `magpie accounts takeover` | `TestAccountsTakeover`; all five accounts are in `logins.json`, on, with the four team ones marked `taken` |
| 6. Back to work1 when the window resets | `NextLogin` | `TestCodexAccountsTurnOverAsACycle` |
| 7. The windows are read | `internal/provider/subscription_usage.go` | `magpie quota codex --json` reads the 5 hours and the week of every one of the five |
| 8. The local account last | `savedLogin.Order` | The gateway's order is the same list (`alsoOn` reads `Logins`) |
| 9. Readable in the GUI | `internal/gui/assets/app.js` | The page says Codex moves on with 15% of its five hours left, in four languages; `TestCoreScriptBudget` and the i18n suite pass |
| 10. What the app and the CLI see | `internal/provider/codex_daemon.go` | `magpie accounts switch codex <email>` moves `~/.codex/auth.json`, and the profile folder it came from is byte-for-byte what it was |

**The sign-in was moved and moved back by hand** on the machine, to see
that the switch works and that no folder is written to: `enda@doinggroup.com`
→ `onlinefirst001@gmail.com` → `enda@doinggroup.com`, with
`~/.codex/auth.json` holding each account's credentials in turn and every
profile folder's `auth.json` unchanged.

**Run at this head** (2026-11-05, `ab19e9b6` plus the two fixes below):

- `gofmt -l` on the changed packages: nothing.
- `go vet ./...`, `GOOS=linux go vet -tags nogui ./...`, `GOOS=windows go vet
  ./...`: ok.
- `go build`, `GOOS=linux go build -tags nogui`, `GOOS=windows go build`: ok.
- `go test -tags nogui ./internal/appdir ./internal/provider ./internal/cli
  ./internal/gui`: ok.
- The full suite: the failures are `TestUnreadLoginsWriteErrorsKeepCredentials`
  (provider), `TestMarshalRawIsEncodingJSON`, `TestCodexFittedImagesPastTheBudget`
  and `TestServeWSLListensBesideLoopback` (gateway) and `TestPluginSOCKSProxy`
  (plugin). Each was checked at `main` and fails there too.
- The GUI suite's `i18n-keys` in Chromium: ok.

**Two fixes came out of the check:**

1. The `takeover` command said the sign-in "stays in its multicodex folder
   and is refreshed there", and that "no fresh sign-in is needed". The code
   does the other thing: the folder is read, never written, and magpie's
   copy in `logins.json` is the account's one holder of its refresh token.
   The command now says so, and says that the account needs a fresh sign-in
   in multicodex afterwards (`multicodex login <name>`) — which is what the
   plan's step 2 and the subsystem page say.
2. `ab19e9b6` grew `app.js` past its budget (`TestCoreScriptBudget`, in the
   GUI package, red on this branch and green at `main`). The budget was
   raised to the file's size, with the reason, as the standards ask.

**Still to do by hand:** the released app is v0.1.1561, which does not know
the `taken` field and writes `logins.json` without it, so the takeover has
to be run with a build that knows it until this is released. The order and
the accounts survive; the mark does not.

## The share, run and checked

The share the sign-in moves off an account at is now the user's, told in
`~/.config/magpie/.env` (see above). It was checked on this machine and
in the suite:

- **The command.** `magpie accounts switch-left` says the two shares and
  where they are kept; `magpie accounts switch-left 30` sets the rolling
  one and `magpie accounts switch-left 5 long` the long one, and both are
  read back by the next call. A share that isn't one is refused and
  nothing is written.
- **The sign-in follows it.** With `MAGPIE_SWITCH_LEFT=40` the sign-in
  moves off an account with 40% of its five hours left, where the
  default (15%) leaves it there (`TestTheToldShareDecidesWhenTheSignInMoves`).
- **The page says the share it is**, in all four languages, and a share of
  nothing (0) is said as 0 rather than left out: the payload's field is
  always written, since an omitted one would have the page fall back to
  the default (`TestProvidersSayTheShareTheUserTold`,
  `TestProvidersSayAShareOfNothing`).

**Run at this head** (2026-11-05, on top of `ab19e9b6`):

- `gofmt -l` on the changed packages: nothing.
- `go vet ./...`, `GOOS=linux go vet -tags nogui ./...`, `GOOS=windows go vet
  ./...`: ok.
- `go build`, `GOOS=linux go build -tags nogui`, `GOOS=windows go build`: ok.
- `go test -tags nogui ./internal/appdir ./internal/provider ./internal/cli
  ./internal/gui`: ok, except `TestUnreadLoginsWriteErrorsKeepCredentials`
  (provider), which fails at `ab19e9b6` too ("close of synctest channel
  from outside bubble") and is not this work's.
- The GUI suite's `routing-signin`, `switch-only`, `account-return` and
  `keep-login` in Chromium and WebKit, and `i18n-keys`: ok.

**One fix came out of the check:** the payload's share was left out when
it was 0, so a user who told "0" would have seen the default (15) on the
page instead. The field is always written now, and the two tests above
are red without it.

## Open Questions

**Answered while the work was done:**

1. **Does magpie write the refreshed sign-in back into the multicodex
   profile folders?** **No.** The user chose it: the folders are read,
   never written, so the two tools never fight over a refresh token.
   magpie's copy in `logins.json` is the account's one holder, and
   multicodex needs a fresh sign-in for those accounts
   (`multicodex login <name>`). The takeover says so before it happens.
2. **Does the local account take part in the rotation at all?** Yes: it is
   on, ordered last, and used when the four team accounts are out. It is
   not skipped because it is a different plan.
3. **Is the local account's own 5-hour window consulted?** Yes, as any
   other account's: it has both windows, so its 5-hour window can push it
   out too. It is last in the order either way.
4. **Should the rotation also change what the gateway sends, or only the
   sign-in?** Only the sign-in's order changes; the gateway's routing
   already sends to whichever account has room, and it follows the same
   order.
5. **What happens to multicodex itself?** It keeps its profiles and its
   folders, and its accounts need a fresh sign-in there
   (`multicodex login <name>`) once magpie has taken them over. Until
   then, `multicodex status` still shows them as they were.

## Out of Scope

- **Changing multicodex.** It is another tool; magpie reads it and (under
  decision (a)) writes back to it. Its own account rotation, `monitor` and
  `editor` are not touched.
- **Codex accounts other than these five.** The plan is about how magpie
  finds and orders accounts, not about adding accounts to ChatGPT.
- **The ChatGPT API subscription** (`ChatGPTAPIID`), Copilot, Gemini,
  Antigravity, Claude Code and every plugin's accounts. The 5-hour window
  is a ChatGPT/Codex notion; the other subscriptions keep their windows.
- **The Codex app's and the background app-server's own sign-in.** magpie
  tells the user they are stale; it does not quit them.
- **A new GUI page.** The Accounts page already lists the accounts, their
  windows and their order; this plan uses it and adds aliases and names.
- **A new rotator.** magpie's `KeepOnAnAccountWithRoom` → `NextLogin` →
  `SwitchWhenSpent` is the rotator, and it already turns the accounts over
  as a cycle in the user's order. The plan changes what it looks at (the
  5-hour window) and where the accounts come from (multicodex's profiles).

---

> Next step: run `/pre-mortem docs/planning/codex-multi-account-rotation.md`
> to identify failure modes and harden this plan.
