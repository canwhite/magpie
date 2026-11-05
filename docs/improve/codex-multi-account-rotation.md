# Codex 多账号轮换使用文档（multicodex + 本地账号）

写于 2026-11-05。对象是这台机器上的五个 ChatGPT/Codex 账号：multicodex 管的
四个团队账号，和本机自己签的一个本地账号。

## 1. 这套东西是什么

magpie 把五个账号当成**一个池子**来管：Codex 的登录（`~/.codex/auth.json`）在
账号之间自动轮换，谁的额度用完就换下一个，用完一圈再回到第一个。

| 账号 | 套餐 | 登录在哪 | 5 小时窗口 | 7 天窗口 |
| --- | --- | --- | --- | --- |
| `enda@doinggroup.com`（work1） | team | `~/multicodex/profiles/work1/codex-home/auth.json` | 有 | 有 |
| `onlinefirst001@gmail.com`（work2） | team | `~/multicodex/profiles/work2/codex-home/auth.json` | 有 | 有 |
| `doingluning@gmail.com`（work3） | team | `~/multicodex/profiles/work3/codex-home/auth.json` | 有 | 有 |
| `starchsyrupmachine@gmail.com`（work4） | team | `~/multicodex/profiles/work4/codex-home/auth.json` | 有 | 有 |
| `yongye.zhengyy@gmail.com`（本地） | self_serve_business_prolite | `~/.codex/auth.json` | **无** | 有 |

五个账号共用一个 `chatgpt_account_id`，但 `chatgpt_user_id` 各不相同，所以是五个
独立的 ChatGPT 用户，各有各的额度。

**注意本地账号没有 5 小时窗口。** 厂商对 `self_serve_business_prolite` 只给
`7 days` 一个窗口（`~/.config/magpie/quotas.json` 里看得到），所以它只能靠周
额度参与轮换。这一点跟最初的计划假设不同（计划里以为它也有 5 小时窗口）。

## 2. 轮换规则

1. **顺序**：`work1 → work2 → work3 → work4 → 本地`。本地账号排最后，只有四个
   团队账号都没有额度了才轮到它。
2. **切换时机**：一个账号的五小时窗口**还剩 15%** 时，登录就切走。留 15% 是为了
   让下一个账号有一整个窗口的量可用——如果等到用光才切，下一个账号的窗口已经
   白跑了一部分。
3. **回到第一个**：work1 的五小时窗口重置后，它又是顺序里的第一个有额度的账号，
   登录自动回到 work1，每一轮都从同一个地方开始。
4. **周额度仍然管人**：某个账号的周窗口到 100%，它就出局了（两个窗口都没了），
   哪怕它五小时还很空。

## 3. 现在是什么状态（2026-11-05 实测）

```
$ magpie accounts codex
● codex   enda@doinggroup.com · Team           · team   5h 99% ↻42m 16:56  7d 38% ↻4d21h Tue 13:59
○ codex   onlinefirst001@gmail.com · Team      · team   5h 0% ↻4h59m 21:14  7d 0% ↻6d23h Nov 12 16:14
○ codex   doingluning@gmail.com · Team         · team   5h 4% ↻2h3m 18:17   7d 2% ↻4d22h Tue 14:23
○ codex   starchsyrupmachine@gmail.com · Team  · team   5h 43% ↻3h2m 19:16  7d 88% ↻24m 16:39
○ codex   yongye.zhengyy@gmail.com             · self_serve_business_prolite  7d 30% ↻5d1h Tue 18:07
  ● signed in · ○ takes over when it runs out
```

`●` 是当前登录着的，`○` 是轮到时会被切过去的。

轮换真的自己动了，日志里记着：

```
2026/11/05 15:53:57 codex: enda@doinggroup.com · Team has 15% or less of its
allowance left; signed it in to onlinefirst001@gmail.com · Team
```

work1 的五小时额度剩 1%，于是登录切到了 work2。**每五分钟查一次**，所以切换有
最多五分钟的延迟。

## 4. 怎么用

### 4.1 接管 multicodex 的账号

