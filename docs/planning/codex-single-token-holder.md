# Plan v2: 一个刷新令牌，一个持有者，一个刷新者

> **状态**：2026-11-09 裁决后重写（v2.1）。v1（同文件早前版本）的前提"magpie 接管并在
> profile 目录就地刷新"已被 11-09 的事故与 codex-cli 0.154.0 的实测证伪，
> 差异见 §9。**先跑 §4 的实验门，过门才动真实账号。**
>
> v2.1 的补强：职责边界（INV-7/8）、刷新前准入检查（G1）、池空窗期压到最短
> （阶段 0.6）、回滚不再靠恢复令牌副本、Q1 多一条退路。

## 0. 这个计划回答什么

用户的原始请求（`painpoints/pp0.md`）：本机有 5 个 ChatGPT/Codex 账号（4 个在
multicodex 的 profile 里，1 个在 `~/.codex`），要用 magpie 统一管理，5 小时额度
用完自动切下一个。

`painpoints/pp1.md` 记的是事故后仍存的风险：**两个进程写同一个 auth.json**。
令牌轮换后，后刷新的一方吃到 `refresh_token_reused` / `refresh_token_invalidated`，
会话作废。pp1 要求的落点是：**magpie 刷新令牌前后都重新读盘，不拿内存里的旧值
去刷新**。

事故报告：[docs/improve/multicodex-token-dual-holder.md](../improve/multicodex-token-dual-holder.md)。

Goal：**每个 ChatGPT 账号的每一枚刷新令牌，全机只有一个持有文件、只有一个刷新者。**
做到这一条，"一个令牌被两处持有"这类事故在结构上不再可能发生。

Scope：magpie 侧（`internal/provider` 的 Codex 登录、刷新、切换、账号归属；
`internal/gateway` 的 Codex 池）+ 这台机器上的 5 个真实账号 + 防复发的守卫与测试。

## 1. 今天量到的事实（不是推断）

### 1.1 双持有仍然存在

`~/.config/magpie/logins.json` 里 4 个账号的 `auth.tokens.refresh_token` 与各自
profile 的 `auth.json` **是同一枚**（前 12 位）：

| 账号 | profile 的 auth.json | logins.json 里的副本 |
| --- | --- | --- |
| enda@doinggroup.com | `rt.1.AADjfXU` (work1) | `rt.1.AADjfXU` |
| onlinefirst001@gmail.com | `rt.1.AABIemG` (work2) | `rt.1.AABIemG` |
| doingluning@gmail.com | `rt.1.AAD1mrt` (work3) | `rt.1.AAD1mrt` |
| starchsyrupmachine@gmail.com | `rt.1.AADk5RQ` (work4) | `rt.1.AADk5RQ` |
| yongye.zhengyy@gmail.com | `~/.codex/auth.json` (`rt.1.AABr74L`) | `rt.1.AABr74L` |

这正是事故的根因，也是 v1 想拆的东西。

### 1.2 事故是"magpie 就地刷新 profile"造成的

`~/multicodex/profiles/work1/codex-home/logs_2.sqlite`：

```
ERROR codex_login::auth::manager: Failed to refresh token: 401 Unauthorized
  "message": "Your session has ended. Please log in again."
  "code": "refresh_token_invalidated"
DEBUG codex_core::session: permanent_refresh_failure: Some(Revoked)
```

magpie 那一侧的路径是 `internal/provider/logins_on.go:262`：`codexHeldIn`（`internal/provider/logins.go:806`）
发现这个账号在某个 Codex home 里登录着，就去那个 home **就地刷新**（`codexTokenRenewing`，
`internal/provider/account.go:1054`），令牌轮换 → profile 里的 Codex 手里那枚旧令牌作废
→ 它判 Revoked → **自己删掉 auth.json**。

### 1.3 codex-cli 0.154.0 不会在刷新前重读文件

本机 codex：`codex-cli 0.154.0`
（`~/.nvm/versions/node/v22.23.2/lib/node_modules/@openai/codex/node_modules/@openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex`）。
二进制里的相关字串：

- `Reloading auth for account ` / `Reloaded auth, changed: `
- `Skipping token refresh because auth changed after guarded reload.`
- `permanent_refresh_failure`、`external auth lock is poisoned`
- **没有** `auth.lock` 字样 → v1 引用的 codex-rs `home/auth.lock` 文件名不能照抄，
  锁的真实路径与语义要实测（§4 GATE-2）。

合起来是一句话：**刷新走的是进程内存里那枚 refresh token**；`Reloading auth` 是
401 之后的补救，不是刷新前的防线。⇒ **magpie 刷新 profile 的令牌，profile 里的
Codex 一定会踩到作废的令牌**，第二持有者还在。

⇒ v1 的"profile 目录独占 + magpie 就地刷新"这条路本身是事故成因，走不通。

### 1.4 五个 Codex 客户端都只跟本机网关说话

`~/.codex/config.toml` 与 4 个 profile 的 `config.toml` 都有：

```toml
cli_auth_credentials_store = "file"
openai_base_url = "http://127.0.0.1:3425/backend-api/codex"
```

