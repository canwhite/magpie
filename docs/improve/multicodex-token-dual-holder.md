# 多账号轮换事故：enda 的 auth.json 为什么会丢

写于 2026-11-09。对象是这台机器上的五个 ChatGPT/Codex 账号里，
`enda@doinggroup.com`（multicodex 的 work1）那一个。

## 1. 事实

### 1.1 账号与凭据在哪

| 账号 | multicodex profile | 登录文件 | magpie 的副本 |
| --- | --- | --- | --- |
| enda@doinggroup.com | work1 | `~/multicodex/profiles/work1/codex-home/auth.json` | `~/.config/magpie/logins.json` 里有同一枚令牌的副本 |
| onlinefirst001@gmail.com | work2 | work2/codex-home/auth.json | 同上 |
| doingluning@gmail.com | work3 | work3/codex-home/auth.json | 同上 |
| starchsyrupmachine@gmail.com | work4 | work4/codex-home/auth.json | 同上 |
| yongye.zhengyy@gmail.com | （无，本机） | `~/.codex/auth.json` | 同上 |

五个账号里，`~/.config/magpie/logins.json` 存着每个账号的完整凭据
（`auth` 字段是 auth.json 的原样拷贝，含 `refresh_token`），同时
profile 目录里也有同一枚令牌的文件。**同一枚刷新令牌有两个持有者。**

### 1.2 文件确实丢过，又回来了

- 08:59、10:52、11:40、11:43（本地时间）三个时点，
  `~/multicodex/profiles/work1/codex-home/auth.json` 都不存在。
- 现在它又存在：mtime 2026-11-09 16:43，内容是 enda 的新登录，
  `last_refresh=2026-11-09T08:43:37Z`。
- `work1/codex-home/log/codex-login.log` 对得上：08:38、08:43 起了浏览器
  登录，08:43:34 token 交换成功 → 这是 16:43 那次**重新登录**写出来的新文件。

### 1.3 Codex 自己登出，magpie 没有删过它

work1 的 codex 日志（`work1/codex-home/logs_2.sqlite`）：

```
ERROR codex_login::auth::manager: Failed to refresh token: 401 Unauthorized
  "message": "Your session has ended. Please log in again."
  "code": "refresh_token_invalidated"
DEBUG codex_core::session: permanent_refresh_failure: Some(Revoked)
```

`refresh_token_invalidated` 是**刷新令牌被重用过**的答复：
codex-rs 把 `refresh_token_invalidated` 归类为 `Revoked`
（`codex-rs/login/src/auth/manager.rs:1837`），会话作废，
Codex 随即登出并删掉自己的 auth.json。

magpie 这一侧没有任何删除 profile 文件的动作：
`deleteCodexLogin` 只作用于 `codexHome()`（本机 `~/.codex`），
`codex_takeover.go` 对 profile 目录只读不写。删文件的只有 Codex 自己。

### 1.4 时间线（还原）

| 时刻（UTC） | 事件 | 证据 |
| --- | --- | --- |
| 11-03 01:37 | work1 登录，写入 auth.json | codex-login.log |
| 11-05 | `magpie accounts takeover` 接管四个团队账号 | commit ee850833 |
| 11-09 08:38–08:43 | work1 又登录一次，令牌换成新的 | codex-login.log |
| 11-09 08:43:37 | magpie 的 discovery 把新令牌抄进 logins.json | logins.json 的 `last_refresh` |
| 11-09 ~08:5x | magpie 在 **work1 的目录里**刷新了这枚令牌（轮换） | 见下 |
| 11-09 08:59:05 | work1 的 Codex 用缓存的旧令牌刷新 → 401 `refresh_token_invalidated` → Revoked | logs_2.sqlite |
| 11-09 08:59 起 | Codex 登出，删掉自己的 auth.json | 文件不在了 |
| 11-09 09:00 起 | magpie 手里那枚也作废：`magpie accounts codex` 显示 enda `Unauthorized`、无窗口 | accounts --json |

“为什么 magpie 会在 work1 的目录里刷新”：`taken` 标记丢了
（logins.json 里五个 codex 账号 `taken` 全是 null），magpie 就走
“账号还签在别的 Codex 目录里”那条路（`codexHeldIn`），
刷新时去那个目录里原地刷新（`codexTokenRenewing(home)`）。

## 2. 根因

**一个刷新令牌，两个持有者。** 令牌每刷新一次就轮换一次，
旧令牌立刻作废；两个持有者先后刷新，后一个拿到
`refresh_token_invalidated`，整个会话作废。

- magpie 的 logins.json 有一份副本；
- profile 目录的 auth.json 有一份；
- Codex CLI 自己在内存里也缓存着。

magpie 一刷新，Codex 手里那份就死了；Codex 一登出，文件就没了。
今天的 `taken` 标记丢失，只是让 magpie 走了"原地刷新 profile 目录"那条路，
把冲突从"两个文件"变成"同一个文件、两个刷新者"，结果一样。

`taken` 为什么会丢：发布版 `v0.1.1561`（`/Applications/magpie.app`）
的二进制里没有 `json:"taken"` 这个字段，它读 logins.json 再写回去时
就把不认识的字段丢了（`strings` 里查 `json:"taken` 命中 0 次；
已下载待更新的 `~/.magpie-update/app` 里命中 1 次）。
这正是 pre-mortem 记下的最高风险。

## 3. 结论

1. **不是 magpie 删的文件**，是 Codex 判定会话作废后自己登出删的。
   magpie 的刷新**间接**造成了这件事。
2. **双持有是根因**，`taken` 丢失只是让它提前暴露。
3. magpie 不应该持有任何"别人的"刷新令牌副本。
   账号在 multicodex 的目录里，magpie 就该在需要时去那个目录读，
   令牌只留在那里。

下一步：见 [执行计划](../planning/codex-single-token-holder.md)。