```
$ magpie accounts takeover
! magpie takes each account over, to switch to it and to order it.
  Each multicodex folder is read, never written: magpie keeps its own copy of the
  sign-in and refreshes that, so the two never hold one refresh token.
  Each account taken over then needs a fresh sign-in in multicodex: multicodex login <name>.
✓ took onlinefirst001@gmail.com · Team over
✓ took doingluning@gmail.com · Team over
✓ took starchsyrupmachine@gmail.com · Team over
✓ took enda@doinggroup.com · Team over
```

只接管某几个：`magpie accounts takeover work1 work2`。

**这一步有个前提**：magpie 得是**认识 `taken` 字段的版本**。老版本（v0.1.1561
及以前）不知道这个字段，它写 `logins.json` 时会把标记抹掉，于是账号又被算作
"还登录在 multicodex 目录里"，`magpie accounts switch` 会拒绝切过去，轮换也就
不会碰这些账号。用当前源码构建的版本跑一次接管即可，之后就稳了。

### 4.2 排顺序

```
$ magpie accounts order codex enda@doinggroup.com onlinefirst001@gmail.com \
    doingluning@gmail.com starchsyrupmachine@gmail.com yongye.zhengyy@gmail.com
1. enda@doinggroup.com · Team
2. onlinefirst001@gmail.com · Team
3. doingluning@gmail.com · Team
4. starchsyrupmachine@gmail.com · Team
5. yongye.zhengyy@gmail.com
```

也可以在 Accounts 页面拖。排在前面的账号必须先勾上（on），不然排不了。

### 4.3 手动切

```
$ magpie accounts switch codex enda@doinggroup.com
✓ codex is now signed in as enda@doinggroup.com · Team · sessions already running
  keep their account until restarted
```

手动切过之后，magpie **不会**把你手动切走的那个账号当"要回去"的账号——它认为
这是你的选择。这条也是为什么手动切到本地账号之后，登录就一直待在本地账号上：
你切走 work1 的那笔记录被清掉了（`login-returns.json` 是空的），magpie 于是按
"顺序里第一个有额度的账号"来判断，而不是回到 work1。

### 4.4 改切换比例

比例是**你的**，写在 magpie 自己的 `.env` 里：

```
$ magpie accounts switch-left
a rolling window (five hours): 15% left
a long window (a week, a month): 2% left
  kept in /Users/doing/.config/magpie/.env

$ magpie accounts switch-left 30
✓ the sign-in now moves off an account with 30 % of its rolling window left
  kept in /Users/doing/.config/magpie/.env · it takes effect at the next look

$ magpie accounts switch-left 5 long
✓ the sign-in now moves off an account with 5 % of its long window left
```

- **rolling** 是"一天之内会跑完一遍"的窗口（Codex 和 Claude Code 的五小时），
  默认剩 15% 就切。
- **long** 是"跑不完一遍"的窗口（周、月），额度稀缺，默认剩 2% 才切。
- 文件是 `~/.config/magpie/.env`，变量名 `MAGPIE_SWITCH_LEFT` 和
  `MAGPIE_SWITCH_LEFT_LONG`。环境变量优先级更高（在终端里
  `MAGPIE_SWITCH_LEFT=30 magpie serve` 就能临时改）。
- **改完五分钟内生效**，不用重启。
- 比例离 100% 不到 5 个点时，账号回到原来的比例（不再往后退），免得在边上
  来回抖。

### 4.5 看额度

```
$ magpie accounts codex          # 每个账号的窗口和剩余时间
$ magpie quota codex --json       # 同样的，带 JSON
$ magpie accounts codex --json
```

## 5. 一个 refresh token 只有一个持有者

Codex 每刷新一次就换一次 refresh token。同一个 token 有两个持有者的话，谁先刷新
谁就把另一个作废（`refresh_token_reused`），两个工具会互相把对方登出。

magpie 的做法是**接管**：把 profile 里的登录读进自己的 `logins.json`，之后刷新
只刷新 `logins.json` 里那份，**profile 目录只读不写**。所以：

