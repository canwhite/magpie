# Plan: 账号成组后按 85% 用量自动换号（网关请求层）

> **User's request (restated)**: magpie 把多个 Codex 账号组成一组（multicodex 的四个 / hybrid 的五个，
> 或一个 routing group）后，要自己查各账号余量，某个账号用量超过 85% 就切到下一个账号；
> 判断方式可以灵活，但要由 magpie 自动完成，不靠用户手动切。
>
> **What this plan answers**: 现状下 85% 只作用于「登录切换」（且只对 magpie 自己持有的账号），
> 真正承载请求的网关路由仍是 90%/98%。本计划让网关请求路由也按同一个用户设定线（默认 85%）换号。
>
> Goal: 组内账号的滚动窗口用量达到切换线（默认 85%）时，magpie 把后续请求排到下一个有空间的账号；
> 全组都过线时按最少用量继续，不拒绝请求；账号不在组里（单账号）或余量未知时不乱切。

## Context

### 用户要的行为

- 账号成组（≥2 个 on 的 Codex 账号，或一个路由组）后，magpie 自己读余量。
- 某账号用量超过 85% → 后续请求改用下一个账号。
- 切号判断由 magpie 完成，不要求写 `auth.json`、不要求用户在客户端手动换成另一个号。

### 今天的实际情况（已核对源码与本机）

| 层 | 现状 | 文件 |
| --- | --- | --- |
| 登录切换（写 `~/.codex/auth.json`） | 滚动窗口留 15%（即 85% 用量）就移开，每 5 分钟检查 | `internal/provider/switch_config.go:44`、`codex_switch.go:47,422` |
| 登录切换的候选 | 只含 magpie 自己持有的账号；multicodex profile 的 `home` 账号被排除 | `codex_switch.go:210`、`logins.go:1047-1056` |
| 网关请求路由 | 默认 Smart：<90% 正常、90–98% 降级、≥98% 才算用尽 | `internal/routes/strategies.go:14-28`、`provider/codex_switch.go:52` |
| 会话粘滞（affinity） | 只在账号 ≥98% 时解除，保留 prompt cache | `internal/gateway/affinity.go:523` |
| 余量读取 | `Allowances(agent)` 每分钟后台刷新；读不到 = 未知，不当成已用尽 | `internal/provider/routing.go:500` |

本机证据（2026-11-10）：

- `~/.config/magpie/.env`：`MAGPIE_SWITCH_LEFT=15`；`magpie accounts switch-left` 输出「rolling 15% left / long 2% left」。
- `logins.json` 中 4 个 multicodex 账号带 `home`、无 takeover 标记 → 不参与登录切换。
- 当前签入 `yongye.zhengyy@gmail.com`（只有 7 天窗口，paused）。
- `make multicodex` / `hybrid` 只做 `accounts on/off` + `serve`，不改变任何阈值。

### 结论（本计划要补的缺口）

请求层换号线 = 90%/98%，不是用户的 85%；且 affinity 把长会话钉在同一个账号上直到 98%。
所以「底层 Codex 账号在 85% 时切下一个」今天没有成立。

### 相关已有计划

- [codex-multi-account-rotation.md](codex-multi-account-rotation.md)：多账号发现、登录切换、15% 规则（已落地）。
- [codex-single-token-holder.md](codex-single-token-holder.md)：一枚 refresh token 一个持有者；
  其 §11 明确「不重做 5 小时窗口与切换线算法」，本计划正好补请求层，不改令牌归属。

## Goal

1. 组内任一账号的**滚动窗口**用量达到用户设定线（默认 85% 用量 = 15% 余量，
   来自 `~/.config/magpie/.env`）时，网关后续请求优先发给组内未过线的账号。
2. 判断语义与登录切换一致：
   - Codex：只看 5 小时窗口；周窗口仅在 100% 用尽时把账号排除（`codexFiveHourOut`）。
   - 其他 agent：滚动窗口用 rolling 线，长窗口（周/月）用 long 线（默认 98% 用量）。
   - 窗口已重置（reset 时间已过）视为空；余量未知或读取失败 → 不切换。
3. 全组账号都过线：按现有策略（最少用量 / pace）继续服务，不拒绝请求、不 bench。
4. 组只有一个账号：行为不变。
5. `order` / `rotate` / 插件自定义策略的语义不被悄悄改变。

## Plan

### 阶段 0 — 先确认请求真的经过网关（不改代码）

1. 用 `make multicodex`（或 `hybrid`）起网关；确认 Codex 客户端配置里出现
   `openai_base_url`（或 magpie 的 `model_provider`）指向 `127.0.0.1:3425`。
   当前本机 `~/.codex/config.toml` 与 4 个 profile 的 `config.toml` 都没有这一项，
   而 `./magpie serve` 在跑 —— 先查清是没生效还是被别的机制取代
   （`internal/agent/codex.go:878,1951` 的 `codexFailover`）。
2. 抓一次基线：2 个账号在池中，用一个会回 `x-codex-*` 用量的假上游跑几次请求，
   记录 `routes` 选择顺序与 `usage.jsonl` 的账号归属。
