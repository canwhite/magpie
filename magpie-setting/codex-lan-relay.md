# Plan: 把 Codex(ChatGPT 订阅) 的流量通过 magpie 网关转到局域网其他机器

> **User's request (restated)**: 用户想把 Codex 的流量"转出来"用，具体是**给局域网其他机器上的客户端用**；方案形态选**开箱即用的配置清单**（不改仓库代码），把命令、配置、接入步骤、限额与验证写清楚。
>
> **What this plan answers**: 让局域网内任意 OpenAI/Anthropic 兼容客户端指向本机 magpie 网关，用 `codex/*` 模型吃本机 ChatGPT 订阅的额度，带 key 鉴权、账号/模型/用量三重限制。
>
> Goal: 局域网其他机器的客户端能通过本机 magpie 网关调用 Codex 账号的模型，受网关 key 管控，且可随时关闭。

## Context

magpie 内置了 Codex(ChatGPT) 订阅账号的读取与转发能力：

- Codex 的登录存在 `~/.codex/auth.json` 或 macOS Keychain 的 `Codex Auth` 项里，magpie 直接读成 provider `codex`，上游基址 `https://chatgpt.com/backend-api/codex`（`internal/provider/account.go:978`、`:1036`）。
- 网关 `127.0.0.1:3425` 说 OpenAI Chat、OpenAI Responses、Anthropic Messages、Gemini 四种协议并互转（`internal/gateway/gateway.go:744-750`），模型 id 形如 `codex/gpt-5.5`（`docs/reference.md:1792`）。
- 局域网共享开关是 Settings → Network and sharing → **Share on local network**（`internal/settings/settings.go:127` 的 `LAN`，由 `POST /api/settings/lan` 写，`internal/gui/api.go:2018`）。开启后，远程请求必须带**已启用的 gateway key**（Bearer / `x-api-key` / `x-goog-api-key` / `?key=`），否则 401；网关不再共享则 403（`internal/gateway/lan.go:116-118`、`docs/reference.md:706-712`）。
- 每个 gateway key 可锁模型（`magpie gateway-key models <id> codex/*`）、锁账号（`magpie gateway-key accounts <id> codex/me@example.com`）、加限额（`magpie gateway-key limit`）（`docs/reference.md:600-700`、`internal/cli/gateway_keys_cli.go`）。

约束（决定了本方案怎么写）：

1. **不改仓库代码**。全部用现成 CLI + GUI 开关完成，配置落在 magpie 自己的 `settings.json` / `caller-keys.json`。
2. **Codex 上游是特殊通道**：强制 HTTP/1.1、流式走 WebSocket、请求体由 magpie 按 Codex CLI 的形状重造（`codexBody`）。所以转出来的是 Codex 账号的额度与 Responses 语义，不是裸 OpenAI API。
3. **额度共享是全局的**：Codex 账号的 5 小时/周额度被本机与局域网一起吃。限额必须在 gateway key 上做，否则一个客户端能把订阅打空。
4. **OpenAI 账号条款**：把 ChatGPT 订阅额度分给其他机器/多人使用通常不在 OpenAI 允许范围内。本方案定位为**自用/家庭/小范围内部**，不作为对外分发手段。见 Open Questions 的合规确认项。

## Goal

完成时可达成的状态（可逐条验证）：

- 本机 magpie 网关已开局域网共享，监听 `0.0.0.0:3425`（或固定到某个内网地址）。
- 至少一个 gateway key 存在、已启用、只允许 `codex/*` 模型、只允许指定 Codex 账号、带一条用量限额。
- 局域网另一台机器上，一个 OpenAI 兼容客户端用该 key + `http://<本机内网IP>:3425/v1` 成功完成一次 `codex/<model>` 的流式请求。
- 一条命令能关掉共享（回到只本机可用），且已建 key 可随时 `remove` 或 `rotate`。

## Plan

### 1. 确认本机 Codex 账号在位且可用

```sh
magpie accounts            # 列出 magpie 知道的订阅账号，看 codex 那行有没有额度
magpie quota codex         # 看 ChatGPT 账号各窗口还剩多少
magpie accounts refresh    # 手动续一次登录（可选，验证 token 有效）
```

若列表里没有 codex 账号，二选一：

- 交互登录：`magpie accounts add codex`（走 OAuth 浏览器流程）。
- 导入现成文件：`magpie accounts import codex ~/.codex/auth.json --yes`。
  **注意**：导入会把那个文件原本的 ChatGPT 登录从别的工具（Codex CLI、codexbar、CLIProxyAPI）挤掉，那些工具需要重新登录（`docs/reference.md:1827-1833`）。

若 `magpie accounts` 里 codex 账号显示 `Lapsed` 或 `Unauthorized`，先按提示重新登录，再往下走。

