# 局域网网关 key 的限额设计（LAN clients）

写于 2026-10-26，对象是本机 magpie 网关里给局域网用的那把 key。

- key id：`a30df9c9462b31f403c37552`（名 `LAN clients`）
- 只允许：`codex/gpt-5.6-luna` 一个模型、`codex/yongye.zhengyy@gmail.com` 这个账号
- 上游套餐：ChatGPT Business **Business Premium**（接口 plan id `self_serve_business_prolite`）

## 1. 为什么要改

2026-10-26 13:25，局域网客户端收到 429：

```
magpie gateway key "LAN clients" has used 211345 of its 200000 input, output
and cache-write tokens for this 5-hour cycle; it resets at 2026-10-26 16:03 CST
```

当时代理侧 7 天内的 26 次 429 **全部**是本地的 `gateway_key_limit`，上游一次
rate limit 都没报过。也就是说 200K 不是"套餐的一半"，只是当初随手定的一个数，
它先于真实额度把线卡死了。

## 2. magpie 限额的计量口径

`internal/access/limit.go`：未缓存 input + output（含 reasoning）+ **cache write**，
cache read 不计（`CacheReads` 未开）。多个 limit 之间是"都还有余额才放行"。

## 3. 上游到底怎么算量

官方只按**本地消息条数**计量，不按 token。官方文档原话：

> The number of messages you can send depends on the model used, size and
> complexity of your tasks… These estimates are not fixed message limits.

本地消息 / 5 小时（Plus 与 Standard Business 档）：

| 模型 | Plus | Standard Business |
| --- | --- | --- |
| GPT-6 Astra | 5–45 | 5–45 |
| GPT-6.1 Sol | 15–160 | 15–160 |
| GPT-6 Sol | 15–150 | 15–150 |
| **GPT-6 Luna** | **350–3,000** | **350–3,000** |
| GPT-5.6 Sol | 10–100 | 10–100 |
| **GPT-5.6 Luna** | **250–2,000** | **250–2,000** |
| GPT-5.5 | 15–80 | 15–80 |

关键几点：

1. **Pro 类没有 5 小时窗口。** Business Premium 就属于这一类：本机
   `~/.config/magpie/quotas.json` 里该账号只有 `7 days` 一个窗口，没有 5h。
2. **周额度官方不给数字**，只写 "Weekly limits may also apply"，界面上只显示
   整数百分比。
3. 信用价（credits / 1M tokens，Standard 速度）：

| 模型 | input | cached input | output |
| --- | --- | --- | --- |
| GPT-6 Luna | 2.5 | 0.25 | 12.5 |
| GPT-5.6 Sol | 100 | 10 | 500 |
| GPT-5.6 Terra | 50 | 5 | 300 |
| GPT-5.6 Luna | 5 | 0.5 | 30 |

即：**同样 token，5.6 Luna 的价格是 6 Luna 的两倍**；Sol 是新款 Luna 的 20–40 倍。

## 4. 本机实测：1% 对应多少 token

`~/.config/magpie/quota-history.json` 只记变化，是唯一靠谱的观测点。
2026-10-26 上游周窗口在 14:10 重置到 100%（`start` 从 00:54 变 06:10 UTC，
14:10 CST），之后经网关的流量与百分比的对应：

```
14:10 起点          100%          0 token
掉到 99% 时         累计 ≤ 1,054,736 token（区间内 245 个请求）
掉到 98% 时         累计 ≤ 1,614,688 token（区间内 327 个请求）
17:27 为止          累计 4,252,905 token，仍停在 98%
```

推算：**1% ≈ 1.0–1.6M token → 整周 ≈ 150–250M token 当量**（按 5.6 Luna 的流量构成量得）。

注意两个坑：

- 百分比是整数，一格就是一格的精度；上面每个数字都是"跳格区间内的上限"。
- 2026-10-26 早些时候曾有"2% 从 0.94M 到 6.2M 都不动"的误判，原因是拿
  **窗口重置前**和**重置后**的读数做了比较，不是百分比真的不动。

## 5. 当前设置与理由

```
Limit 1  every 5 hours   20,000,000 tokens   （无 $ 上限）
Limit 2  per week       100,000,000 tokens   （无 $ 上限）
```

| 设置 | 占上游当量 | 理由 |
| --- | --- | --- |
| 5h 20M | 累计上限 ≈ 满速 5h 的 2.7 倍 | 实测峰值 ≈ 1.5M/小时，满 5h ≈ 7.5M；20M 只当事故闸 |
| 周 100M | 40–67% | 中性估计下一半留给本机 Codex app（自己日常 3–5M/周） |

$ 上限（曾是 $60）已于 2026-10-26 删除：模型锁死成 5.6 Luna 后花费比例也锁死，
100M token × 实测 $0.466/M = $46.6 < $60，那道闸永远不响，留着只会让人以为
多了一层保险。

## 6. 风险

1. **当量是 Luna 的。** 官方同一 5h 窗口里 Luna 是 250–2,000 条，Sol 只有
   10–100 条，权重差 20 倍。2026-10-26 18:00 起这把 key 已收窄到只允许
   `codex/gpt-5.6-luna`，所以局域网无法再切到 Sol/Astra/6 Luna，这一条风险已被
   配置消除；反过来，想放开模型时这条会立刻回来。
2. **限额是上限不是预留。** magpie 不区分"LAN 的份额"和"自己的份额"，两边共用一个
   上游窗口。100M 只保证 LAN 不会超过它，不保证剩下的一定够自己用。
3. **Business Premium 没有超量缓冲。** 它在 Codex 的 reserve 实验名单里
   （`internal/provider/subscription_usage.go:1042`），周额度用完时 app 会直接锁住
   输入框（`codexHeld`，同文件 1068 行起）。唯一能越过这把锁的是 credits 余额。
   所以本地闸应该留在上游下面，至少 429 消息能说清是谁拦的。

## 7. 建议的下一步

1. **可以考虑把局域网和本机的模型切到 `gpt-6-luna`**（账号里已有这个模型，见
   `magpie provider models codex`）。同样 5h 窗口额度从 250–2,000 条涨到
   350–3,000 条（+40%），credits 价格减半（input 5 → 2.5，output 30 → 12.5）。
   当前选择：用户 2026-10-26 决定**只用 5.6 Luna**，理由是省额度；上面这个换法
   其实更省，但属于质量偏好决定，留着以备再评估。
2. 想给自己留硬保险，二选一：
   - 把 LAN 的周限额收回到 60M；
   - 或者给 LAN 单独发一把只允许 `codex/gpt-6-luna` 的 key，把 Sol 这个变量去掉。
3. 每周看一次校准信号：`magpie gateway-key limit a30df9c9462b31f403c37552` 与
   app 里的 "Weekly limit ... left"。周末还剩 70% 以上说明给宽了，掉到 50% 以下
   要收。

## 8. 证据来源

- 官方：[Codex pricing](https://developers.openai.com/codex/pricing)、
  [Codex models](https://developers.openai.com/codex/models)、
  [Business credit rate card](https://help.openai.com/en/articles/11481834-chatgpt-rate-card-business-enterpriseedu-credit-based-pricing)
- 本机：`~/.config/magpie/quotas.json`（上游读数，每分钟刷新）、
  `~/.config/magpie/quota-history.json`（百分比变化史）、
  `~/.config/magpie/usage.jsonl`（逐请求记账）、`~/.config/magpie/caller-keys.json`（key 限额）
- 代码：`internal/access/limit.go`（计量口径）、
  `internal/provider/subscription_usage.go`（plan 分类与 reserve 实验）、
  `internal/provider/quota_last.go`（读数落盘）