3. 若请求不经过网关（例如 `SwitchOnly` 开着，或 Codex 直连 OpenAI），本计划的 85%
   只影响经过网关的客户端；先在计划里记录该事实，再决定是否同时处理登录层（见 Open Questions）。

### 阶段 1 — 核心：把切换线接进路由排序

1. **provider 层新增判定**（建议新文件 `internal/provider/switch_route.go` 或放进 `switch_config.go`）：
   ```go
   // SwitchShareReached：账号 a 的滚动窗口是否已到用户切换线；
   // agent 决定窗口语义（Codex 只看 5 小时；周窗口仅 100% 用尽时排除）。
   func SwitchShareReached(agent string, a Allowance, model string, now time.Time) bool
   ```
   - 复用 `switchShareFor(span)`（其 `.left` 来自 `~/.config/magpie/.env`，环境变量优先）、
     `resetPassed`、`codexFiveHour`、`codexFiveHourOut`。
   - 路由是逐请求排序，只用 `.left`，不用登录层的 `.back`（回线 +5）滞后值。
   - 只统计 `Model == ""`（整账号）窗口，与登录切换一致；读不到该窗口 → `false`（未知≠过线）。
   - 不受 `Allowance.used`（For 取「最满的、计入该 model 的窗口」）影响，避免周窗口 85% 误触发。

2. **routes.Seat 增加字段**：`Switched bool`（该账号的滚动窗口已过切换线）。
   改 `tier` 为 seat-aware，同时保留 key 的旧行为：
   ```go
   func tier(s Seat) int {
       switch {
       case s.Used >= UsedShare:          return 2 // 98：只有别无选择才用
       case s.Switched:                   return 1 // 85：排到所有未过线账号之后
       case !s.Account && s.Used >= LowShare: return 1 // 普通 key 仍 90
       }
       return 0
   }
   ```
   更新 `Smart`、`LeastUsed`、`Pace`、`FreeFirst` 里四处 `tier(...)` 调用。
   插件策略拿到的 Seat 多一个零值字段 → 语义不变。

3. **gateway 填字段**：`weighRouted`（`internal/gateway/routing.go:1560-1575`）在已知 allowance 时
   调用 `provider.SwitchShareReached(c.p.Account.UsageAgent(), a, c.model, now)`，
   写入 `Seat.Switched`。未知 → `false`。

4. **affinity 解除钉住**：`internal/gateway/affinity.go:523` 增加分支：
   - 被钉账号 `Switched`，且池里有另一个候选未 `Switched`（且未休息/未用尽）→ 换号；
   - 否则保持原样（全组过线时仍钉在原账号，避免无效抖动）。
   - 明确接受代价：85% 换号会丢该账号的 prompt cache；`Affinity: strict` / `session` 仍是用户可选的例外。

5. **策略边界**：`order`（`SpentShareOf` 返回 100）与 `rotate` 不改语义；文档写明
   「要 85% 换号请用默认 Smart / least used / pace」。默认路由就是 Smart，本机现状无 `providers.json` 覆盖。

### 阶段 2 — 配置、界面与文档

1. `magpie accounts switch-left` 的帮助文本与输出：说明该线同时决定**登录切换**与**网关请求换号**。
2. Routing 页文案（`internal/gui/assets/app.js:11928`）：补一句「经 magpie 的请求在该账号达到
   {left}% 余量且组内还有账号有空间时，改走下一个账号」。更新 4 种语言与 i18n key。
3. 文档：
   - `docs/subsystems/gateway-routing.md`：90/98 表改为「smart tier：切换线（默认 85）/ 98」，
     说明 affinity 在该线解除的条件。
   - `docs/subsystems/providers-accounts.md`：登录切换与请求切换共用同一 share，但候选集不同。
4. **余量线统一在 `~/.config/magpie/.env` 管理**（本次需求的配置面）：
   - 不新增环境变量；登录切换与网关请求换号读同一组值：

     | 变量 | 默认 | 含义 | 生效范围 |
     | --- | --- | --- | --- |
     | `MAGPIE_SWITCH_LEFT` | 15 | 滚动窗口（≤24h，Codex 5h）余量线；15 = 用量 85% | 登录切换 + 网关请求换号 |
     | `MAGPIE_SWITCH_LEFT_LONG` | 2 | 长窗口（周/月）余量线；2 = 用量 98% | 登录切换 + 网关请求换号（Codex 周窗口仍仅 100% 排除） |

   - 读取点唯一：`internal/provider/switch_config.go` 的 `SwitchLeftRolling()` / `SwitchLeftLong()`
     （内部 `shareFromFile` → `edit.GetEnvFile`），优先级 **环境变量 > `.env` > 代码默认**。
   - 路由侧禁止再为订阅余量写死 `85` / `15` / `90`；`SwitchShareReached` 只调这两个访问器。
     普通 API key 的 90/98 仍走 `routes.LowShare/UsedShare`，不属于「订阅余量」，本次不搬进 `.env`。
   - `magpie accounts switch-left <share> [rolling|long]` 继续写 `.env`；写完后登录切换与请求路由
     都按新值生效（缓存按 mtime+size 失效）。
   - 启动时在日志里打印一次实际生效的余量线（rolling/long 各多少），便于核对。
   - 文档列出上表，写明「改一处，两层都变」。