### 2. 记录本机内网地址与模型清单

```sh
magpie gateway-key list     # 已有 key 一览
magpie models codex         # 本机 Codex 账号能服务的模型（provider/model 形式）
```

记下第 2 步里 `codex/…` 的模型 id（后续客户端填这个），以及本机内网 IP（`ipconfig getifaddr en0` 或 `ip -4 addr show`）。

### 3. 开启局域网共享

GUI 路径（推荐，设置项与密钥都由 GUI 落盘）：

- 打开 magpie → Settings → **Network and sharing** → **Share on local network** → On。
- 记下页面给出的地址（`http://<内网IP>:3425`）。它是 `gateway.LANURLs()` 算出来的（`internal/gateway/lan.go:158`）。

无 GUI 的服务器/容器路径（二选一，别同时开）：

```sh
# 方式 A：网关直接听在内网地址上（不再是 0.0.0.0 全网卡）
MAGPIE_ADDR=192.168.1.20:3425 magpie serve

# 方式 B：Docker 场景，监听全网卡
MAGPIE_ADDR=0.0.0.0:3425 magpie serve
```

`MAGPIE_ADDR` 非回环时网关即视为"在网络上"，远程请求同样要求 gateway key（`internal/gateway/lan.go:111-118`）。用它钉在某个地址可以让 LAN URL 稳定（`pinnedHost`，`internal/gateway/lan.go:42`）。

> **注意**：`magpie daemon install --lan` 是给 **magpie web 的页面**开 LAN，与网关 API 的共享是两件事（`internal/cli/daemon_cli.go:76-95`）。网关 API 用上面的 Settings 开关或 `MAGPIE_ADDR`。

### 4. 建一个专给局域网客户端的 gateway key

```sh
magpie gateway-key add "LAN clients"
```

这条命令**只打印一次**明文 key，立刻存好。紧接着收紧这个 key：

```sh
magpie gateway-key models  <id> codex/*                                  # 只允许 Codex 账号的模型
magpie gateway-key accounts <id> codex/me@example.com                    # 只允许这一个账号（换成实际邮箱）
magpie gateway-key limit   <id> 5h --tokens 200k                         # 每 5 小时 20 万 token
magpie gateway-key limit   <id> week --cost 5                            # 每周 5 美元估算上限
magpie gateway-key quotas  <id> on                                       # 允许这个 key 看到额度
```

`<id>` 是 `magpie gateway-key list` 里那一行的 ID 列。

限额语义：每个窗口独立计数，token 按"未命中缓存的输入 + 输出 + 缓存写入"，命中缓存的读只在开启 *Count cache reads too* 时计入；所有限额都还有余量才放行，耗尽后请求在**问上游之前**被 429 拒掉，并带 `Retry-After`（`docs/reference.md:643-676`）。计数写在 usage log 里，magpie 重启不清零。

### 5. 收紧来源（可选但建议）

Settings → Network and sharing → **Allowed addresses**，或环境变量：

```sh
MAGPIE_ALLOW_IPS=192.168.1.0/24,192.168.1.50
```

写错的条目会让**谁都进不来**（不是放行全部），本机自己永远放行。地址取连接的 `RemoteAddr`，不看转发头，所以前面有反代时限制的是反代的地址（`docs/reference.md:776-786`）。

### 6. 客户端接入（局域网另一台机器）

**通用 OpenAI 兼容客户端**（Cline / Continue / OpenCode / 自写脚本）：

```
base_url = http://<本机内网IP>:3425/v1
api_key  = <第 4 步打印的 gateway key 明文>
model    = codex/<第 2 步记下的模型 id>
```

**Codex CLI 装在另一台机器上**（要 Codex 自己的目录格式，用 `/v1/codex/models`）：

```toml
# ~/.codex/config.toml
model_provider = "magpie"
model = "codex/gpt-6.1-sol"

[model_providers.magpie]
name = "magpie"
base_url = "http://192.168.1.20:3425/v1"
model_catalog_url = "http://192.168.1.20:3425/v1/codex/models"
env_key = "MAGPIE_API_KEY"
wire_api = "responses"
supports_websockets = false
```

Codex 0.161+ 默认读 `model_catalog_url`；0.160 需要 `[features] api_key_model_discovery = true`。目录上限 1 MiB，超出会截断并由 `X-Magpie-Left-Out` 头说明漏了几个（`docs/reference.md:732-759`）。

**Claude Code 在另一台机器上**：

```sh
export ANTHROPIC_BASE_URL=http://192.168.1.20:3425
export ANTHROPIC_API_KEY=<gateway key>
```

### 7. 验证

本机：

```sh
curl -s http://127.0.0.1:3425/v1/models | head -c 400
```

局域网另一台机器：

