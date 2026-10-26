# Codex 与 MiniMax 合并到统一网关

本文说明如何在现有局域网中转的基础上，将 Codex 和 MiniMax 的流量纳入同一个网关，并复用现有的 gateway key 与 `base_url`。

## 目标

- 继续使用同一个 magpie 网关。
- 继续使用现有的 gateway key。
- 继续使用现有的 `base_url`，例如：

  ```text
  http://<网关IP>:3425/v1
  ```

- 根据需要，让 Codex 和 MiniMax 自动 fallback，或继续使用各自原来的模型名。

## 先确认实际模型名

```sh
magpie models codex
magpie models minimax-code
```

记录输出的完整模型 ID，例如：

```text
codex/gpt-5.5
minimax-code/MiniMax-M2.1
```

实际模型名以本机 `magpie models` 的输出为准，不要直接照抄示例。

## 方案一：创建 Routing Group，自动 fallback

创建一个同时包含 Codex 和 MiniMax 模型的组：

```sh
magpie group add codex-minimax \
  models=codex/gpt-5.5,minimax-code/MiniMax-M2.1 \
  routing=order
```

`routing=order` 表示按成员顺序尝试。也可以使用按用量选择的策略：

```sh
magpie group set codex-minimax routing=usage
```

确认组内容：

```sh
magpie group codex-minimax
```

让现有 gateway key 只允许访问这个组：

```sh
magpie gateway-key models <key-id> group/codex-minimax
```

客户端配置改为：

```text
base_url = http://<网关IP>:3425/v1
api_key  = 原来的 gateway key
model    = group/codex-minimax
```

这种模式下，客户端只请求一个组名。magpie 根据组内路由策略选择 Codex 或 MiniMax；当前成员不可用时，可以尝试其他成员。

网关仍会根据实际成员处理协议转换，因此客户端不需要改变为厂商专用协议。

## 方案二：保留原来的 model 名称

如果客户端的 `model` 字段必须保持原样，例如继续使用：

```text
codex/gpt-5.5
minimax-code/MiniMax-M2.1
```

就不要创建 Routing Group。直接让同一个 key 同时允许两个 provider 的模型：

```sh
magpie gateway-key models <key-id> \
  codex/* \
  minimax-code/*
```

客户端仍然使用原配置：

```text
base_url = http://<网关IP>:3425/v1
api_key  = 原来的 gateway key
model    = codex/gpt-5.5
```

需要切换 MiniMax 时，将 `model` 改为：

```text
minimax-code/MiniMax-M2.1
```

这种模式会共用网关、key 和 `base_url`，但由客户端明确选择 provider，不会通过组自动 fallback。

## key 与账号限制

如果现有 key 还限制了 Codex 账号，需要检查该限制是否会阻止 MiniMax：

```sh
magpie gateway-key accounts <key-id> all
```

如果希望限制到特定账号，则把 Codex 和 MiniMax 允许使用的账号都加入限制。否则，账号限制可能导致模型虽然在 `models` 白名单中，实际请求仍然被拒绝。

原有的限额可以继续保留，例如：

```sh
magpie gateway-key limit <key-id> list
```

限额是按 gateway key 汇总的。也就是说，Codex 和 MiniMax 会共同消耗同一把 key 的 token 或费用额度。

## 验证

查看网关暴露的模型：

```sh
curl -s \
  -H "Authorization: Bearer <gateway-key>" \
  http://<网关IP>:3425/v1/models
```

组模式下，应能看到组模型或组可用的成员；原模型模式下，应能看到允许的 `codex/*` 和 `minimax-code/*` 模型。

测试组请求：

```sh
curl -N http://<网关IP>:3425/v1/responses \
  -H "Authorization: Bearer <gateway-key>" \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "group/codex-minimax",
    "stream": true,
    "input": "say hi"
  }'
```

查看实际由谁响应：

```sh
magpie usage --last 1h
```

响应头中的 `X-Magpie-Provider` 和 `X-Magpie-Model` 也可以用于确认最终使用的是 Codex 还是 MiniMax。

## 选择建议

- 需要自动切换和统一入口：使用 `group/codex-minimax`。
- 必须保持原来的 model 类型和模型名：不要使用组，只扩大 gateway key 的模型白名单。
- 两者共用一个 key 时，限额也是共享的；如果需要分别统计或分别限流，应为 Codex 和 MiniMax 创建不同的 gateway key。

## Pre-mortem：上线前把 High Risk 降为 0

假设这次改动已经失败，最可能的原因不是“组命令执行成功或失败”，而是配置看起来成功、实际请求却走错路径。因此上线前必须完成下面的约束。`High Risk` 表示尚未满足前置证据；所有检查通过后，High Risk 数量必须为 0。