即：客户端自己的 ChatGPT 登录**不是**它发请求的凭据；额度由网关侧的池决定
（`internal/gateway/codex_backend.go:712` `codexAccounts` → `codex/<model>` 走池）。

### 1.5 已经做到的一半

- 刷新在跨进程锁下做（`internal/provider/account.go` 附近的 `codexMu` =
  `filelock.Mutex{loginsPath() + ".codex.lock"}`），锁内重读源、刷新、写回同一文件。
- 测试 `internal/provider/refresh_across_magpies_test.go`
  `TestCodexRefreshWaitsForAnotherMagpie`：别的 magpie 轮换过 → 用新的、0 次刷新。
- 但这只保证"magpie 之间"，不解决"magpie 与 Codex 客户端之间"。

## 2. 不变量（每条都要配一条会红的测试）

| 编号 | 不变量 | 守卫（阶段 3） |
| --- | --- | --- |
| INV-1 | 一枚 refresh token，全机只有一个持有文件 | G1 扫描器 + G3 测试 |
| INV-2 | 一枚 refresh token，全机只有一个刷新者（进程） | G1 扫描器 + G2 写路径守卫 |
| INV-3 | magpie 永不写任何 Codex home 的 `auth.json`（写 = 造出第二持有者） | G2 只读沙箱测试 |
| INV-4 | magpie 存的每枚 ChatGPT refresh token 必须是 magpie 自己登录得到的那枚，且与任何 Codex home 里的都不同 | G1 + G3 |
| INV-5 | 账号归属/顺序/开关不住在 `logins.json`（旧版 `v0.1.1561` 会抹） | G4 测试 |
| INV-6 | 任何刷新被拒（`refresh_token_reused` / `invalidated`）时，**先重读源**再判死 | G3 测试（pp1 落点） |
| INV-7 | magpie 发往 ChatGPT 的请求，**只用 magpie 自己登录的那枚会话**；客户端的 access token 一律不取、不用 | G2 测试 + G3 |
| INV-8 | 客户端刷新它自己的会话（AT 过期时）是它自己的事：magpie 不参与、不阻止、不把它当异常 | G5 文案 + 实跑记录 |

## 3. 设计：双会话，各持一枚

同一个 ChatGPT 账号可以有两个**互不相干**的会话（两枚不同的 refresh token）。
每个账号：

```
profile 的 auth.json ── RT_profile ── 持有者: Codex 客户端 ── 刷新者: Codex 客户端
magpie 的 logins.json ─ RT_magpie ── 持有者: magpie ──────────── 刷新者: magpie
```

两枚令牌各自单持有、单刷新 ⇒ **任何一方轮换都伤不到另一方** ⇒ 11-09 那类事故
在结构上不可能再发生。额度仍按账号算（同一个 ChatGPT 账号的 5 小时窗口共享），
所以池照旧能用这 5 个账号。

### 3.1 magpie 侧

- magpie 对每个池账号**自己登录一次**（`internal/provider/signin.go` 的 codex OAuth 流程，
  浏览器回调在本机 1455 端口），拿到的会话只存 magpie 自己的 `logins.json`。
- `logins.json` 里 codex 账号的 `auth` 字段 = magpie 自己的会话；加字段
  `session: "magpie"`，并**不再**用 `home` 指向别人的 profile（那是别人的家）。
- 账号归属（哪个账号、属于哪个 profile、排第几、开不开、上次看到）放独立文件
  `~/.config/magpie/codex-accounts.json`——**不存任何 token**（INV-5）。

### 3.2 profile 侧

- profile 的 `auth.json` **一个字节都不动**。它归它自己的 Codex 客户端。
- magpie 可以**只读**它的 `email` / `account_id` / `plan` 来展示"这个 profile 登录的是谁"，
  但**不把它的 token 读进内存**（这正是 pp1"不拿内存里的旧值"的极端形态）。
- magpie 不再在这些目录里刷新任何东西。
- **magpie 也不再拿它的 access token 去发请求**（INV-7）。今天 magpie 就是这么干的：
  `codexHeldIn` → `codexTokenRenewing(home)` → `codexSign` 用的就是 profile 那枚令牌。
  双会话落地后，这条路整个断掉：网关上所有发往 ChatGPT 的请求，只用 magpie 自己的会话。
- 客户端刷新它自己的会话（它的 AT 过期时）是它自己的事（INV-8）：magpie 不参与、
  不阻止、也不把它记成异常——它在**自己那枚**令牌上轮换，碰不到 magpie 的。

### 3.2.1 两套会话的职责边界（一句话版）

| | 客户端的会话 | magpie 的会话 |
| --- | --- | --- |
| 谁持有 | profile 的 `auth.json` | magpie 的 `logins.json` |
| 谁刷新 | 客户端自己 | magpie 自己（锁内重读 + 失败重读） |
| 用来做什么 | 客户端本地显示自己的 ChatGPT 账号；客户端自己连 ChatGPT 时 | 网关转发：额度轮换、5 小时窗口、用量与配额 |
| magpie 碰它吗 | **不读 token、不刷新、不写入、不拿它发请求** | 是它唯一的持有者 |

### 3.3 要删掉的旧机制