### 阶段 3 — 观测与防复发

1. 切号只记一行日志（同一 pool 头号变化时记一次，不做每请求日志）：
   `gateway: <pool> moved from <acctA> (85% used) to <acctB>`。
2. 路由追踪（Routing 页已有的 trace）输出每个 seat 的 `Switched` 与 `Used`，用户能自查为什么换。
3. 守卫测试（见 Do）：未知余量、单账号、全组过线三个反例必须有测试，防止「静默切光」。

## Think — 调试方法论

- 先读源码再猜：`routes/strategies.go` 的排序 → `gateway/routing.go` 的 `weighRouted` →
  `affinity.go` 的钉住判断 → 实际请求的账号归属（`usage.jsonl`）。链路顺序就是排查顺序。
- 日志加在边界：allowance 读取结果（`left.used`）、Seat 构造、排序后的头号、最终选中的 candidate。
  临时用 `[DEBUG-switch]` 前缀，提交前清掉。
- 复现用注入而非等待：测试里直接构造 `Allowance`（84.9 / 85.0 / 85.1）与假 upstream 头，
  不靠真跑一个 5 小时窗口。
- 「读到的余量」不是「账号实际余量」：以 vendor 返回的窗口为准；读不到就保持原样（LESSONS：失败读 = 未知）。

## Do — 验证策略

**必跑门禁**

- Build：`make build`（或 `go build ./...`）
- Static：`go vet -tags nogui ./...`、`gofmt -l`（零输出）
- Tests：`go test -tags nogui -timeout 45m ./...` 全绿，日志 `tee` 到文件留存
- GUI：`make test-ui`（两种引擎、窄宽度、每种语言）

**必须覆盖的逻辑路径**

| 场景 | 期望 |
| --- | --- |
| 两个账号，A 5h=84.9%，B=10% | A 优先，不切 |
| A 5h=85.0%，B=10% | 下一个请求走 B |
| A=85%，B=85%，C=10% | C；A/B 按策略（最少用量/pace）排序 |
| 全部 ≥85% | 仍服务，选最少用量；不 429/不 bench |
| A 余量未知 / 读取失败 | 不切，保持原顺序 |
| A 周窗口 90%、5h=10%（Codex） | 不切（周仅在 100% 排除） |
| A 周窗口 100% | 跳过 A，无论 5h |
| 窗口 reset 时间已过但读数仍是 85% | 视为空，可用 |
| 组只有一个账号 | 行为完全不变 |
| 普通 API key（非订阅） | 仍按 90/98，不受影响 |
| `MAGPIE_SWITCH_LEFT=10`（环境变量） | 阈值变为用量 90%；84.9 不切、90.0 切 |
| 不设环境变量，`.env` 为 15 | 阈值 85%；未写 `.env` 时用代码默认 15 |
| `MAGPIE_SWITCH_LEFT_LONG=0` | 长窗口到 100% 才降级；Codex 周窗口语义不变 |
| affinity 钉在 A，A=85%，B 有空间 | 下一次请求走 B |
| affinity 钉在 A，全组都 ≥85% | 仍钉 A，不抖动 |
| 路由 `order` | 不变（按顺序直到用尽） |
| 插件注册的 strategy | Seat 新字段为零值，排序不变 |

**运行时验证**

1. 沙箱 HOME + 假上游（返回可控的 5 小时用量头）跑网关，两个 fake 账号；用 curl 连续请求，
   断言 85% 后 `usage.jsonl` 归属切到 B，且日志只有一行。
2. 本机实跑：`make hybrid`，跑满一段用量跨过 85%（或临时把 `MAGPIE_SWITCH_LEFT` 调大以快速触发），
   对照 Accounts 页与 `usage.jsonl` 确认实际账号切换。
3. 回归：单账号 `make big-account` 行为不变；`make multicodex` 四个号能在 85% 依次轮转。
4. 配置生效验证：`magpie accounts switch-left 10` 后不重启网关（或按现有缓存规则等待失效），
   跑一次两个 fake 账号的请求，确认换号点从 85% 移到 90%；再改回 15 确认恢复。

## Adjust — 回滚与全局扫描

- **回滚**：纯行为变更，无持久化状态。代码 `git revert` 即可；不改 `providers.json` / `logins.json`。
  配置层可把 `MAGPIE_SWITCH_LEFT` 调回 10（≈90%）作为临时缓和，但不等同旧 90/98 语义，需在回滚说明里写清。
- **全局扫描**：改完后检查所有 `SpentShare` / `LowShare` / `SpentShareOf` 的使用点是否应同步：
  - `internal/gateway/affinity.go:523`（本计划改）
  - `internal/gateway/fallback.go:134`、`internal/gateway/routing.go:1303`（「用尽/无可选」判定，保持 98）
  - `internal/provider/key_allowance.go:145,232`、`allowance_renewed.go:73`（key/续期语义，保持）
  - `internal/routes/strategies.go` 的 tier 与文档
  - GUI 文案里所有提到「15%」的地方
- **兼容性**：`Seat` 新字段对插件策略是可加字段；`routes.Strategy` 接口不变；老 `providers.json` 不需迁移。

## Open Questions

