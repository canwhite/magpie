# Post-Mortem: Codex pool switch at 85%

Date: 2026-11-10 · Plan: [codex-pool-switch-at-85.md](codex-pool-switch-at-85.md)

> **Objective (restated)**: for `make multicodex`, `make big-account` and `hybrid`, magpie reads
> each Codex account's remaining quota itself and, when an account passes 85% used, the next
> requests go to the next account in the pool. The threshold lives in one place,
> `~/.config/magpie/.env`. Then keep mitigating until no HIGH risk remains.

## Definition of "no HIGH risk" (measurable)

A risk is HIGH when `severity(1-5) × likelihood(1-5) × (1 - detectability(0-1)) > 12`.
Acceptance: the tracked register (plan, `## Risk Register`) has **0 rows with residual > 12**,
and every row names its mitigation and state. Measured result: **0 HIGH; highest residual 6.0 (R1)**.

## Acceptance criteria and result

| # | Acceptance criterion | Evidence | Result |
| --- | --- | --- | --- |
| AC1 | Rolling window moves at 85% used by default (`MAGPIE_SWITCH_LEFT=15`); 84.9% does not | `TestTheToldShareDecidesWhenRequestsMove` | PASS |
| AC2 | Env overrides .env: `MAGPIE_SWITCH_LEFT=10` moves at 90%, not 89.9% | same test | PASS |
| AC3 | Only whole-account windows count; a model's window is ignored | `TestSwitchShareReadsWholeAccountWindowsOnly` | PASS |
| AC4 | Codex: 5h at 85% moves; 5h at 84.9% stays; week at 90% does not move; week at 100% removes | same table | PASS |
| AC5 | Passed reset counts as empty; nothing read = unknown = stays | same table | PASS |
| AC6 | Switched seat goes after accounts with room, before an all-but-used account; 84% changes nothing | `TestSwitchedAccountGoesAfterRoom` | PASS |
| AC7 | A conversation on a switched account moves only when another account is known, under the line and not resting; if all are switched it stays | `TestAChatOnASwitchedAccountMoves`, `TestSwitchedElsewhere` | PASS |
| AC8 | Prior behaviour preserved: 98→tier2, 90 band kept for accounts and keys, `order`/`rotate` untouched | `TestSmartRouting`, `TestSwitchedSeatsGoAfterThoseWithRoom` | PASS |
| AC9 | The move is logged once per (pool, from, to) | `TestTheSwitchLineIsSaidOnce` | PASS |
| AC10 | One source of truth surfaced in CLI, `serve` line, GUI text (4 languages) and docs | CLI tests, i18n test, startup run | PASS |
| AC11 | Build, format, vet clean; affected packages green | evidence below | PASS |
| AC12 | Independent verifier verdict PASS | verifier audited this AC table and re-ran the commands; its runs are the raw outputs below | PASS |
| AC13 | Residual HIGH risk count = 0 | Risk Register | PASS |
| AC14 | Real account observed crossing 85% | not run (5h window needs hours; no account near line) | NOT DONE |
| AC15 | GUI browser suite | not run (`Cannot find module 'playwright'`) | NOT DONE |

## Deliverables (files changed)

- `internal/provider/switch_route.go`, `switch_route_test.go` — threshold decision.
- `internal/routes/routes.go`, `strategies.go`, `routes_test.go` — `Seat.Switched`, tier.
- `internal/gateway/routing.go`, `affinity.go`, `trace.go`, `switch_route_test.go` — ordering,
  affinity move, trace field, one-line log.
- `internal/cli/accounts_cli.go`, `providers_cli.go` — `switch-left` help/output, `serve` line.
- `internal/gui/assets/app.js`, `i18n.js`, `plugins/routing/client.js`, `plugins/routing/i18n.js`
  — four-language text, `switched` reason, low group wording.
- `docs/reference.md`, `docs/subsystems/gateway-routing.md`, `docs/subsystems/providers-accounts.md`.
- `docs/planning/codex-pool-switch-at-85.md` — plan, TODO, pre-mortem, post-mortem, register.

## What went wrong and what caught it

1. **Threshold window**: using `Seat.Used` would fire on the week window; caught in design, fixed by a
   dedicated `SwitchShareReached` over whole-account `Allowance` windows, with table tests.
2. **Tier change**: making the 90 band key-only turned `TestSmartRouting` red; reverted to keeping
   the band for accounts too, adding 85 as an extra tier.
3. **i18n edit**: a scripted replace split on the first colon inside `first: {who}`, producing a
   duplicate key tail; `node --check` caught it, tests then 4/4.
4. **Switch log never fired**: `who()` (`id@user`) was compared with `Seat.Key` (`id#user`); fixed by
   matching `rest`, with `TestTheSwitchLineIsSaidOnce`.
5. **Comment narrower than code**: `LowShare` comment said key-only; corrected.

## Known gaps (tracked, not HIGH)

- R3/R16 (4.0): single-token-holder plan not merged; rerun injected tests after it lands.
- R8 (3.6): fake-upstream tests only; real 5h crossing not run (AC14).
- R17 (2.7): Playwright missing here; run `make test-ui` before release (AC15).
- Plugin-served Codex would use the generic branch (`plugin:<id>`); unreachable today.

## Raw verification output

### Go

```
$ go build ./...
# github.com/yetone/magpie
exit=0
$ gofmt -l [touched]
exit=0
$ go vet -tags nogui ...
# github.com/yetone/magpie/internal/gui
# github.com/yetone/magpie/internal/gui
$ go test provider routes
ok  	github.com/yetone/magpie/internal/provider	4.720s
ok  	github.com/yetone/magpie/internal/routes	1.593s
$ go test gateway
ok  	github.com/yetone/magpie/internal/gateway	3.853s
```

### UI / CLI

```
$ node --test internal/gui/tests/i18n-keys.test.cjs
# tests 4
# suites 0
# pass 4
# fail 0
# cancelled 0
# skipped 0
# todo 0
# duration_ms 9971.376416
$ go test ./internal/cli
ld: warning: object file (/private/var/folders/_m/2c_dm44125lcydqtf7dwqgk00000gn/T/go-link-1945945373/000063.o) was built for newer 'macOS' version (26.0) than being linked (11.0)
ok  	github.com/yetone/magpie/internal/cli	30.651s
$ node --check (JS files)
all three parse
```

## Sign-off

- Independent verification subagent: **PASS** (targeted tests, i18n, register check).
- Residual HIGH risks: **0**. Highest residual: **6.0** (R1, documented and accepted).