| 机制 | 位置 | 为什么删 |
| --- | --- | --- |
| `takeover` 携带别人的登录 | `internal/provider/codex_takeover.go:75` `TakeOverCodexAccounts` | 把 profile 的令牌搬进 logins.json = 造第二持有者。语义改为"纳入池"（只记账号，不搬令牌） |
| 在 Codex home 里就地刷新 | `internal/provider/logins_on.go:262`（`codexHeldIn` 分支）→ `codexTokenRenewing(home)` | 事故成因（§1.2） |
| 把别的账号写进 `~/.codex/auth.json` | `switchSavedLogin` → `saveCodexLogin` | 复制 = 第二持有者（v1 §2.6） |
| 轮换靠改写 Codex 的登录 | v1 阶段 3 | 改为**轮换只在网关侧**：`codex/<model>` 走池（§1.4 已有），不再改写任何 Codex home |

⇒ 本机账号 `yongye.zhengyy@gmail.com`：`~/.codex` 里的登录归 Codex 自己；magpie
另登一枚进池；池里它周额度，最后用。

### 3.4 防复发的守卫（这一节才是"以后不再出现"）

- **G1 双持有者扫描器 + 刷新前准入检查**（两道，缺一不可）：
  - **准入（事前）**：magpie **每次刷新自己会话之前**，先把这枚 refresh token 的
    哈希与所有 Codex home（`~/.codex`、各 multicodex profile 的 `auth.json`、
    macOS keychain 项）比对。**同一枚令牌在别处也出现 → 拒绝这次刷新**（fail closed），
    并明确告警"这枚令牌有两个持有者：<路径1> 与 <路径2>，magpie 不刷新它"。
    这一道才是真正防复发的：事后扫描只能告诉你"已经炸了"。
  - **扫描（事后 + 常态）**：启动时、每 5 分钟，把所有 home 与 magpie 自己的
    `logins.json` 的 refresh token 哈希两两比对。同一枚出现在两处 → 告警，
    指出两处路径，并在账号页上标出来。
  - **看得见**：加一条 `magpie accounts codex --holders`，列出每枚 ChatGPT 令牌
    谁持有（路径 + 角色），让用户随时能查，而不是等事故。
- **G2 写路径守卫**：任何写 Codex home 的代码路径（`saveCodexLogin`、`import_codex.go`、
  `migrate*.go`、`switchSavedLogin`）都不许再写 `auth.json`。用只读沙箱 HOME 做测试：
  跑一遍 magpie 的账号发现/刷新/切换，断言那些文件**没有被创建或修改**。
- **G3 不变量测试**：
  - magpie 的 store 里的 refresh token 与任何 Codex home 里的都不同；
  - `logins.json` 里没有"不属于 magpie 自己会话"的 ChatGPT refresh token；
  - 刷新被拒（`refresh_token_reused`）时**先重读源**：源里若是别人刚轮换的新令牌
    就用它继续，不判死；源里没有才标 lapsed（INV-6 / pp1）；
  - magpie 发往 ChatGPT 的请求用的令牌来自 magpie 自己的会话，不是任何 home 里的
    （INV-7）。
- **G4 归属状态守卫**：旧版 magpie 写过一次 `logins.json` 之后，magpie 的归属、
  顺序、开关仍完整（因为它们不住在 `logins.json`）。
- **G5 外部登出可见**：profile 的 `auth.json` 消失或换成别的账号时，Accounts 页与
  CLI 写明"work1 已被登出（文件在 <时间> 被删/替换），请在 multicodex 里重新登录"，
  不再显示没有解释的 `Unauthorized`。
- **G6 刷新边界可观测**：每次刷新在**读盘前、发送前、写回后**各记一行日志：
  源路径、令牌哈希前 8 位、access token 是否变了、服务端答复。
  下一次事故不用再翻 `logs_2.sqlite`——11-09 那次翻了一整晚。写回之后还要
  **重读一次确认**文件里是自己刚写的那枚（防"写到了别人的持有者上"）。

### 3.5 旧版 magpie

用户已裁决：**移除旧版，只留新版**。`/Applications/magpie.app`（`v0.1.1561`）读改写
`logins.json` 时会抹掉它不认识的字段（`strings` 里 `json:"taken` 命中 0 次），
这是 v1 里"接管标记丢失 → magpie 误去 profile 目录就地刷新"的直接原因。

即使移除旧版，归属状态仍要搬出 `logins.json`（INV-5 / G4）：下一次旧版出现时，
状态不该再丢。

## 4. 实验门（先做，不碰真实账号）

**GATE-1（必须过，否则回到 HITL）**：同一 ChatGPT 账号能否并存两个会话，且互不作废。

- 用一个**测试账号**（不进池），在 magpie 里跑一次 codex 登录（拿 RT_magpie），
  在另一个临时 Codex home 里登录一次（拿 RT_client）。
- 断言：两枚 RT 不同；刷新其中一枚，另一枚仍能刷新成功；刷新一枚**不**使另一枚
  已发出的 access token 失效。
- 每一步记录服务端答复原文（`refresh_token_reused` / `invalidated` / 200），
  写进 `docs/improve/` 的实验记录。
- 不过 → 回 §10 的 Q1，不许继续往下做。

**GATE-2**：把 11-09 复现出来，拿到 codex 端的**事实**而不是推断。