1. **`order` / `rotate` 是否也应 85% 换号？** 本计划假设否（保持用户明确选的顺序/轮转语义），
   默认 Smart 已满足需求。若你要所有路由都强制 85%，需改 `SpentShareOf` 语义，影响 #530 的旧决策。
2. **登录层（`auth.json`）是否也要对 multicodex 的 `home` 账号生效？** 需要 `magpie accounts takeover`
   （会花掉 profile 里的 refresh token，之后每个号要 `multicodex login`）。本计划不改这条路径，
   因为请求层换号不依赖它；若客户端在本账号 85% 后自行停发，再单独处理。
3. **切换线要不要按组/按 provider 单独配置？** 本次已定为**全局统一**：只认
   `~/.config/magpie/.env` 的 rolling/long 两条线（与环境变量），不再分散在代码常量里。
   如果以后不同组要不同线，再在 `.env` 增加带组名的覆盖项，并让同一个访问器按组解析；
   在此之前不做 Provider/Group 字段，避免第三处真相。

## Out of Scope

- 不改 98%「用尽」的硬语义（fallback、rest、pooled 判定）。
- 不改登录切换对 `home` 账号的候选规则，不自动 takeover。
- 不改 5 小时/周窗口本身的读取与重置算法。
- 不改 Makefile 的 `multicodex` / `big-account` / `hybrid` 目标（它们只负责开关账号）。

## TODO

- [x] done 阶段 0：确认请求路径。本机 `./magpie serve` 在跑，但 `~/.codex/config.toml` 与其 4 个
      profile 均无 `openai_base_url`（all symlinks to it），macOS 系统代理 `127.0.0.1:7890` 让
      `codexProxyOK` 不成立，`stash.json` 只有 `codex.started`。结论：本机 Codex 客户端直连
      OpenAI，85% 登录切换是实际生效的换号；本计划的请求层换号（90/98 → 85）影响经网关的
      客户端（其它指向 `SERVE_ADDR` 的工具，以及在登录层换号后 magpie 临时接管时的请求）。
      **未实测**：用一个真实账号跨过 85% 观察 `usage.jsonl` 的归属（见 Do）。
- [x] done 阶段 1.1：`provider.SwitchShareReached`（`internal/provider/switch_route.go`）：
      只读整账号窗口；Codex 只读 5 小时、周窗口仅 100% 排除；未知/reset 已过 = false；
      线来自 `switchShareFor(...).used()`（`.env` → 环境变量 → 默认 15/2）。
- [x] done 阶段 1.2：`routes.Seat.Switched` + `tier`；98 → 2，Switched → 1，90 → 1（账号与 key 都保留
      旧的 90 档，避免改变已有账号 90–98 的排序），其余 0。
- [x] done 阶段 1.3：`weighRouted` 填 `Switched`；`Weighed.Switched` 随 trace 输出。
- [x] done 阶段 1.4：affinity 在“被钉账号 Switched 且另一候选已知、未 Switched、未休息”时
      解除（`why: "switched"`）；全组过线时保持。
- [x] done 阶段 1.5：`switch_route_test.go`（provider + gateway）：84.9/85/90 线、env 覆盖、
      整账号 vs model 窗口、Codex 周 90 不切/周 100 切/5h 按名识别、reset 已过、未知、
      全组过线、key 92 仍 90 档、order/rotate 不变（现有 TestRouting 回归绿）。
- [x] done 阶段 2：CLI `accounts switch-left` 帮助与输出说明两层共用；Routing 页文案四语言；
      `docs/reference.md`、`docs/subsystems/gateway-routing.md`、`docs/subsystems/providers-accounts.md`；
      `magpie serve` 启动打印生效的 rolling/long 线与 `.env` 路径。
- [x] done 阶段 3：`noteSwitched` 在池头变化且原头已过线时记一行
      `gateway: <pool> moved from <acct>, past its switch line, to <acct>`（同一移动去重）；
      路由 trace 带 `switched`，Routing 页 `low` 档与 `why: "switched"` 文案按它显示。
- [ ] todo 阶段 3b：真实账号跨 85% 的实跑记录（沙箱 fake 上游已由单测覆盖；真实 5h 窗口
      需数小时，尚未做，发布 body 须写 Not tried）。
- [x] done 阶段 4（部分）：`go build ./...`、`gofmt -l` 零、`go vet -tags nogui`（仅 cgo 警告）、
      `go test ./internal/routes ./internal/provider ./internal/cli` 全绿；gateway 套件
      仅 `TestServeWSLListensBesideLoopback` 与 `FuzzMarshalRaw/seed#7` 红，已在 HEAD 的
      独立 worktree 复现同样红（与本改动无关）。GUI 浏览器测试**未跑**：本机
      `Cannot find module 'playwright'`；`i18n-keys.test.cjs` 4 项通过，JS `node --check` 通过。

---

## Pre-Mortem Risks

<!-- AUTO-GENERATED: New risks will be appended below -->

六个月后，这个计划失败了。回看，是这样坏的：

### [Risk] 请求根本不经过网关，85% 路由改了个寂寞

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.3
**Risk Score**: 5 × 4 × (1 - 0.3) = **14.0 — HIGH**