| 失败情形 | 初始风险 | 约束与证据 | 失败时动作 | 通过后风险 |
| --- | --- | --- | --- | --- |
| 实际 provider 或模型 ID 写错，组成员不可用 | High | 以 `magpie models codex` 和 `magpie models minimax-code` 的当前输出为准；组创建后用 `magpie group codex-minimax` 核对每个成员 | 删除或修正组，不改客户端 | Medium |
| 把现有 key 的模型白名单误覆盖，导致原客户端中断 | High | 先执行 `magpie gateway-key list`、`magpie gateway-key models <key-id>`、`magpie gateway-key accounts <key-id>` 和 `magpie gateway-key limit <key-id> list`，保存输出；优先新建临时 key 做 canary | 用原白名单恢复，或撤销临时 key；不要在未验证前切换所有客户端 | Low |
| Codex 与 MiniMax 的账号限制不兼容 | High | 在目标 key 上明确检查 `accounts`；分别用 Codex 模型和 MiniMax 模型调用一次，确认不是 403 | 将账号限制改为明确允许的账号，或按 provider 拆分 key | Medium |
| 组 fallback 触发条件与预期不同，失败请求没有切到另一成员 | High | 先用 `routing=order`；分别成功调用两个成员，并在受控 canary 中记录 `X-Magpie-Provider`、`X-Magpie-Model` 和 usage | 保留原模型模式，暂不把组名写入生产客户端 | Medium |
| 客户端协议或能力与组成员不兼容 | High | 用实际客户端使用的 endpoint、`model`、stream 设置和工具调用做一次真实 canary；不能只用 `/v1/models` 证明可用 | 回退到原 provider/model；不要把协议转换问题当作额度问题处理 | Medium |
| Codex 与 MiniMax 共用额度，造成一方耗尽另一方 | High | 上线前确认 key 的 token/cost 限额，并把两种请求分别打到 `magpie usage --last 1h`；需要隔离时使用两个 key | 立即撤销或限流共享 key，分别建立 key 和额度 | Low |
| 局域网 key 暴露后，两种订阅都可被消耗 | High | 检查 Share on local network、Allowed addresses、key 是否启用，并确认无 key 请求为 401、非允许地址为 403 | 关闭 LAN sharing 或 rotate/remove key | Low |
| 新配置失败后无法快速恢复 | High | 先记录原 key、组、客户端 model 和限额；只做可逆修改；准备下面的 rollback 命令 | 立刻回滚组限制或切回原 model | Low |

### 推荐的低风险发布顺序

不要直接把所有客户端切到新组。先使用临时 key 做单客户端 canary：

```sh
magpie gateway-key add "codex-minimax-canary"
magpie gateway-key models <canary-id> group/codex-minimax
magpie gateway-key quotas <canary-id> on
```

然后用临时 key 验证：

```sh
curl -s \
  -H "Authorization: Bearer <canary-key>" \
  http://<网关IP>:3425/v1/models

curl -N http://<网关IP>:3425/v1/responses \
  -H "Authorization: Bearer <canary-key>" \
  -H 'Content-Type: application/json' \
  -d '{"model":"group/codex-minimax","stream":true,"input":"say hi"}'
```

至少确认以下结果：

1. `/v1/models` 返回组允许的模型。
2. 组请求成功返回流。
3. usage 中能看到实际响应的 provider 和 model。
4. Codex 和 MiniMax 成员各至少成功一次。
5. 无 key 的请求仍然返回 401。
6. 超出 key 模型限制的请求返回 403，且不会请求上游。

### Rollback

组模式只影响使用组名和对应 key 的请求。出现异常时，先把客户端的 `model` 改回原来的 `codex/...` 或 `minimax-code/...`，再撤销临时配置：

```sh
magpie gateway-key remove <canary-id>
magpie group rm codex-minimax
```

如果已经修改了现有 key，则先恢复修改前保存的模型白名单、账号白名单和限额；如果不能确认原值，不要继续扩大白名单，应直接 rotate 该 key 并重新建立受限 key。

### High-risk exit condition

只有同时满足以下条件，才认为本计划的 High Risk 已降为 0：

- 当前模型 ID 已从本机命令输出核对，而不是使用示例值。
- 原 key 的模型、账号和限额配置已经记录。
- canary key 已验证 Codex 和 MiniMax 两个成员。
- 使用真实客户端协议验证过至少一次流式请求。
- 已验证 key 限制、401、403 和回滚路径。
- 能从 usage 和响应头确认最终 provider/model。
- 已决定共享额度，或已经按 provider 分离 key。

未满足任一项时，计划状态仍为 `High Risk > 0`，不应切换所有客户端。