- 沙箱 HOME 里放一个 `auth.json`；一侧用本机 codex 0.154.0 触发刷新，
  另一侧 magpie 旋转同一枚令牌。
- 记录：codex 端收到什么答复？有没有 `Reloading auth`？有没有 `permanent_refresh_failure`？
  它删不删文件？codex 自己的锁文件叫什么、锁住什么？
- 产出：`docs/improve/codex-client-refresh-race.md`。

**GATE-3**：magpie 刷新**自己**的会话时，失败重读的兜底（INV-6）。

- 用假服务端造 `refresh_token_reused`，源文件里放一枚"别人刚轮换的新令牌"，
  断言 magpie 重读后用它继续、账号不判死。

## 5. 阶段步骤

### 阶段 0：止血（今天，不改代码）

- [ ] 0.1 备份 `~/.config/magpie/logins.json`（`logins.json.bak.2026-11-09`）与 5 个
  `auth.json`（连同 sha256、mtime），基线记进 `docs/improve/`。
- [ ] 0.2 移除旧版 `/Applications/magpie.app`（用户已裁决）。先确认新版本能启动、
  网关能起来，再挪走；挪走而不是删除，便于回滚。
- [ ] 0.3 **不动** `logins.json` 里的 4 份副本：现在 magpie 靠它们服务池，
  删了池就空了。清理属于阶段 2。
- [ ] 0.4 确认 `make serve-multicodex` 的现状记录（哪几个账号在池、各自的窗口）。
- [ ] 0.5 **先停掉"在 Codex home 里就地刷新"这条路**（`internal/provider/logins_on.go:262`
      的 `codexHeldIn` 刷新分支 → `codexTokenRenewing(home)`），这是本机现在唯一
      会去动别人 auth.json 的地方，也是 11-09 的成因。这一步单独一个提交，
      阶段 0 之后立刻做：在双会话落地之前，magpie 一律不碰任何 Codex home。
      停了之后，profile 里的账号暂时读不到额度（池里少了它们），这是刻意的。
- [ ] 0.6 **把池尽快补回来**：0.5 之后立刻用 magpie 自己的登录流程，为 4 个团队账号
      各登一次（客户端 → 浏览器 → 回调 1455），登一个验一个（`account_id` 对得上、
      窗口能读），登完池就恢复。这是阶段 2.3 的提前，**顺序很重要**：
      先登录、后清理 `logins.json` 里的旧副本，中间池一直是好的。
      登录做砸的风险见 pre-mortem 的"五次浏览器登录被拦"——所以**一次只做一个**，
      先做 work2–work4（没被事故波及的），最后做 work1（11-09 丢过会话的那个）。
      本机账号（周额度）最后再登，不急。
- [ ] 0.7 事故当天就把事实记进 `LESSONS.md`（不必等方案定稿）：
      "同一枚刷新令牌不能有两个持有者、一个刷新者"——先记事实与规则，
      阶段 4 再补齐方案引用。

### 阶段 1：实验门

- [ ] 1.1 GATE-1（测试账号，双会话互不作废）
- [ ] 1.2 GATE-2（复现 11-09，拿到 codex 端事实）
- [ ] 1.3 GATE-3（magpie 侧失败重读兜底）
- [ ] 1.4 结论写进 `docs/improve/`，并在本文件 §4 记录通过与否
- [ ] 1.5 GATE-1 不过时的退路演练：把 §10 Q1 的 (d) 半自动轮换写成一页可行性说明
      （谁在哪一步按什么键、magpie 怎么知道切好了），以便当场决定

### 阶段 2：实现

- [ ] 2.1 新增 `~/.config/magpie/codex-accounts.json`：账号身份 + 归属 profile +
  顺序 + 开关，**不含 token**。
- [ ] 2.2 `logins.json`：codex 账号只放 magpie 自己的会话；加 `session: "magpie"`；
  去掉指向别人 profile 的 `home`；旧 `auth` 里的复制在首次写入时清掉。
- [ ] 2.3 magpie 自己登录每个账号（CLI/GUI 各一条路），账号进池。阶段 0.6 已先做完
      4 个团队账号；这里补本机账号，并加一条"批量登录"的路子：读 multicodex 的账号
      列表，逐个开浏览器登录，回调自动记账，失败一个不影响其余。
- [ ] 2.4 删掉 `codexHeldIn` 的**刷新**分支与 `TakeOverCodexAccounts` 的携带语义。
      `magpie accounts takeover` 改名 `magpie accounts pool add`（语义：从"搬走凭据"
      变成"纳入轮换"）；旧名保留一个别名并提示改名（CLI 兼容，见 §10 Q3）。
- [ ] 2.5 删掉 `switchSavedLogin` → `saveCodexLogin` 改写 `~/.codex/auth.json`；
  轮换只在网关侧（`codexAccounts` 已有）。
- [ ] 2.6 刷新被拒时先重读源再判死（INV-6），错误消息区分"外部登出"与"需重新登录"。
- [ ] 2.7 外部登出检测与展示（G5）。
- [ ] 2.7.1 **断掉"拿客户端的令牌发请求"**（INV-7）：`codexSign` / `savedLoginToken` /
      `alsoOn` 里的 `a.token` 全部只走 magpie 自己的会话；客户端 home 里的
      access token 不进内存。