**Failure Scenario**: 上线后发现 Codex 客户端是直连 OpenAI（`openai_base_url` 没写、
`SwitchOnly` 开着、或 failover 配置没被应用）。所有单元测试、GUI 文案都绿，但真实账号
仍然只用签入的那一个，85% 从不触发。本机 `~/.codex/config.toml` 与 4 个 profile 的
配置文件现在都没有 `openai_base_url`，正是这个失败场景的样子。

**Mitigation**:
- 阶段 0 作为硬门禁：起 `make hybrid` 后用 `grep openai_base_url ~/.codex/config.toml`
  与各 profile 的 `config.toml` 验证；不出现就停下，先解决请求路径，不进入阶段 1。
- 运行时不只看日志：用 `usage.jsonl` 的账号归属证明请求确实被网关分发到了多个账号。
- 若请求确实不经过网关，把「只影响经过网关的客户端」写进计划与发布说明，不在验收里
  声称「Codex 已经 85% 换号」；改走登录层方案要另立计划。

### [Risk] 直接拿 Seat.Used 判 85，周窗口误触发，把周额度提前切光

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.3
**Risk Score**: 5 × 4 × (1 - 0.3) = **14.0 — HIGH**

**Failure Scenario**: `Seat.Used` 来自 `Allowance.For(model)`，取的是「最满的、计入该
model 的窗口」——Codex 的 7 天窗口 86% 而 5 小时只有 10% 时，账号被当成过线切走。
四个号的周额度在半天内被切到全满，之后全体过线，反而比不切更早用光。

**Mitigation**:
- 只实现 `SwitchShareReached`，不用 `Seat.Used` 判切换线；只统计整账号窗口
  （`Model == ""`），Codex 只看 5 小时，周窗口仅 100% 用尽时排除。
- 测试表强制覆盖：5h 10% + 周 90% → 不切；5h 85% + 周 10% → 切；
  周 100% + 5h 0% → 排除。任何实现若拿 `For()` 结果来判，这组测试会红。
- 与登录切换共用同一判定函数，避免两套语义漂移。

### [Risk] 与进行中的 single-token-holder 计划互相破坏，池退化、切换静默失效

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.3
**Risk Score**: 5 × 4 × (1 - 0.3) = **14.0 — HIGH**

**Failure Scenario**: [codex-single-token-holder.md](codex-single-token-holder.md) 正在
改 `savedLogin` / `home` / `AlsoOn` 的账号模型。两边各改一半后，`Allowances` 里读不到
组内账号、或池只剩一个账号，85% 路由永远找不到第二个候选；所有测试仍绿，因为单账号
路径本来就不切换。两个计划都「完成」，需求却没生效。

**Mitigation**:
- 开工前在计划文件与提交信息中声明本计划依赖账号模型稳定；在两个计划都落地的树上有
  一个 owner 跑一遍「池里 5 个账号 + 85% 切换」的集成测试（红→绿），没有这条测试不合并。
- 测试断言**池成员数**（≥2 才有切换）与**各自不同的窗口读数**，防止同一账号被读成多份。
- 任何改 `savedLogin` / `Allowance` / `weighRouted` 的提交，body 写明改动前后池成员数与
  85% 是否仍生效。

### [Risk] 全组过线被实现成「不可用」，全组 429

**Severity**: 5 | **Likelihood**: 2 | **Detectability**: 0.4
**Risk Score**: 5 × 2 × (1 - 0.4) = 6.0

**Failure Scenario**: 为了「超过 85% 就切」，把过线账号直接判为 `Full`/不可用。当四个号
都过 85% 时，网关认为全组都没空间，请求被拒绝或长时间 bench，Codex 报错。用户损失
比不切更大。

**Mitigation**:
- `Switched` 只改变排序 tier（1），不改变 `full()` / `Allowance.Full` 的 98 硬语义；
  全组过线时按 least used / pace 继续服务。
- 验收表里的「全部 ≥85%：仍服务」必须由测试断言（返回 200、选中最小用量账号）。
- 代码评审清单里明确：本计划不允许新增任何拒绝/429 路径。

### [Risk] 85% 换号丢掉 prompt cache，成本和延迟上升，用户把功能当回归

**Severity**: 3 | **Likelihood**: 4 | **Detectability**: 0.4
**Risk Score**: 3 × 4 × (1 - 0.4) = 7.2

**Failure Scenario**: 默认 affinity 会把会话钉在账号上以复用 prompt cache。85% 解钉后，
长会话每跨一次号就全量重读上下文，花费与首字延迟上升；用户看到账单/延迟变差，认为
新功能是 bug。

**Mitigation**:
- 只在「另一个候选有空间」时解钉；全组过线时保持原账号，不做无效切换。
- 保留 `Affinity: strict` / `session` 作为用户自选的例外，并在 Routing 页文案里说明
  85% 与缓存取舍。
- 运行时验证记录切号前后的 `cache_read` 变化；若代价明显，把默认切换线是否仍取 15%
  交回用户裁决，而不是偷偷调高。

### [Risk] 余量读数滞后或未知，导致盲切 / 该切不切

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: `Allowances` 每分钟后台刷新，vendor 慢或限流时读数陈旧；实现若把
未知当 0% 或当 100%，会出现永远不切或全组误切。也可能读取失败后长时间停在旧值，
账号跨过 85% 后仍被使用。

