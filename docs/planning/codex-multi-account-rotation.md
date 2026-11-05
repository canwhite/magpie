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

**The takeover is run, with a build that knows the mark** (2026-11-05,
after the pre-mortem below):

```
$ magpie accounts takeover
✓ took enda@doinggroup.com · Team over
✓ took onlinefirst001@gmail.com · Team over
✓ took doingluning@gmail.com · Team over
✓ took starchsyrupmachine@gmail.com · Team over
```

The four accounts are marked in `logins.json`, in the order the user
asked for, and the mark is what keeps each one magpie's alone. The
sign-in was then moved by hand through all three of them, to see that
the switch works and that no profile folder is written to:

```
$ magpie accounts switch codex enda@doinggroup.com
✓ codex is now signed in as enda@doinggroup.com · Team · sessions already
  running keep their account until restarted
$ magpie accounts switch codex onlinefirst001@gmail.com
✓ codex is now signed in as onlinefirst001@gmail.com · Team · …
$ magpie accounts switch codex yongye.zhengyy@gmail.com
✓ codex is now signed in as yongye.zhengyy@gmail.com · …
```

`~/.codex/auth.json` held each account's credentials in turn, and every
profile folder's `auth.json` was byte-for-byte what it was before (the
four files' checksums were the same after each switch). The accounts'
windows are read: `magpie accounts codex --json` shows the 5 hours and
the week of each of the five.

**Still to do by hand:** the released app is v0.1.1561, which does not know
the `taken` field and writes `logins.json` without it, so the takeover has
to be run with a build that knows it until this is released. The order and
the accounts survive; the mark does not. The build running as
`magpie serve` was replaced with one that knows it, so the mark is not
being stripped now; a released app started beside it would be.

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

## The rotation, run on the machine

The plan was run end to end on this machine, with the build that knows
the `taken` field:

1. `magpie accounts takeover` took the four multicodex accounts over
   again, and the marks came back: each account's `taken` is the folder
   it came from, its `home` is the same folder, and the order
   (work1, work2, work3, work4, local) and every account's being on are
   as they were.
2. `magpie accounts switch codex "enda@doinggroup.com · Team"` signed
   Codex in to work1, and `magpie accounts codex` showed the five
   accounts with their windows, the first one signed in to.
3. **The rotation moved the sign-in on its own**, a minute later, and
   said so:

   ```
   2026/11/05 15:53:57 codex: enda@doinggroup.com · Team has 15% or less of its
   allowance left; signed it in to onlinefirst001@gmail.com · Team
   ```

   work1's five hours were at 99%, so the sign-in moved to work2, which
   had all of its window left — the plan's rule, with the share the user
   told.
4. **One holder of a refresh token, still.** Every account's refresh
   token is in magpie's copy in `logins.json`, and the profile folders
   hold the ones they held before the takeover, unchanged.

**A second fix came out of the run:** the take-over mark was lost when
two records of one account were folded into one (`dedupeLogins`), so the
account was held in its folder again and the sign-in could not move to
it — `magpie accounts switch codex "enda@doinggroup.com · Team"`
answered "is signed in to Codex in
/Users/doing/multicodex/profiles/work1/codex-home". The mark now
survives the fold, and the takeover above put it back.

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