- [ ] 2.7.2 刷新边界日志（G6）：读盘前 / 发送前 / 写回后各一行（源路径、令牌哈希
      前 8 位、access token 变没变、服务端答复）；写回后重读确认。
- [ ] 2.8 **池不许退化**：`codexAccounts`（`internal/gateway/codex_backend.go:712`）
      与 `AlsoOn` 判定池成员，靠的是 `savedLogin` 记录与 `Home` 字段。改了账号模型
      （`home` 不再指向 profile、auth 换成 magpie 自己的会话）就可能让池只剩一个账号，
      5 小时切换静默失效。改动前先让
      `internal/provider/codex_cycle_test.go` 的 `TestCodexAccountsTurnOverAsACycle`
      覆盖新模型（红→绿），并补一条"池里 5 个账号"的集成断言。
- [ ] 2.9 **合并门**：§2 的 8 条不变量，每条都要有一个**能指名的测试**在仓库里；
      少一条不许合并。改动的每个子项一个提交，`go test ./...` 全绿。

### 阶段 3：守卫（防复发）

- [ ] 3.1 G1 双持有者扫描器 + **刷新前准入检查** + 告警 + fail closed +
      `magpie accounts codex --holders`
- [ ] 3.2 G2 写路径守卫测试（只读沙箱）
- [ ] 3.3 G3 不变量测试（store ≠ home；刷新失败重读；请求只用 magpie 自己的会话）
- [ ] 3.4 G4 旧版写过一次 `logins.json` 后状态仍完整
- [ ] 3.5 G5 的测试与文案
- [ ] 3.6 G6 刷新边界日志 + 写回后重读确认

### 阶段 4：验证与文档

- [ ] 4.1 `make build`、`make cli`、`go vet -tags nogui ./...` 零错误
- [ ] 4.2 `go test -tags nogui ./...` 全绿（`-timeout 45m`）
- [ ] 4.3 GUI 套件全绿（两种引擎、窄宽、每种语言）
- [ ] 4.4 实跑：`make serve-multicodex`，跑满一个 5 小时窗口；
      `serve.log` 里没有 `refresh_token_invalidated` / `refresh_token_reused`；
      5 个 profile 的 `auth.json` 的 sha256 在窗口内**不变**（magpie 不再碰它们）
- [ ] 4.5 `magpie accounts codex --json`：5 个账号各有 magpie 自己的会话与窗口；
      本机账号最后用
- [ ] 4.6 文档：`docs/subsystems/providers-accounts.md` 更新归属与刷新规则；
      `docs/improve/codex-multi-account-rotation.md` 补这次事故；
      `docs/improve/codex-client-refresh-race.md` 记 GATE-2 的事实；
      `LESSONS.md` 记一条："同一枚刷新令牌只能有一个持有者、一个刷新者"，
      并写进 `docs/code-standards.md`（若第三次见到）

## 6. 验收标准

1. `logins.json` 里每个 ChatGPT 账号的 refresh token，与所有 Codex home 里的
   **都不同**（G1 扫描器输出为空；`magpie accounts codex --holders` 每枚令牌只列一处）。
2. magpie 跑一轮账号发现/刷新/切换后，5 个 Codex home 的 `auth.json`
   **没有被创建、修改或删除**（G2 测试断言 mtime 与内容不变）。
3. 刷新被拒且源里已有别人的新令牌时，magpie 用新令牌继续，账号**不**被标 lapsed（G3）。
4. profile 的 `auth.json` 消失时，Accounts 页与 CLI 说"work1 已被登出"，
   不是 `Unauthorized`（G5）。
5. 旧版 magpie 写过一次 `logins.json` 后，账号归属与顺序仍完整（G4）。
6. 一个 5 小时窗口实跑：`serve.log` 无 `refresh_token_*`，5 个 profile 文件不变。
7. magpie 发往 ChatGPT 的请求，令牌来自 magpie 自己的会话（INV-7）；网关日志里
   看不到任何 home 里的令牌哈希。
8. 客户端自己刷新会话（AT 过期）时，magpie 不报错、不介入（INV-8），实跑记录里有。
9. 写回后重读确认：文件里是自己刚写的那枚令牌（G6）。

## 7. 风险与回滚

**回滚的第一原则：令牌副本永远不靠"恢复备份"回滚。** 把 `logins.json.bak` 拷回去
＝把双持有原样装回去＝把事故装回去。回滚只回**行为**（代码），令牌要就重新登录。

| 风险 | 缓解 | 回滚 |
| --- | --- | --- |
| GATE-1 不过（OpenAI 让两会话互相作废） | 阶段 1 就停，回 Q1 | 未动真实账号，无损失 |
| 删掉 4 份副本后池空了 | 阶段 0.6 先登录、池保持是好的，阶段 2.2 才清副本 | 不恢复副本；用 magpie 自己的会话继续（池是好的） |
| 5 次浏览器登录失败/被 MFA 拦 | 逐个账号做，每做一个验一个 | 池里已有的账号不受影响；少的那一个不参与轮换 |
| 移走旧版后菜单栏 App 没了 | 用户已裁决；可放回 | 把 `/Applications/magpie.app` 放回 |
| 改动波及 CLI/账号页 | 每步单独提交，`go test ./...` 全绿 | 逐提交 `git revert`（代码行为） |
| 新版起不来 | 挪走旧版前先确认新版可用 | 把旧版放回 |
| **令牌本身无法回滚** | 阶段 2 之前不删任何一份持有 | 账号作废只能重新登录 |