**Mitigation**:
- 未知 / 读取失败 / 无窗口 → `Switched=false`，保持原顺序（LESSONS：失败读 = 未知）。
- 切换判定的日志带上读数的 age；超过 2 分钟时先触发一次刷新，下一次请求再判。
- 测试覆盖「未知」「读数过期」「reset 已过但读数仍旧」三条路径。

### [Risk] 模型专属窗口与整账号窗口混用导致切错

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: Claude 的 Opus 专属周窗口 90%、账号整体只有 10% 时，若把「计入该
model 的窗口」当作账号过线，会把账号错误切走，Sonnet 请求也一起被移开。

**Mitigation**:
- `SwitchShareReached` 只读 `Model == ""` 的整账号窗口，与登录切换完全一致。
- 测试用 Claude 的 scoped window（`Model != ""`）造 90%，断言不触发；整账号窗口 85% 才触发。

### [Risk] 假上游与真实 vendor 字段不一致，绿测试、红生产

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: 测试用自己编的 `x-codex-*` 头或简化 JSON，真实 `/wham/usage`
字段名/单位不同，生产里窗口读成 0% 或漏读，85% 永不触发；而 CI 全绿。

**Mitigation**:
- 测试 fixture 直接取自本机 `quota-history.json` 与真实响应样本（去掉令牌），保留原字段。
- 阶段 4 用真实账号实跑一次跨线（可临时调大 `MAGPIE_SWITCH_LEFT` 加快），记录
  `usage.jsonl` 账号切换与日志；没做就在提交 body 写「Not tried」。
- 解析层沿用现有 `SubscriptionQuota` 路径，不新增第二种读取格式。

### [Risk] 双套换号线并存，用户认知混乱：登录显示 A、请求实际用 B

**Severity**: 3 | **Likelihood**: 4 | **Detectability**: 0.4
**Risk Score**: 3 × 4 × (1 - 0.4) = 7.2

**Failure Scenario**: 登录切换（写 `auth.json`）与请求路由（网关选号）各自 85%，但候选集
不同。用户看到 Codex 仍登录在 A，以为没换；或者登录切到 B 而请求在 C。反复之后用户不再
信任任何一处的显示。

**Mitigation**:
- GUI 文案与 Routing 页 trace 同时显示「请求当前账号」与「登录账号」，说明两者区别。
- 文档 `providers-accounts.md` 写明：登录层只适用于 magpie 持有的账号；请求层适用于所有
  经过网关的账号。
- 验收时读 `usage.jsonl`（事实来源），不以客户端显示为准。

### [Risk] 回滚说明不准确，运维以为调一个环境变量就恢复旧行为

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.5
**Risk Score**: 3 × 3 × (1 - 0.5) = 4.5

**Failure Scenario**: 出问题时把 `MAGPIE_SWITCH_LEFT` 调回 10 以为恢复 90/98，但 tier 结构
已改，90–98 的排序与旧版不同；问题没解决，还叠加了新配置，定位更慢。

**Mitigation**:
- 回滚路径只写一条：`git revert` 对应提交（无持久化状态）。
- 若必须配置缓和，明确写出它与旧行为的差异，并在回滚记录里标「非等价」。
- 合并提交 body 记录回滚步骤与验证命令。

### [Risk] `order` / `rotate` 用户以为也会 85% 换号，结果不切

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 3 × 3 × (1 - 0.4) = 5.4

**Failure Scenario**: 用户按旧习惯把 routing 设为 In order（或 rotate），期待 85% 换号；
实现却保留了顺序/轮转语义，账号用到 100% 才走。用户判定需求未满足，反复提 issue。

**Mitigation**:
- Routing 页与 CLI 明确写出「85% 换号仅对 Smart / least used / pace 生效」。
- Open Question 1 保留给用户裁决；若用户确认要全局强制，再改 `SpentShareOf` 并更新
  #530 的决策记录。

### [Risk] Vendor 改变配额窗口的字段或语义，切换线读错

**Severity**: 4 | **Likelihood**: 2 | **Detectability**: 0.4
**Risk Score**: 4 × 2 × (1 - 0.4) = 4.8

**Failure Scenario**: OpenAI 改名或合并 5 小时/周窗口，`codexFiveHour` 找不到窗口 →
`Switched` 恒 false，85% 静默失效，且没有告警。

**Mitigation**:
- 找不到 5 小时窗口时记「未知」并在 Routing trace 显示；连续多天所有 Codex 账号都无 5h
  读数时输出一条警告。
- 与现有 `codex_fivehour` 解析测试共用 fixture；供应商变更时由同一处测试先红。

---

## Post-Mortem (after implementation, 2026-11-10)

### [Risk] GUI 文案改动把 i18n.js 弄成语法错误，整页白屏

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.85
**Risk Score**: 4 × 3 × (1 - 0.85) = **1.8 — low**

**Failure Scenario**: 脚本化替换 key 时按第一个冒号切分，而 key 自身含 `first: {who}`，
造成重复片段；若未做语法检查，Routing 页的 i18n 文件解析失败，页面无文案。