```sh
# 1) 无 key：必须 401
curl -s -o /dev/null -w '%{http_code}\n' http://192.168.1.20:3425/v1/models

# 2) 有 key：必须 200 且列出 codex/* 模型
curl -s -H "Authorization: Bearer <key>" http://192.168.1.20:3425/v1/models | head -c 400

# 3) 真正打一次模型（流式）
curl -N http://192.168.1.20:3425/v1/responses \
  -H "Authorization: Bearer <key>" \
  -H 'Content-Type: application/json' \
  -d '{"model":"codex/<model>","stream":true,"input":"say hi"}'

# 4) 这个 key 的额度状态
curl -s -H "Authorization: Bearer <key>" http://192.168.1.20:3425/v1/magpie/limit
```

看 magpie 侧：`magpie usage --last 1h` 应出现这次请求，`X-Magpie-Provider` 头应显示 codex。

### 8. 日常操作与关停

```sh
magpie gateway-key list                 # 看各 key 用了多少
magpie gateway-key rotate <id>           # 换明文（旧的立即失效）
magpie gateway-key limit <id> list       # 看窗口用了多少
magpie gateway-key remove <id>           # 吊销某台机器
```

关停整条链路：Settings → Share on local network → Off（或 `MAGPIE_ADDR` 改回 `127.0.0.1:3425`）。已建的 key 保留但从此远程打不进来（403）。

### 9. 可选加固

- **TLS**：`magpie serve --tls-cert <fullchain.pem> --tls-key <key.pem>`，或 `MAGPIE_TLS_CERT` / `MAGPIE_TLS_KEY`。走 TLS 后远程客户端必须用 https（`internal/gateway/tls.go`、`docs/reference.md:790-800`）。
- **HTTPS 自签**：网关自己发 TLS，客户端用 `https://<内网IP>:3425/v1`。
- **每台机器一个 key**：不共用一把，便于单点吊销和独立限额。

## Think — Debug Methodology

排障顺序（先看日志，不猜）：

1. **先看 magpie 的日志**：`magpie heap` / `magpie goroutines` 确认进程活着；macOS 上日志在 magpie 的 Console 侧或 `~/Library/Logs/magpie*`（以仓库实际路径为准）。
2. **分层定位**，每层单独 curl：
   - 端口通不通：`nc -vz 192.168.1.20 3425`（不通 = 防火墙/没开共享/地址不对）。
   - 网关在不在：本机 `curl http://127.0.0.1:3425/v1` 应回 info JSON。
   - 鉴权过不过：带 key 打 `/v1/models` → 401 是 key 没启用或带错位置；403 是没开共享 / 被 Allowed addresses 拦 / key 被限死。
   - 模型名对不对：403 且信息里点名模型与"允许的账号"，说明 `models`/`accounts` 限制写窄了。
3. **代码读，不猜**：
   - 路由与鉴权：`internal/gateway/gateway.go:698-790`、`internal/gateway/lan.go`。
   - 远程判定与 401/403：`internal/gateway/lan.go:116-118`、`:243-280`。
   - key 存储与校验：`internal/access/access.go:491`（`Authenticate`）、`:572`（`ConfigureLAN`）。
   - key 的模型/账号/限额限制：`internal/cli/gateway_keys_cli.go:367`、`:426`、`:133`。
   - Codex 账号读取：`internal/provider/account.go:998-1044`。
4. **加日志的位置**：在**框架边界**打，不在业务逻辑里打 —— 即 `Handler` 入口、`lanGuard` 判定处、`forwardOnce` 出站处。用 `[DEBUG-` 前缀，`grep -r "\[DEBUG-" internal/` 一次清空。
5. **不要用 curl 代替浏览器验证**：若客户端是网页应用，要看 DevTools Console 的 CORS 与 403（`corsGuard` 对来源有白名单，见 `internal/gateway/cors.go:141`）。

## Do — Verification Strategy

本次是配置类变更，**没有代码改动**，所以门禁形式不同：