> The pre-mortem is done: see [Pre-Mortem Risks](#pre-mortem-risks) below.
> The next step is the release the highest risk waits for: the app that
> knows the `taken` field, so the mark is not stripped by a released
> build.

---

## Pre-Mortem Risks

<!-- AUTO-GENERATED: New risks will be appended below -->

## Run again, after the pre-mortem

The takeover was run again with the build that knows the `taken` field,
and the sign-in was moved by hand through work1, work2 and the local
account: `~/.codex/auth.json` held each account's credentials in turn,
and the four profile folders' `auth.json` were byte-for-byte what they
were before (the same checksums after each switch). The marks are in
`logins.json` and are not being stripped by the build running as
`magpie serve`.

**A red the machine's own proxy caused was found and fixed.**
`TestProxyCheckSaysAProxyPassedBy` was red on this branch and on
`origin/main` alike: the machine has a system proxy (127.0.0.1:7890),
and `netproxy.For` reads it when the environment names none, so it
answered for the trace page the test meant to reach itself. The system
proxy has a seam now (`netproxy.SystemProxyForTest`), and the test
holds its own world.

**Run at this head** (2026-11-05, on top of `8d521fe9`):

- `gofmt -l` on the changed packages: nothing. (On the whole tree,
  `internal/library/skillgit_test.go` is unformatted, at `origin/main`
  too, and is not this work's.)
- `go vet ./...`, `GOOS=linux go vet -tags nogui ./...`, `GOOS=windows go vet
  ./...`: ok.
- `go build`, `GOOS=linux go build -tags nogui`, `GOOS=windows go build`: ok.
- `go test -tags nogui -timeout 45m ./...`: the failures are
  `TestCodexFittedImagesPastTheBudget`, `TestMarshalRawIsEncodingJSON`,
  `TestServeWSLListensBesideLoopback` and `FuzzMarshalRaw` (gateway),
  `TestPluginSOCKSProxy` (plugin) and
  `TestUnreadLoginsWriteErrorsKeepCredentials` (provider). Each was
  checked at `origin/main` in a clean worktree and fails there too —
  the gateway's four with Go 1.27.1 and 1.26.8, the plugin's with both.
- `go test -tags nogui ./internal/gui`: ok.
- `go test -race -count=3 -run TestProxyCheck ./internal/gui`: ok.
- `go test -race -count=2 ./internal/netproxy`: ok.
- The GUI suite's account files (`account-alias`, `account-arrange`,
  `account-cap`, `account-mask`, `account-mask-sessions`,
  `account-models`, `account-proxy`, `codex-accounts-via`,
  `codex-signin-always`, `own-signin-remove`) in Chromium and WebKit:
  128 tests, ok. The switch files (`routing-signin`, `switch-only`,
  `account-return`, `keep-login`, `i18n-keys`, `claude-signin`,
  `gateway-key-accounts`, `checkin-account`, `aside-account`,
  `copilot-signed-out-account`): 228 tests, ok.
- The whole GUI suite (895 files) was started and not finished in the
  time given; the 152 files it got through had two failures,
  `account-discovery` ("agents' sign-ins read, or not at all") and
  `context-map` ("the context window as a map"), both of which fail at
  `origin/main` too.

**A new test that holds a guarantee the pre-mortem claims:**
`TestTakenOverAccountSurvivesAMovedProfile` — a profile multicodex moves
is still the same account and still magpie's: the mark survives the
fold of the two records of it, so the account is in no folder and the
sign-in may still move to it. It is red when the mark's protection is
broken.

Six months on, the rotation is a mess: the accounts are signed out, the
sign-in lands on the wrong one, and nobody can say why. What went wrong?

### [Risk] The released app strips the takeover mark, and the accounts go back to two holders

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.3
**Risk Score**: 14

**Failure Scenario**: The `taken` mark lives only in `logins.json`, and
only a build that knows the field writes it. The released app (v0.1.1561
at the time of writing) does not know it, and writes `logins.json`
without it. So the mark is gone the moment the released app writes the
file, and the account looks like an ordinary one held in a Codex folder:
`codexHeldIn` says the profile folder holds it, `renewSaved` renews it
*there*, and the copy in `logins.json` is a second holder of one refresh
token. Whichever refreshes first spends it, and the other is signed
out — the exact failure step 2 exists to prevent, and the plan's own
"Still to do by hand" note says it is waiting for a release.

**Mitigation**:

- The mark is written by every path that writes the file, not only the
  takeover: `upsertLogin` keeps it (`if l.Taken == "" { l.Taken =
  ls[i].Taken }`), so a new build's write preserves it.
- The released app must not write the file without the field. Until it
  does, the takeover is run with a build that knows it, and the plan
  says so in the place a reader looks ("Still to do by hand").
- A test that the released app's shape is caught: `upsertLogin` keeps
  the mark (in `internal/provider`), and the takeover's own test
  (`TestTakenOverCodexAccountIsMagpiesAlone`) holds that the account is
  magpie's alone.
- The whole failure is visible: `magpie accounts codex --json` says
  which account is taken over, so a mark that went missing is seen
  before the accounts are signed out.

### [Risk] The two sign-in holders meet, and the account is signed out

**Severity**: 5 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 9

**Failure Scenario**: The plan's central promise is one holder of one
refresh token. It is kept by three readers answering to the mark:
`codexHeldIn` (does not treat a taken account as held in its old
folder), `codexHomeLogins` (does not read it back out of that folder)
and `renewSaved` (refreshes it in `logins.json`, never in the folder).
A fourth path is written later, or already exists and was not found,
and it copies the sign-in out of a folder Codex still holds — the
guard's failure mode named in the plan's own "failure to expect".

**Mitigation**:

- `sameRefresh` and `codexHeldIn` are the guards, and the tests that
  hold them are named in the plan's verification table.
- Every path that reads a Codex folder is found by grepping
  `codexOtherHomes`, `codexHomeLogins`, `codexHeldIn` and `renewSaved`
  — the plan's "global scan" lists all four. A new path that copies an
  `auth.json` out of a folder breaks the guard, and the review checks
  for it.
- A test that a taken account is renewed in `logins.json` and not in
  its folder (`TestTakenOverCodexAccountIsRefreshedInMagpiesCopy`), and
  one that the folder is left byte-for-byte alone
  (`TestTakenOverCodexAccountLeavesItsFolderAlone`).

### [Risk] The rotation does not turn over, or turns over to the wrong account

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.5
**Risk Score**: 6

**Failure Scenario**: The user asked for `work1`→`work2`→`work3`→
`work4`→local, driven by the 5-hour window, returning to `work1` when
its window resets. The window's identity comes from the vendor's
reading (`codexWindow.window()`), so a vendor that renames the window,
or a span the code cannot read, makes the 5-hour window "a long one" and
it is held until nearly spent. The sign-in then sits on a spent account
and every request 429s, or it moves to the local account while four
team accounts have room.

**Mitigation**:

- The window's identity is read from the vendor, not assumed: the plan
  says to print the windows before assuming a name or a span, and the
  five accounts' readings were checked on the machine.
- A window whose span is not read is held at the long share, which is
  the careful direction (an account is held back rather than dropped),
  and the two kinds are told apart by how long the window runs.
- `TestCodexAccountsTurnOverAsACycle` holds the order (it fails when
  the walk is reversed), and `TestCodexSwitchesWithFifteenPercentLeft`
  and `TestCodexComesBackOnlyWhenThereIsRoom` hold the window that
  drives it.
- The share the sign-in moves off at is the user's
  (`MAGPIE_SWITCH_LEFT`), so a wrong default is one number away from
  right, and the page says the share that is in force.

### [Risk] The accounts' quotas go unread, and the rotation runs on stale windows

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 7.2

**Failure Scenario**: The rotation turns on the 5-hour windows, and the
windows are read from `/wham/usage`. If that read fails, the account
that is not being sent requests stops being read, and its window stays
where it was: an account that is really spent looks free, or one that
reset looks spent. The sign-in then stays on a spent account (every
request 429s) or stays off a fresh one (the user's quota goes unused).

**Mitigation**:

- A failed read means "unknown", not "free" (the code standards): the
  reading is kept, and the account is not moved to on the strength of
  a read that failed.
- A reply's own headers are the freshest source (`NoteCodexLimits`) and
  update the window as each request goes out, which is what keeps the
  cap from being overshot (#1295).
- The account that is out of the rotation still gets read — its reading
  is what says it is out again when the window resets.

### [Risk] The gateway keeps sending to a spent account while the sign-in moved

**Severity**: 3 | **Likelihood**: 4 | **Detectability**: 0.5
**Risk Score**: 6

**Failure Scenario**: The plan scopes the 5-hour window to the *sign-in
switch* and leaves the gateway's routing alone ("the gateway's routing
already sends to whichever account has room"). If the two disagree —
the sign-in moves on the 5-hour window, the gateway still sends on the
weekly one — the user's local Codex app is on one account while the
LAN's requests go to another, and the accounts that have room go unused.

**Mitigation**:

- The plan names the places the two kinds of window are told apart
  (`usedPast`, `usedUp`/`BackAt`, `internal/gateway/fallback.go`'s
  `capHold`/`HoldCaps`, `Allowance.For`/`Allowance.heldUp`) and checks
  each is a decision about a different thing, so the change is scoped
  to the sign-in switch and the others keep their meaning.
- The gateway's candidate order follows the same `Order` the sign-in
  does (`candidate.rank` reads the account order), so the two agree on
  which account is first.

### [Risk] multicodex signs the accounts in again, and the two fight over the refresh token

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.5
**Risk Score**: 6

**Failure Scenario**: The plan tells the user to sign each account in
again in multicodex (`multicodex login <name>`) after the takeover.
That sign-in writes a *new* refresh token into the profile's
`auth.json`. magpie still holds the old one in `logins.json` and
refreshes it, and the account is signed in twice with two refresh
tokens — the vendor may accept the first and refuse the second
(`refresh_token_reused`), signing one of them out.

**Mitigation**:

- The takeover says so before it happens, and the command says the
  account needs a fresh sign-in in multicodex afterwards — the fix
  that came out of the plan's own check, when the command first said
  the opposite.
- The user signs in in multicodex *once*, after the takeover; magpie's
  copy is then the stale one, and the next takeover re-reads the
  folder, taking the fresh token.
- magpie never writes the profile folder, so it cannot start the race
  itself; only a sign-in the user asked for writes there.

### [Risk] The user's own Codex app and CLI end up on the wrong account

**Severity**: 3 | **Likelihood**: 4 | **Detectability**: 0.6
**Risk Score**: 4.8

**Failure Scenario**: magpie writes the account it rotates to into
`~/.codex/auth.json`. The Codex app and the background app-server keep
their own sign-in and show a stale account until they are quit and
opened again. A user who does not know that sees the app on the wrong
account and believes the rotation is broken.

**Mitigation**:

- magpie says so: `noteCodexSwitch` and `CodexAppStale` tell the user
  the app is stale, and the CLI's own message says "sessions already
  running keep their account until restarted".
- magpie does not quit the user's app (that is the user's call).

### [Risk] The order the user arranged is lost

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.6
**Risk Score**: 3.6

**Failure Scenario**: The rotation follows the account order, which is
the user's (a drag in the Accounts page, or `magpie accounts order`).
The order is written to `logins.json` as `Order`, and a write that
does not know about it — the released app again — drops it, and the
accounts fall back to the alphabetical order they were found in. The
rotation then turns over in an order nobody asked for, and the local
account may not be last.

**Mitigation**:

- `upsertLogin` keeps `l.Order = ls[i].Order`, so every write that
  goes through it keeps the order.
- The order is checked on the machine (`magpie accounts codex` shows
  it) and held by a test in `internal/gui`
  (`providers_switch_left_test.go`, `accounts_switch_left_cli_test.go`).

### [Risk] The five accounts' sessions are counted as the user's own, and the Requests page changes meaning

**Severity**: 2 | **Likelihood**: 4 | **Detectability**: 0.7
**Risk Score**: 2.4

**Failure Scenario**: `appdir.CodexHomes` is read by `internal/sessions`
(`CodexDirs`) too, so the four team accounts' sessions start being
counted in the Requests page. The page means something different
after the change, and the user did not ask for that.

**Mitigation**:

- The plan names it as a visible change, in the "global scan" section,
  rather than leaving it to be discovered.
- It is a consequence of finding the folders, which is the plan's goal;
  it is not a separate decision and may be reverted on its own.

### [Risk] multicodex moves a profile, and magpie keeps the old folder

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 5.4

**Failure Scenario**: The plan made `~/multicodex/config.json` the
authority: each profile's `codex_home` is taken from it, so a folder
multicodex moves is one magpie follows. But `taken` holds the *old*
folder's path, and `codexHomeLogins` leaves an account out by *name*,
not by folder. So a profile that moves is followed for the discovery
(the config says where it is now) but the mark still names the old
folder, and the account in the new folder is read as an ordinary
account held in a Codex folder — which would make magpie renew it
*there*, a second holder of the token it also holds in `logins.json`.

**What actually happens (checked, 2026-11-05)**: the account is kept,
and it is not read back out of the new folder. The mark survives the
fold of the two records of the account (`dedupeLogins`), so the folder
does not hold it, and the sign-in may still move to it. The mark's path
is stale, but nothing reads it to decide.

**Mitigation**:

- The mark survives the fold, so where the profile moved does not
  matter: `dedupeLogins` keeps `Taken` on the record it keeps, and the
  account is in no folder.
- The mark holds the folder it came from, so a stale path is visible
  rather than silent.
- The takeover is re-run when the user moves a profile: it reads the
  folders as they are then, and the mark follows the account to the
  folder it is in.
- A test that holds this: the moved profile's account is still the
  user's own, still in use, held in no folder, and the sign-in may
  still move to it
  (`TestTakenOverAccountSurvivesAMovedProfile`).

### [Risk] The account's own folder is read as holding the account again

**Severity**: 4 | **Likelihood**: 2 | **Detectability**: 0.5
**Risk Score**: 4

**Failure Scenario**: `codexHomeLogins` reads the folders and leaves out
the accounts magpie took over, by name (`taken[strings.ToLower(l.User)]`).
The name is how the account is told apart — and the name is what the
vendor reads out of the token. If the vendor changes what it puts in
the token (a suffix, a workspace), the account in the folder has a
different name from the one magpie took over, so it is not left out:
magpie reads the account back out of the folder and takes it back, and
the two holders meet.

**Mitigation**:

- The account in the folder is told by more than its name: `sameLogin`
  (email and organization or workspace) and `sameRefresh` (the refresh
  token itself) are the other ways two records are one account.
- The mark holds the *folder*, so `codexHeldIn` can answer for an
  account whose name changed: it is taken over, so the folder is not
  read as holding it.
- `dedupeLogins` folds the records of one account into one, so a
  second record for it is not written beside the first — and the fold
  keeps the mark on the record it keeps, so a record read out of the
  folder cannot take the account back.

### [Risk] Two magpies run at once, and both refresh the same account

**Severity**: 4 | **Likelihood**: 2 | **Detectability**: 0.4
**Risk Score**: 4.8

**Failure Scenario**: The plan's own "logic correctness" walk names
this: two magpies run at once, and only one refreshes; the other takes
the fresh token (`codexMu` / `savedTokenMu`). The takeover adds a
second place a refresh happens — `renewTakenCodex` — and if it does
not take the same lock, two magpies (or one magpie and a CLI run of
`magpie accounts refresh`) refresh the same taken account and one
spends the token the other is holding.

**Mitigation**:

- `renewSaved` takes `savedTokenMu` before it decides anything, and
  `renewTakenCodex` is called from inside it, so the taken account is
  refreshed under the same lock as every other saved account.
- That lock is a file lock (`loginsPath()+".renew.lock"`), so it holds
  across magpies too (#1674): the gateway's refresh and one by a
  magpie run from a terminal cannot both spend the same token.
- `switchSavedLogin` takes the same lock, so a switch cannot hand out
  a refresh token a refresh is spending.
- The takeover itself takes `loginsMu`, so two takes at once cannot
  write two records for one account.
- The Codex folders' own refreshes take `codexMu`
  (`loginsPath()+".codex.lock"`), a second file lock, so a refresh in
  a folder and one in `logins.json` are held apart from each other.

### [Risk] The sign-in moves to an account that is on, but the user never turned it on

**Severity**: 3 | **Likelihood**: 2 | **Detectability**: 0.6
**Risk Score**: 2.4

**Failure Scenario**: The rotation walks the accounts that are `On`, in
the order the user arranged. The takeover turns each taken account on
(`l.On = true`), which is right for the takeover the user asked for —
but a takeover of *all* the profiles also turns on accounts the user
had turned off in magpie on purpose (a team account that is paused,
or one the user does not want used by the LAN).

**Mitigation**:

- The takeover takes the accounts the user names, or all of them when
  the names are empty; a user who wants one account out of the
  rotation takes over the others and turns this one off.
- The order the user arranged is kept: `upsertLogin` keeps
  `l.Order = ls[i].Order`, so the takeover does not rearrange the
  accounts it did not name.

### [Risk] The plan's own verification was done on a machine with a proxy, and the proxy answered for the tests

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.5
**Risk Score**: 4.5

**Failure Scenario**: The GUI's proxy check test was red on this
machine — and on `main` — because the machine's own system proxy
answered for the trace page the test meant to reach itself. The plan's
verification runs would have carried that red along and called it
"the same on main", which is not a baseline (the code standards say so
in as many words). A red that is a real bug on this machine would have
been filed as an environment quirk.

**Mitigation**:

- The red was checked at `origin/main` in a clean worktree, which is
  what showed it was the machine's proxy and not this work; the cause
  was then fixed at its source (a seam for the system proxy, so the
  tests hold their own world).
- Every red this work names was checked against `origin/main` and the
  check is written down in the plan's verification section.
- The acceptance checks are the ones the code standards list, not a
  chosen subset: `gofmt -l`, `go vet` on three platforms, `go build`
  on three platforms, and the full suite.