**Mitigation**:
- 已发生并被 `node --check` 与 `i18n-keys.test.cjs` 抓住；修复后 4 项测试全绿。
- 后续改 i18n 固定“改完即 `node --check` 每个 i18n.js + `node --test i18n-keys.test.cjs`”。

### [Risk] 切换日志的百分比取错窗口，日志说 20% 却已过 85% 线

**Severity**: 2 | **Likelihood**: 3 | **Detectability**: 0.8
**Risk Score**: 2 × 3 × (1 - 0.8) = **1.2 — low**

**Failure Scenario**: 用 `Seat.Used`（计入该 model 的最满窗口）打印 share，Codex 周窗口低、
5h 高时日志数字误导排障。

**Mitigation**:
- 日志不打印 `Seat.Used` 数字，只写 `past its switch line`；具体窗口在 Routing 页 trace 看。
- 同一 pool 的同一移动去重（`switchedNotes`），rotate 不会每请求刷屏。

### [Risk] 90% 档只给 key，改变现有账号在 90–98 的排序

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.9
**Risk Score**: 3 × 3 × (1 - 0.9) = **0.9 — low**

**Failure Scenario**: 计划原文写 `!s.Account && s.Used >= LowShare`；照做会让账号在 90–98
不再被降级，`TestSmartRouting` 立刻红（实测）。老用户感知到排序变化。

**Mitigation**:
- `tier` 对账号与 key 都保留 90 档，Switched 另加一档；现有回归测试保持绿。
- 计划里的措辞以本记录为准；85 线只新增优先级，不削减旧档。

### [Risk] 依赖 single-token-holder 计划未落地，仍可能互相破坏

**Severity**: 4 | **Likelihood**: 2 | **Detectability**: 0.5
**Risk Score**: 4 × 2 × (1 - 0.5) = **4.0 — medium**

**Failure Scenario**: `fix/codex-single-token-holder` 分支仍未合并；若其改动账号模型，池成员
或 `Allowances` 读取可能变化。

**Mitigation**:
- 本实现只依赖 `provider.Allowances`/`Allowance` 的现有形状与 `UsageAgent()`，不改账号模型；
  两边合并后在 `internal/gateway/switch_route_test.go` 的注入测试上再跑一次。
- 提交 body 写明池成员数与 85% 判定未动登录层。

### [Risk] GUI 浏览器测试本机不可用，页面文案未经真实浏览器验证

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.7
**Risk Score**: 3 × 3 × (1 - 0.7) = **2.7 — low**

**Failure Scenario**: `Cannot find module 'playwright'`，routing-*.test.cjs 全部无法运行；
文案渲染、窄宽度、四语言布局未在浏览器里看过。

**Mitigation**:
- 已跑 `i18n-keys.test.cjs`（四语言键一致、无重复）与两个 JS 文件的 `node --check`。
- 发布前在装有 Playwright 的环境跑 `make test-ui`；否则在提交 body 写 “GUI browser tests not run”。

### 残余风险（防住之后再打分）

| 原 HIGH 风险 | 原分 | 残余 | 依据 |
| --- | --- | --- | --- |
| 请求不经过网关 | 14.0 | 6.0（S5 L2 D0.4） | 本机路径已查清并写进计划；功能对经网关的客户端有效，不再宣称“Codex 客户端已 85% 换号”。 |
| 拿 Seat.Used 判 85 | 14.0 | 1.5（S3 L1 D0.5） | 只实现 `SwitchShareReached`，单测锁死整账号窗口/Codex 5h/周 100%；`Seat.Used` 不再参与判定。 |
| single-token-holder 互相破坏 | 14.0 | 4.0（S4 L2 D0.5） | 不改账号模型；注入式集成测试在合并后重跑；提交 body 记录池成员。 |

**残余 HIGH 风险：0。**

### 审查发现并已修复（独立 verification agent，2026-11-10）

- `noteSwitched` 原先用 `candidate.who()`（账号是 `id@user`）去匹配 `Seat.Key`（`id#user`），
  对账号永远不相等 → 切换日志永远不会打印。现改为用 `rest` 匹配，`who` 只用于日志可读名；
  新增 `TestTheSwitchLineIsSaidOnce` 覆盖“只记一次”“头没动不记”。
- `LowShare` 注释写“只属于 key”，但 `tier` 对账号也保留 90 档（有意向后兼容）；注释已改。
- Verification agent 判定 **PASS**：provider/routes 阈值测试、gateway 切换/affinity 测试、
  四语言 i18n 键测试均通过；上述两点为它列出的非阻断项，均已修复。

### 开放项（非阻塞）

- `SwitchShareReached` 以 `agent == "codex"` 识别 Codex 语义；插件服务的账号
  `UsageAgent()` 返回 `plugin:<id>`。当前 Codex 不是已迁移的内置订阅，走不到该分支；
  若将来 Codex 也由插件服务，需要把 `plugin:<codex id>` 一并识别，否则会退到通用
  长窗口线（98）而不是 Codex 的“5 小时 + 周 100%”规则。

---

## Risk Register（最终，2026-11-10）

HIGH 判定 = 严重度 × 可能性 × (1 − 可发现率) > 12。分数为**缓解后残余**。