- **构建**：N/A（不改代码）。若误改了任何 `.go`，门禁是 `go build ./...` + `go vet ./...` 必须零错误。
- **静态分析**：N/A，同上。
- **运行时验证**：第 7 步的四条 curl，逐条给出期望状态码（401 / 200 / 流式 200 / 限额 JSON）。
- **逻辑正确性**——必须逐条走通的执行路径：

  | 路径 | 期望 | 怎么测 |
  | --- | --- | --- |
  | 远程无 key | 401 | curl 不带 Authorization 打 `/v1/models` |
  | 远程带已启用 key | 200，列表含 `codex/*` | curl 带 Bearer |
  | 远程带已禁用/已吊销 key | 401 | `magpie gateway-key remove` 后重试 |
  | 远程模型不在 key 允许范围 | 403，点名模型与允许的账号 | `models <id>` 只留 `codex/*`，再请求别的模型 |
  | 远程账号不在 key 允许范围 | 403 | `accounts <id>` 只留一个邮箱，请求走另一个账号 |
  | key 限额耗尽 | 429 + `Retry-After`，且**没问上游** | 把限额调到极小，跑一次后再跑一次 |
  | 共享关闭后远程请求 | 403 | Settings 关掉共享后重试 |
  | Allowed addresses 之外的来源 | 403，点名地址 | 设 `MAGPIE_ALLOW_IPS=192.168.1.50` 后从别的 IP 试 |
  | 额度耗尽 | 上游 429/5xx → 自动切下一个账号或模型 | `magpie quota codex` 观察窗口 |
  | Codex 账号 token 过期 | 自动刷新一次后照常服务 | `magpie accounts refresh` + 打一次 |

- **本机不受影响**：以上改动后，`curl http://127.0.0.1:3425/v1/responses`（无 key 或 token `magpie`）必须照旧可用。

## Adjust — Rollback and Global Scan

- **回滚**（按影响从小到大）：
  1. 吊销单台机器：`magpie gateway-key remove <id>`。
  2. 关共享：Settings → Share on local network → Off；或把 `MAGPIE_ADDR` 改回 `127.0.0.1:3425` 并重启服务。
  3. 撤销 Codex 账号导入：magpie 侧 `magpie accounts forget codex <email>`；原来那个工具（Codex CLI 等）需重新登录被导入的 ChatGPT 账号。
  4. 还原配置：备份的 `settings.json` / `caller-keys.json`（`magpie backup` 可导）。
- **全局扫描**（做完一轮检查这几处，避免只改一处）：
  - 同一台机器上是否还有别的暴露面：`magpie web --lan` 的页面端口（3430）是**另一套 key**（`MAGPIE_WEB_KEY`），不要与网关 key 混用。
  - WSL / Docker 出口：Docker 端口发布出来的请求的 peer 是 bridge，不是宿主机 loopback，同样按"远程"要 key；WSL NAT 有专门的 `wsl-` key 机制（`internal/gateway/wsl.go`）。
  - `Allowed addresses` 生效范围是**所有**路由，包括 `/v1/magpie/*` 状态接口与 MCP 路由 —— 收紧时别只想着模型接口。
  - `magpie accounts switch-only codex on` 与本方案互斥：那条让 Codex 自己的请求直连 OpenAI、不过网关；本方案要的是网关转发，别同时开。
  - 反向代理 / Cloudflare Tunnel 场景：走 loopback 但带转发头的请求会被当"别人的"（`docs/reference.md:760-775`）。若日后加隧道，要按那节的规则配，而不是照搬本方案的地址。
- **兼容性**：不改任何协议与文件格式，magpie 升级不破坏已建 key（key 存 `caller-keys.json`，`caller-keys.json` 有 `.bak` 恢复但**不会**从 `.bak` 读回——吊销过的 key 不会被恢复，见 `docs/subsystems/providers-accounts.md:223`）。

## Open Questions

- **合规确认（用户侧）**：OpenAI 的 ChatGPT 订阅条款是否允许把额度给家里/同事的其他机器用？本方案只建议自用与家庭内部；对外分发前用户需自行确认。
- **客户端具体是谁**：Cline / Continue / OpenCode / 自写脚本对 base_url 的写法略有差别（有的要带 `/v1`，有的不要）。拿到实际客户端清单后可补一份逐个的接入表。
- **是否要每台机器一把 key**：默认建议是。若用户只想一把，简单化第 4 步即可。
- **是否需要服务端 TLS**：局域网内 HTTP 明文传的是请求内容（含可能夹带的密钥）。若客户端所在网络不可信，需要第 9 步的 TLS。**此项需用户决定**。
- **Codex 账号数**：多账号时，`accounts <id>` 是否要放行全部（自用 failover）还是锁单个 —— 影响限额的实际含义。

## Out of Scope

- 不改 magpie 仓库代码，不新增 CLI 命令或接口。
- 不做公网暴露（隧道、反代、DNS、证书签发）。若要做，从本方案的 Allowed addresses + TLS 两节接着写，不在本文覆盖。
- 不做多机高可用、负载均衡、key 的自动轮换。
- 不改 Codex 上游协议细节（HTTP/1.1 强制、WebSocket 流式、native compaction 走账号直连等既有行为）。
- 不覆盖 Claude Code / Codex 在**本机**的配置改写（那属于"把 Codex 的流量转走"的反向需求，`magpie codex <model>` 一条命令即可，方案另写）。

---

> Next step: run `/pre-mortem docs/planning/codex-lan-relay.md` to identify failure modes and harden this plan.