- magpie 的 `logins.json` 是那四个账号 refresh token 的唯一持有者；
- profile 目录里的文件保持接管前的样子，谁想读还能读；
- 代价是那些 token 已经被 magpie 的刷新用掉了，**multicodex 要再用这些账号得
  重新登录**：`multicodex login <name>`。

本机实测（refresh token 的指纹）：

```
~/.codex（当前登录）              8185dff5c8e3
multicodex work1 目录             8185dff5c8e3   ← 同一个账号的同一份登录
multicodex work2 目录             1ef3176d4e45
multicodex work3 目录             8e9cdfe8c2a8
multicodex work4 目录             866e5fed6603
magpie 副本 enda@…                8185dff5c8e3
magpie 副本 onlinefirst001@…      1ef3176d4e45
magpie 副本 doingluning@…         8e9cdfe8c2a8
magpie 副本 starchsyrupmachine@… 866e5fed6603
magpie 副本 yongye.zhengyy@…      274b43f3df6f
```

当前登录的账号，它的 token 同时在 `~/.codex` 和 magpie 副本里——这是**同一个账号
的两份副本**，不是两个持有者：magpie 切走时把它保存下来，切回来时再用，同一时刻
只有一处是活的。

## 6. 出问题了怎么办

### 6.1 `magpie accounts switch` 说"is signed in to Codex in …"

```
magpie: enda@doinggroup.com · Team is signed in to Codex in
/Users/doing/multicodex/profiles/work1/codex-home: run Codex there
(CODEX_HOME=/Users/doing/multicodex/profiles/work1/codex-home codex) to use it,
or turn it on for magpie's requests
```

账号的接管标记丢了（多半是让老版本的 magpie 写过 `logins.json`）。用当前版本跑
一次 `magpie accounts takeover` 就回来了。

### 6.2 登录一直待在某个账号上不动

最常见的原因是**手动切过**（见 4.3）：手动切会把"要回去"的记录清掉。
`magpie accounts order codex …` 重新排一次顺序，会把登录切到顺序里的第一个；
如果它此刻没有额度，轮换会在下一次检查（最多五分钟）里把它挪到有额度的那个。

另一个原因是那个账号的额度读数是**未知**而不是**已用完**——未知不等于用完，
magpie 不会切走一个它看不清的账号。

### 6.3 回滚

- 账号本身在 `logins.json` 里，`magpie accounts forget codex <email>` 删掉一个。
- profile 目录的登录靠 `multicodex login <name>` 恢复——**第一次接管前先把每个
  profile 的 `auth.json` 备份一份**，这样回滚不用指望厂商接受一个用过的 token。
- 切回原样：`magpie accounts switch codex <email>`。

## 7. 代码在哪

| 东西 | 在哪 |
| --- | --- |
| 找到 multicodex 的账号 | `internal/appdir/multicodex.go`（读 `~/multicodex/config.json`，读不到就扫 `profiles/*/codex-home`） |
| 接管账号 | `internal/provider/codex_takeover.go` |
| 切换比例的配置 | `internal/provider/switch_config.go` |
| 轮换本身 | `internal/provider/codex_switch.go`（`NextLogin` / `SwitchWhenSpent`） |
| 五小时窗口的读法 | `internal/provider/codex_fivehour.go` |
| 账号顺序 | `internal/provider/account_order.go` |
| 命令行 | `internal/cli/accounts_cli.go`（`takeover` / `order` / `switch-left`） |
| 计划与验证记录 | `docs/planning/codex-multi-account-rotation.md` |
| 子系统说明 | `docs/subsystems/providers-accounts.md` |

## 8. 证据来源

- 本机：`~/.config/magpie/logins.json`（账号、顺序、接管标记）、
  `~/.config/magpie/quotas.json`（厂商读数）、`~/.config/magpie/.env`（比例）、
  `~/Library/Logs/magpie/serve.log`（切换日志）
- 厂商读数：`https://chatgpt.com/backend-api/codex/wham/usage`（magpie 读的
  `/wham/usage`），`primary_window` 是五小时，`secondary_window` 是周
- 代码：本仓库 `internal/provider`、`internal/appdir`、`internal/cli`