## 8. TODO

- [ ] todo 阶段 0：备份基线、移除旧版、现状记录、**停掉 profile 目录刷新**、
      **magpie 自己登录 4 个团队账号把池补回来**、LESSONS 先记事实
- [ ] todo 阶段 1：GATE-1 / GATE-2 / GATE-3
- [ ] todo 阶段 2：归属文件、logins.json 收窄、删旧机制、断掉"拿客户端令牌发请求"、
      失败重读、外部登出、边界日志、池不退化
- [ ] todo 阶段 3：G1（准入+扫描+`--holders`）/ G2–G6
- [ ] todo 阶段 4：验证与文档

## 9. 与 v1 的差异（为什么推翻）

| v1 的说法 | 现在的事实 | 结论 |
| --- | --- | --- |
| magpie 接管账号，在 profile 目录就地刷新 | 这就是 11-09 事故的成因；codex 0.154.0 刷新用内存里的令牌，不会自愈 | 推翻 |
| `logins.json` 里不留任何 ChatGPT refresh token | magpie 必须自己有令牌才能服务 5 小时池 | 改为：留**自己登录得到**的那枚，与任何 home 里的都不同 |
| 靠 `home/auth.lock` + `compare_and_save` 与 codex-rs 共享锁 | 0.154.0 的二进制里没有 `auth.lock` 字样 | 锁的真实路径待 GATE-2 实测；但**双会话让共享锁不再必需** |
| Q1：轮换只在网关侧 vs 改写 Codex 登录 | 用户裁决：profile 保留自己的 ChatGPT 登录 + 双会话 | 轮换只在网关侧，且 magpie 不改写任何 home |
| Q3：旧版 magpie 怎么处理 | 用户裁决：移除旧版 | 但状态仍搬出 `logins.json`（INV-5） |

## 10. Open Questions

- **Q1（若 GATE-1 不过）**：同一账号两会话被 OpenAI 视为互相作废 →
  退路有四条，需要用户裁决：
  (a) magpie 不托管这些账号的额度，5 小时切换跨账号做不到；
  (b) 放弃"profile 保留自己的 ChatGPT 登录"，profile 改用 magpie 网关 key，
      令牌只在 magpie 一处（用户已明确要保留登录，故此条基本排除）；
  (c) 暂停池，等上游支持多会话；
  (d) **半自动轮换**：magpie 不持任何令牌，池由客户端自己的会话组成；magpie 判定
      "该换账号了"，在客户端里提示"请用账号 B 登录"，用户在客户端自己登录 B
      （自己的会话、自己的令牌，零双持有）。代价是每次切换要人点一下。
- **Q2**：magpie 自己的会话过期后无人值守续命——已由 magpie 自己的刷新负责
  （锁 + 失败重读），不再是问题。
- **Q3**：`magpie accounts takeover` 改名 `magpie accounts pool add`（语义：从"搬走
  凭据"变成"纳入轮换"）；旧名保留为别名并提示改名。牵动文档与 Makefile，
  在阶段 2.4 一并改。

## 11. Out of Scope

- 不改 codex-rs / Codex 本身，不给上游打补丁。
- 不动 ChatGPT 账号的套餐、成员数、组织设置。
- 不重做 5 小时窗口与切换线算法。
- 不清理 work5（无登录的 profile），不删任何 profile 目录。
- 不接管 `~/.codex` 里 Codex 自己的登录（本机账号按双会话处理）。

---

## Pre-Mortem Risks

<!-- AUTO-GENERATED: New risks will be appended below -->

六个月后，这个计划彻底失败了。回看，它是这样坏的：

### [Risk] 池退化成只剩一个账号，5 小时切换静默失效

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.4
**Risk Score**: 5 × 4 × (1 - 0.4) = **12.0 — HIGH**

**Failure Scenario**: 改造把 `savedLogin` 的模型换掉（`home` 不再指向 profile、`auth`
换成 magpie 自己的会话）之后，`codexAccounts` / `AlsoOn` 判定池成员的条件不再成立。
网关只服务一个账号，用户以为在轮换，其实五个账号里四个没被用上——额度切换这个
**核心目标**静默消失，而且没有任何报错。

**Mitigation**:
- 阶段 2.8：改动前先让 `TestCodexAccountsTurnOverAsACycle` 覆盖新模型（红→绿），
  并加一条"池里有 5 个账号"的集成断言。
- 阶段 4.5 的验收不只看"5 个账号各有会话"，要看**窗口是各自不同的**（同一个账号
  被读成 5 份，就说明池是一个）。
- 任何改了 `savedLogin` / `codexHomeLogins` / `AlsoOn` 的提交，body 里写明
  池成员数在改动前后各是多少。

### [Risk] 旧的"在 Codex home 里就地刷新"没删干净，双持有回归