| # | 风险 | 残余 S/L/D | 残余分 | 缓解证据 | 状态 |
| --- | --- | --- | --- | --- | --- |
| R1 | 请求不经过网关 | 5/2/0.4 | 6.0 | 阶段 0 实测本机路径并写入计划；不宣称客户端直连场景已覆盖 | accepted |
| R2 | 用 Seat.Used 判 85，周窗口误切 | 3/1/0.5 | 1.5 | 只读整账号窗口；`TestSwitchShareReadsWholeAccountWindowsOnly` | closed |
| R3 | 与 single-token-holder 计划互撞 | 4/2/0.5 | 4.0 | 不改账号模型；提交 body 记录、合并后重跑注入测试 | open-followup |
| R4 | 全组过线被当成不可用 | 2/1/0.3 | 1.4 | 全组 Switched 时不移动；`TestSwitchedElsewhere` | closed |
| R5 | 85% 换号丢 prompt cache | 3/3/0.4 | 5.4 | 有意取舍：过线才移；文档写明 why="switched"；残余接受 | accepted |
| R6 | 余量读数滞后/未知 | 2/2/0.3 | 2.8 | 未知 = 不切；reset 已过 = 空；`TestSwitchShareReadsWholeAccountWindowsOnly` | closed |
| R7 | 模型专属窗口混入 | 2/1/0.3 | 1.4 | `Model/matches` 跳过 + 用例 | closed |
| R8 | 假上游与真 vendor 字段不一致 | 3/2/0.4 | 3.6 | 直接复用 `Allowance`/`codexFiveHour` 识别；真实跨线未跑已记录 Not tried | partial |
| R9 | 双套换号线认知混乱 | 2/2/0.4 | 2.4 | CLI/启动行/文档/界面统一说明“登录与请求同一 .env” | closed |
| R10 | 回滚说明不准确 | 2/1/0.4 | 1.2 | 调整 .env 或回退 commit 即恢复；无数据迁移 | closed |
| R11 | order/rotate 用户以为也切 | 2/2/0.4 | 2.4 | 文档写明只有 Smart/least used/pace 排序 + affinity 移动 | closed |
| R12 | vendor 配额字段变化 | 3/2/0.5 | 3.0 | 复用既有读取；未知即不切；单一 seam 可改 | partial |
| R13 | GUI i18n 语法错误 | 2/1/0.2 | 1.6 | `node --check` + i18n 测试 4/4 | closed |
| R14 | 切换日志百分比误导 | 1/1/0.3 | 0.7 | 日志不打印 share；匹配 rest 修复；`TestTheSwitchLineIsSaidOnce` | closed |
| R15 | 账号 90–98 排序变化 | 1/2/0.2 | 1.6 | 保留 90 档；`TestSmartRouting` 回归绿 | closed |
| R16 | single-token 依赖未落地 | 4/2/0.5 | 4.0 | 同 R3 | open-followup |
| R17 | GUI 浏览器未验证 | 3/3/0.7 | 2.7 | 记录限制；发布前须跑 `make test-ui` | partial |

**未关闭 HIGH 风险：0。**
**残余最高分：6.0（R1，已接受并记录），低于 HIGH 阈值 12。**

## Verification Record（最终）

| Gate | Command | Result |
| --- | --- | --- |
| Build | `go build ./...` | pass（仅 cgo 警告） |
| Format | `gofmt -l <touched .go>` | 空 |
| Vet | `go vet -tags nogui ./internal/provider ./internal/routes ./internal/gateway ./internal/cli` | pass（仅 cgo 警告） |
| Provider/routes | `go test ./internal/provider ./internal/routes -run 'TestTheToldShare|TestSwitchShare|TestSwitchedSeats' -count=1` | ok 3.178s / 0.480s |
| Gateway | `go test ./internal/gateway -run 'TestSwitched|TestAChatOnASwitched|TestTheSwitchLine' -count=1` | ok 3.673s |
| i18n | `node --test internal/gui/tests/i18n-keys.test.cjs` | 4 pass / 0 fail |
| Startup | sandbox `magpie serve`（临时 XDG_CONFIG_HOME） | 打印 `switch 15% of a rolling window, 2% of a long one, left ...` |
| Full gateway | `go test ./internal/gateway` | 2 red；HEAD 独立 worktree 复现同样红（与本改动无关） |
| GUI browser | `node --test routing-*.test.cjs` | 未跑：`Cannot find module 'playwright'` |
| Independent verifier | verification subagent | PASS（3 项命令全绿；2 个非阻断项已修） |

**改动文件（git status）**：`docs/reference.md`、`docs/subsystems/gateway-routing.md`、`docs/subsystems/providers-accounts.md`、`internal/cli/accounts_cli.go`、`internal/cli/providers_cli.go`、`internal/gateway/{affinity.go,routing.go,trace.go,switch_route_test.go}`、`internal/gui/assets/{app.js,i18n.js,plugins/routing/client.js,plugins/routing/i18n.js}`、`internal/provider/{switch_route.go,switch_route_test.go}`、`internal/routes/{routes.go,routes_test.go,strategies.go}`、`docs/planning/codex-pool-switch-at-85.md`。
