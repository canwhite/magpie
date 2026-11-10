# Lessons moved to the code standards

These were seen on three or more days. The rules and their evidence are in
[code-standards.md](code-standards.md); a review checks each one. This page
keeps the days each rule was seen, what was missed since, and the commits
done right. It is the detail behind the index in
[LESSONS.md](../LESSONS.md); it is not loaded as project instructions.

- **Never let magpie's default override a value the user set themselves**
  (10-05, 10-10, 10-17). On 10-17 5a2414acd brought every plugins.yml
  without `settled` up once, Essentials to All. A user who had run
  `magpie plugin preset essentials` themselves wrote the same file, and
  the body doesn't name them (#2749).
- **A checkout, worktree or log belongs to the session that made it**
  (10-09, 10-12, 10-17). On 10-17 another session checked out
  origin/main in 7660399a1's worktree mid-run, and another deleted the
  first part of 17557132c's GUI log.

- **A fix reaches every sibling of the bug** (10-05, 10-06, 10-07). Seen
  six times on 10-05. On 10-07 cd8d64a9's sub2api balance URL was left out
  of the hint shown after a failed read.
- **A change re-derives everything built on what it changes** (10-05,
  10-06, 10-07). On 10-07: click-scroll red since 5d871723, four GUI tests
  left red by page-only test runs, and ee2588c2's test race (3e38bd76).
- **A test that fails on main is a bug to fix now** (10-05, 10-06, 10-07).
  On 10-07 dcbae2ef, 76d82528, b2345078, 07e246e5 and e95852e3 each shipped
  past "fails the same on origin/main".
- **Merge only the reviewed head, and write what was run before the merge**
  (10-05, 10-06, 10-07). On 10-07 #1037 merged with no record.
- **Real use before building on it, or telling users it works** (10-05,
  10-07, 10-08). Acceptance criterion 5. On 10-08 bbf99143 "not tried on
  Linux" broke Linux vet with the WSL2 distro there, and #1185 was reviewed
  on macOS only.
- **GUI tests in both engines, at narrow widths, in every language**
  (10-05, 10-06, 10-08). Acceptance criterion 4. On 10-08 aff4f9f2's zh-TW
  missed later strings, and usage-ledger was red ~7h after 586f2bce.
- **A fix reaches every sibling** was missed again on 10-08: #1239 fixed
  Air 9h after a8290d27, and 173ab71e fixed Gemini bodies after #950.
  Done right on 10-08: a8290d27 itself, 9fcd332b (cmd.exe quoting at one
  choke point, tried on the Windows box), 74a809dc with 84f37ac9
  (re-derived account keys), #1271/#1275 (built-in and plugin together),
  3aeb7d1b (a refusal scoped to the model) and #1288.
- **10-09/10-10, criteria still missed.**
  - Sibling: 12c0550a said "Only aBlock had this" while #1445 (open for an
    hour) listed the class. 64179e73, 13356b9d and d7186e51 followed.
  - The suite: 2da8fbe4 ran six packages, not `go test ./...`. Its testenv
    exec broke internal/proc's TestNoCommandBypassesProc, red ~50 min until
    b934d64a.
  - Real use: a3a8e257 shipped GitHub plugin icons without loading the real
    list, and the owner found none showing (52f6b3a3). aa82288f's Volcengine
    plan windows went out "not tried against the real API". It was said
    honestly, and #1427 stays open.
- **Whatever a test starts ends when the test does** (10-06, 10-08,
  10-11). On 10-11 a CLI-identity ask outliving its test wrote into the
  next test's folder (#1524). 4ee3a76b fixed it in the gateway's TestMain
  only; 9c377231 fixed it for every binary through testenv's marker, but
  the gateway isolates itself without testenv and never set the marker, so
  #1524 came back on CI (a349de82). A guard test now fails if the gateway's
  TestMain doesn't set it.
- **A timing test leaves room on both sides** (10-08, 10-09, 10-11). On
  10-10 8424a004 assumed two concurrent handlers let go in order
  (3a1ade97). On 10-11 TestMaxRPM and the queued-stream tests (above).
- **A failed read, parse or hash means "unknown"** (10-05, 10-11, 10-12,
  10-13). On 10-12 7a9490638's IP allow list let every address in when
  settings.json couldn't be read (7d9382db2). On 10-13 #2017's patch read an
  unreadable codex-autoreset.json as "no reset spent" (e297a8ab2). When a
  shared reader is wrong, fix it, not a strict copy beside it.