**Severity**: 5 | **Likelihood**: 4 | **Detectability**: 0.4
**Risk Score**: 5 × 4 × (1 - 0.4) = **12.0 — HIGH**

**Failure Scenario**: 双会话做完了，但 `logins_on.go:262` 的 `codexHeldIn` 刷新分支
（或 `TakeOverCodexAccounts` 的携带、或 `switchSavedLogin` 写 `~/.codex/auth.json`）
还在某条路径上被走到。magpie 又去刷新了 profile 的令牌 → profile 的 Codex 拿到
`refresh_token_invalidated` → 删掉 auth.json。**和 11-09 一模一样**，而且这次
还多了一份 magpie 自己的会话，排查更难。

**Mitigation**:
- 阶段 2.4/2.5 明确列出三个删除点，逐个删、逐个写注释说明为什么删。
- G2 守卫测试：只读沙箱 HOME，跑一遍账号发现/刷新/切换，断言所有 Codex home 的
  `auth.json` **没被创建、修改、删除**。这条测试在旧代码上必须是红的。
- 阶段 0.5 先把那一条路停掉——即使后面的步骤没做完，也不会再有人被作废。
- §2 的 8 条不变量 + §5 阶段 2.9 的合并门：每条不变量一个能指名的测试。

### [Risk] 实施窗口期（阶段 0 → 阶段 2 完成）又作废一次

**Severity**: 4 | **Likelihood**: 4 | **Detectability**: 0.5
**Risk Score**: 4 × 4 × (1 - 0.5) = 8.0

**Failure Scenario**: 双会话还没落地，旧的刷新分支还在跑，用户这几天照常用
multicodex 和 Codex → 期间再炸一次，会话作废、auth.json 被删。

**Mitigation**:
- 阶段 0.5：第一个代码提交就停掉在 Codex home 里刷新那条路。
- 阶段 0.1 的基线（sha256、mtime）留作证据：任何 auth.json 变了都能查是谁动的。
- 阶段 0 结束时把当前状态告诉用户（"这几天 profile 的账号暂时不在池里"）。

### [Risk] GATE-1 不过：OpenAI 让同一账号的两个会话互相作废

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: 实验发现登录第二枚会话会把第一枚作废（或刷新一枚使另一枚失效），
双会话不成立。整个设计的地基塌了，而真实账号已经被动过。

**Mitigation**:
- GATE-1 只用**测试账号**，不进池、不进 `logins.json`。
- 计划里预设了退路（§10 Q1），不过就停，不硬推。
- 每一步的服务端答复原文都记进 `docs/improve/`，失败时能说清是哪一步坏的。

### [Risk] 五次浏览器登录被 MFA 或风控拦下

**Severity**: 4 | **Likelihood**: 4 | **Detectability**: 0.3
**Risk Score**: 4 × 4 × (1 - 0.3) = 11.2

**Failure Scenario**: magpie 对五个账号各登一次，用户在浏览器里逐个过 MFA；
其中一个触发风控或验证码失败，登录卡住；五个账号的登录是一串，做一半失败更难收拾。

**Mitigation**:
- 一次只做一个账号：登录 → 验（`account_id` 对得上、窗口能读）→ 再做下一个。
- 失败的那个账号**不动**它现有的持有者（profile 那枚照旧），池里少一个也能跑。
- 登录后断言 `account_id` 与 `codex-accounts.json` 里记的一致，不一致就丢弃这次登录
  （防止登成同一个账号的另一个 workspace，池里混进错账号）。
- 账号多时登录可以摊到几天做，不必一次做完。

### [Risk] GATE-2 的复现实验用了真实账号

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: 为了看 codex 端的真实答复，在真实账号上做"一边刷新一边作废"，
用户的账号被真作废。

**Mitigation**:
- GATE-2 只在临时目录 + 测试账号的假服务端上做；要真 codex 二进制参与时，用一个
  专门的测试账号，并在实验记录里写明代价。
- 真实账号只在阶段 4 的实跑窗口里参与（那时已经是双会话，不存在互相作废）。

### [Risk] 删掉改写登录后，Codex CLI 自己的额度用完就不肯发请求

**Severity**: 4 | **Likelihood**: 3 | **Detectability**: 0.4
**Risk Score**: 4 × 3 × (1 - 0.4) = 7.2

**Failure Scenario**: 用户原来靠 magpie 改写 `~/.codex/auth.json` 来切账号；改不写了，
CLI 自己的账号额度用完时，CLI 自己拒绝发请求——用户看到的是"工具坏了"。

**Mitigation**:
- 阶段 2.5 之后，阶段 4 必须实跑验证：客户端自己的账号额度用完时，网关仍能用池
  里的账号把请求发出去（`codexAccounts` 的池路径）。
- 验证不过就说明 CLI 侧仍需一个切换机制，那时再回 HITL 讨论（Q1 的 (b)），
  不在实现里临时加"再改写登录"的口子。

### [Risk] 守卫只写在文档里，没有落成代码

**Severity**: 4 | **Likelihood**: 4 | **Detectability**: 0.5
**Risk Score**: 4 × 4 × (1 - 0.5) = 8.0

**Failure Scenario**: G1–G5 五个守卫里有两个只做了"文档里说要扫"，没有代码。
半年后有人加了一条新的复制凭据的路，没人拦，事故以别的形式回来。

**Mitigation**:
- 阶段 2.9 的合并门：8 条不变量，每条一个能指名的测试；测试不存在 = 不合并。
- G1 的扫描器本身要有测试（造两个文件持同一枚令牌 → 断言它报警并拒绝刷新）。
- 阶段 4.4 的实跑记录里附上扫描器的输出（"空"才算过）。

### [Risk] 双会话下读到的窗口是"会话级"而不是"账号级"

**Severity**: 4 | **Likelihood**: 4 | **Detectability**: 0.5
**Risk Score**: 4 × 4 × (1 - 0.5) = 8.0

**Failure Scenario**: magpie 用自己的会话读 5 小时窗口；profile 的 Codex 用它自己的会话
花同一个账号的额度。若窗口是按会话算的，magpie 看到的用量与真实消耗错位，切换线
（15%）算错 → 切早或切晚 → 要么用光额度，要么账号白占着。

**Mitigation**:
- 阶段 4.4 实跑时对照：客户端花掉的量与 magpie 读到的窗口增量对得上。
- 对不上就把切换线改成保守值，并在界面上说明用的是哪一侧的数据。

### [Risk] 归属文件与 `logins.json` 两处状态打架

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.6
**Risk Score**: 3 × 3 × (1 - 0.6) = 3.6

**Failure Scenario**: `codex-accounts.json` 说某账号属于 work1，`logins.json` 里没有它；
或反过来。账号页显示的和实际能用的对不上。

**Mitigation**:
- 单一事实来源：`logins.json` 是"池里有哪些账号"的事实来源；`codex-accounts.json`
  只存归属与顺序，不存成员资格。
- 一致性测试：两处对不上的账号，在账号页显示为"未归属"，不静默。

### [Risk] 一次改太多，出问题难定位、难回滚

**Severity**: 3 | **Likelihood**: 4 | **Detectability**: 0.6
**Risk Score**: 3 × 4 × (1 - 0.6) = 4.8

**Failure Scenario**: 阶段 2 的九个子项一起做，测试红了但不知道是哪条路坏了；
回滚要 revert 一大块。

**Mitigation**:
- 每个子项一个提交；每个提交单独跑 `go test -tags nogui ./...`（`-timeout 45m`）。
- 提交 body 写"跑了什么"（LESSONS 的规矩）。

### [Risk] 扫描器读 keychain 令牌要用户授权，频繁弹窗

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.5
**Risk Score**: 3 × 3 × (1 - 0.5) = 4.5

**Failure Scenario**: G1 每 5 分钟扫一遍所有 Codex home，keychain 里的登录要弹窗授权，
用户被弹烦；或扫描阻塞在授权上。

**Mitigation**:
- 沿用 `codex_store.go` 已有的做法：授权后十分钟内不再问（`codexRefusedKeychain` 那种）。
- 扫描器读不到（用户没授权）时记"未知"，不当成"没有"——LESSONS 里那条
  "failed read means unknown"。
- 扫描在后台线程，失败不挡网关。

### [Risk] 扫描器把自己跟自己比，误报淹没真告警

**Severity**: 2 | **Likelihood**: 4 | **Detectability**: 0.8
**Risk Score**: 2 × 4 × (1 - 0.8) = 1.6

**Mitigation**:
- 同一路径对只算一次；magpie 自己的 store 与它读出的源文件按"同源"记一次。
- 真告警（不同路径、同一枚令牌）单独一类输出，不与"未知""读不到"混在一起。

### [Risk] OpenAI 之后改策略，同账号的会话互相作废

**Severity**: 4 | **Likelihood**: 2 | **Detectability**: 0.4
**Risk Score**: 4 × 2 × (1 - 0.4) = 4.8

**Failure Scenario**: 今天允许并存两个会话，明天改成"新登录作废旧的"，双会话的地基没了，
五个账号的 magpie 会话会被用户下一次在别处登录全部作废。

**Mitigation**:
- G1 扫描器持续盯着：magpie 的会话令牌一旦被判 reused/invalidated，说明策略变了，
  告警并指向 §10 Q1。
- 阶段 4.4 的实跑窗口至少跨一天，覆盖用户自己在别处登录的可能。

### [Risk] 移除旧版 App 后新版起不来

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.7
**Risk Score**: 3 × 3 × (1 - 0.7) = 2.7

**Mitigation**:
- 阶段 0.2 先确认新版本能启动、网关能起来，再挪走旧版；挪走而不是删除。
- 保留一条回滚路径：把 `/Applications/magpie.app` 放回去。

### [Risk] 文档没跟着改，下一个人照旧文档做出双持有

**Severity**: 3 | **Likelihood**: 3 | **Detectability**: 0.6
**Risk Score**: 3 × 3 × (1 - 0.6) = 3.6

**Mitigation**:
- 阶段 4.3/4.6：`docs/subsystems/providers-accounts.md` 更新归属与刷新规则；
  `LESSONS.md` 记一条（"同一枚刷新令牌只能有一个持有者、一个刷新者"）；
  第三次见到就进 `docs/code-standards.md`。
- 本文件 §9 的差异表留着，别让 v1 的说法从历史里爬回来。