- **Nothing that can block runs under a lock its reader needs** (10-06,
  10-08, 10-11, 10-13). 7cba316b9's timer went red under -race
  (a2afc1828); a fake Codex appended from two goroutines (a75de4c21).
- **Another app's values come from that app** (10-05, 10-06, 10-13).
  accc8c090 sent 127.0.0.1 where Claude Code sends localhost (e0ecf0161).
- **A test named for a guarantee fails when it breaks** (10-08, 10-12,
  10-13). bbe8825c0 (9dd5917a4) and ggbdpq's 44b5b2b96 and e21b1dbc1
  (cc93b566e) passed on the old code. Break it by hand, watch it go red.
- **A test never reaches the network** (10-08, 10-11, 10-13). TestPiPackage
  tested npm's latest pi (f230fa23b); tests waited 30s on models.dev
  (bde11c778).
- **A tag waits for a green run, and a tag is not a release** (10-05,
  10-08, 10-12, 10-13). v0.1.1192/1193 tagged one commit; v0.1.1213 and
  v0.1.1218 never published (#1846). `npm view --prefer-online` a plugin
  version before naming it.
- **A large commit has a body with what was run** (10-08, 10-12, 10-13).
  ea3d480ed and 1cd814004 (community, no record); 3ccbe6bc1's "push notes"
  hid two red GUI tests (2dcc51417, ba0712888).
- **10-11 to 10-13, criteria still missed.**
  - The suite: 6fa3109ea, 43d8a5633, 5617281f5 and cdb7b5ff7 ran chosen
    packages and broke guard tests elsewhere (494609866, 24146c031,
    c6de0d2a5).
  - GUI: 18e82adc6 left 8 GUI files red after a run cut short (054bdb8bb);
    e3a497d7a left gui-ja red ~3h (28aac86bc).
  - Sibling: 4ed379f11 left three deciders on the raw body (0c6752dbd);
    90ac1f038 mended StepCode only (cb003877f).
  - Timing: #1874's TestUsageIngestTouchesNoLimit stamped its calls a
    minute ago and went red a minute after midnight UTC (1acdf3a8f).
  - Whatever a test starts: Devin's held installer (2ff4ecb42) and a
    Claude sign-in's `claude auth login` run (ebbe76420, #1802) outlived
    their tests.
- **10-14, criteria still missed.**
  - A large commit has a body with what was run: 509328ccb and c1e354245
    pointed to "the push" for it. A pointer is not a record.
  - A tag is not a release: zed 0.1.7 and zen-free 0.1.2 to 0.1.4 were
    bumped and never reached npm (plugins#56). Done right: every mover min
    raised on 10-14 (0337ce4bc, 50929661f, bd4cb7a26, 2c18c697c,
    aae49d086) named a version npm had.
- **10-15, criteria still missed.**
  - A large commit has a body with what was run: 113 of the 120 commits
    pushed at 23:08 (728cb2d29 to a7a012202) end "Full suite:
    SUITE_LINE", a placeholder no one filled in. The batch's real record
    is in 386d341e7 alone. Before pushing, grep the bodies for a
    template's placeholder.
  - A tag is not a release, and a release is not Latest: v0.1.1255
    (a1f7b8c13) was published 25 minutes after v0.1.1256 (37fec1240), so
    GitHub marked the older one Latest and the site's downloads served
    it until the review moved the mark. Publish in tag order, or pass
    `--latest` to the newest.
  - A rerun's green hides the first try: a1f7b8c13's run is green on
    attempt 2, its attempt 1 red (#2390, fixed at its cause in
    4a6bf80f4). Read `attempt` before calling a run green.
  - GUI tests in every file a change can reach: three changes ran the
    GUI files they chose and left others red on main, which CI never
    runs. 64b0f3a20 (10-15) had the sidebar ask /api/plugin-pages, and
    menu-scroll's strict fixture threw on it, 24 of 24 red until
    a61a6f954 the next day. 8f55c27ad (#2396) held Settings' tabs at the
    top, and text-size's click landed under them, red ~5h until
    d3ca90afc. 94942f9f3 ran only the i18n files, and
    agent-connect-no-models went 8 of 16 red until cf21b71ff (#2435). A
    new API call or a layout change runs the whole GUI suite.
  - A Windows rule needs each test of that agent judged for the right
    system: 6e2aa8e32 (#2300) made ~\.dsh never gone on Windows, and two
    Mac-app dsh tests that didn't force goneOS went red on Windows CI,
    which now runs the Dsh tests, until ce1251cc9.
- **10-16, criteria still missed.**
  - GUI tests in every file a change can reach: 9b04c4e20 (#2118 hooks,
    v0.1.1273) ran the plugin GUI files and four more. plugins.js used a
    core word as its own, so i18n-keys was red under the tag until
    a4fa7bbd8 (#2586) 35 minutes later.
  - What was run at the pushed head: 1978eabe5's suite ran at a751f5282.
    1d6f461a9 and the 22:58 batch had added ~2.5KB of core script since,
    so TestCoreScriptBudget passed at the base and failed at the pushed
    head (1,426,102 > 1,426,000), red on CI for two runs until 81313b912
    (#2612). Run the size budgets again after a rebase.
  - Each commit's record is its own: 61ca3924c and 83f3316cf carry the
    same body word for word, so neither says which test guards which
    change.
  - A cancelled run tests nothing: 6b3ee5a03 and e243937ca never had a
    run of their own; the next push's run covered them.
- **10-17, criteria still missed.**
  - What was run at the pushed head: d4a1b9baa ran "this change plus the
    next commit's library edit". 252bc61ce said it was rebased on
    cb05dc2d1, but its parent 84cd686af changed internal/agent three
    minutes before, and the pair first ran together on CI. bad4508c4
    and bf931f82c ran at older bases too.
  - Each citation is checked: 04e8aa8f1 cites #2710 for a GUI red that
    is #2552.
- **Done right on 10-17.** The retire stack (0f1666b38 to de671719a)
  diffed staticcheck's U1000 against its base for dead code, b98f906b1
  was red on the old code for an own-account gap, and 8 subscriptions
  were moved with real accounts. Kiro, Factory and Zed were not, and
  Qoder answers 401 (plugins#181); both said. df3734b95 (#2627) fixed
  the shared reader with the reporter's fish_variables and ran real fish
  3.7.1. f550d3699 tried Bun's Claude Code with a real SOCKS proxy.
  48aae83b8 found an unmasked route to Jev by reading callers. 9f9bfc160
  scoped Codex's 272K window to Codex and checked an OpenAI model
  unchanged. c4aeaec74 checked npm before the Cursor min.
  2042123f4 (timonwong) and 992f288e2 (JJ-Lin) were ported with credit.
- **Done right on 10-16.** a751f5282 (#2567) found its 408 by injecting a
  250ms stall and was red with renewal turned off. 9b04c4e20 ran a real
  Codex Pro sign-in on every protocol. #1717 (deephbz) took four review
  rounds, each with a probe that lost a 1h cache write, and diffed the
  outgoing request against main through a recording proxy. 6641e1b0a
  fixed #2449 at its cause, red 3 of 3 with an injected pause.
  0cad76cd9 ran the whole GUI suite and filed each red. a15e57656 read
  OpenPencil's keys from its own source and ran its real binary.
  ee2a7c7e8 (ggbdpq) has a test red on the old code.
- **Done right on 10-14.** 86cc7ac56 (hhh2210) ran -race -count=20 and
  says its ordering was injected, not captured. f074996d9 named its load
  red's issue (#2066). bfef5d51b checked the reverted page's screenshots
  against v0.1.1231's assets. 21aa40285 ran the fixed test in four time
  zones.
- **Done right on 10-11 to 10-13.** 2dcc51417 found 3ccbe6bc1's reds the
  same evening. 5241de909 guards a caller's image URL at the one place all
  three draw paths download it. 86b2f4d05 fixed revoked sign-ins for every
  built-in at once. 8179ce768 says what it broke by hand. 097479dab fixed
  #1543 at its cause, and ebbe76420 forced each failure by hand instead of
  rerunning.
- **Done right on 10-10/10-11.**
  - 9e35eef3 fixed #1469 half an hour after it was filed, reproducing CI's
    numbers with an injected pause.
  - ede1e32a (#1508) audited all six pickers of the same kind and added a
    guard test that every one can be narrowed.
  - 64262a19 (Prime Agent), 9c11abbd (jcode) and b72e7d4a (#1539) were tried
    with the real binary in a sandbox, and each says what was broken by hand.
  - #1197 scoped Kimi's sampling rule to Kimi Code's endpoints and models.
- **Done right on 10-09/10-10.**
  - 229ce19a, 570b8fdb and ab72c8eb reproduced CI flakes by injecting a
    delay and fixed their cause.
  - 7f62dbf3 named the siblings the reporter's patch missed (Cursor, Muse,
    GUI, CLI) and covered them.
  - 9b51428e found the same hold-out in Factory's list.
  - #1454 stayed open until the reporter confirmed.
  - #1213 and #1282 merged at the reviewed head with the record seconds
    before.
