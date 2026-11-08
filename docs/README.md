# 计划书怎么读

本目录是 **XallorRemote** 的产品定义。从这份文件进；每件事只在一个文件里写全。

首次使用：[中文 README](../README.md) / [English README](../README.en.md)。两者展示当前已实现命令；CLI 建议稿不属于已实现参数。

代理铁律正文在 [`.cursor/rules/`](../.cursor/rules/)；索引 [../AGENTS.md](../AGENTS.md)。不要把同一铁律再抄进本目录。

**实现 Relay / Runtime 流式：从 [dataplane.md](dataplane.md) 进，不要从 architecture 推断状态机。**

---

## 1. 阅读顺序

| 顺序 | 文件 | 只回答 |
| --- | --- | --- |
| 1 | [PRD.md](PRD.md) | 做什么、不做什么、何时算做成 |
| 2 | [decisions.md](decisions.md) | 品牌、Relay、IPC、技术栈、workspace、安全契约 |
| 3 | [model.md](model.md) | 一台机器是什么、怎么装、怎么双向 |
| 4 | [heads.md](heads.md) | MCP / CLI / GUI 谁听谁（不是 GUI 中枢） |
| 5 | [credentials.md](credentials.md) | ID / 授权码 / 设备密钥 |
| 6 | [architecture.md](architecture.md) | 一次任务谁连谁 |
| 7 | [dataplane.md](dataplane.md) | **Relay 如何把 exec 流式跑通（核心）** |
| 8 | [ipc.md](ipc.md) | 头 ↔ Runtime 本机协议 |
| 9 | [protocol.md](protocol.md) | WSS 消息与错误码 |
| 10 | [runtime.md](runtime.md) | 被控侧怎么执行 |
| 11 | [mcp.md](mcp.md) | Agent 侧 MCP |
| 12 | [relay.md](relay.md) | 中转落盘与预算 |
| 13 | [clients.md](clients.md) | 人侧命令与屏 |
| 14 | [stack.md](stack.md) | 语言、库、发行、不要造的轮子 |
| 15 | [os.md](os.md) | Windows / Linux 差异 |
| 16 | [platform.md](platform.md) | 以后的用户 / 设备 / 订阅 |

---

## 2. 术语

| 说法 | 含义 | 不要再说 |
| --- | --- | --- |
| **XallorRemote** | 产品名 | Remote Device Runtime、rdr |
| **Node** | 一台装了本产品的电脑 | 控制端包 / 被控端包 |
| **Runtime** | 本机长驻 Go 进程 | Device Agent、rdr-agent |
| **头** | MCP / GUI / TUI / CLI | 四个独立产品 |
| **Relay** | 中转；默认官方 hosted，可自托管 | 向日葵服务器 |
| **设备 ID** | 路由键 | 当密码用 |
| **授权码 grant** | 控制端凭证 | 验证码 |
| **设备密钥 secret** | 仅 Runtime 上线用 | 配进 mcp.json |
| **Peer** | 对方 ID + 对方授权码 | 好友隧道 |
| **入站** | 允许别人对本机 invoke | 远控 |

CLI：`xallor-remote`。路径与前缀见 [decisions.md](decisions.md)。`platform.md` = 账号订阅；`os.md` = 操作系统。

---

## 3. SSOT

| 主题 | 只在这里写全 |
| --- | --- |
| 范围、成功标准 | [PRD.md](PRD.md) |
| 品牌、选型、安全契约 | [decisions.md](decisions.md) |
| 一个包、幂等、双向 | [model.md](model.md) |
| **头与 Runtime 关系** | [heads.md](heads.md) |
| 三套秘密 | [credentials.md](credentials.md) |
| 时序（产品路径） | [architecture.md](architecture.md) |
| **exec 数据平面 / Relay 怎么转发** | [dataplane.md](dataplane.md) |
| **本机 IPC** | [ipc.md](ipc.md) |
| 消息与错误码 | [protocol.md](protocol.md) |
| 执行、策略、workspace | [runtime.md](runtime.md) |
| mcp.json 与 tools | [mcp.md](mcp.md) |
| 落盘 | [relay.md](relay.md) |
| CLI/GUI/TUI | [clients.md](clients.md) |
| Cursor CLI 交接试点、操作与恢复 | [handoff.md](handoff.md) |
| **栈与复用** | [stack.md](stack.md) |
| **发行来源验证与剩余安全边界** | [release-security.md](release-security.md) |
| **安装信任根设计与 #74 边界** | [install-trust-design.md](install-trust-design.md) |
| #56/#76 治理候选验收与后续依赖 | [governance-acceptance.md](governance-acceptance.md) |
| #75 受保护发行候选证据 | [attested-candidates.md](attested-candidates.md) |
| #61 目标 Shell capability 验收 | [shell-capabilities-acceptance.md](shell-capabilities-acceptance.md) |
| #65 OIDC / SSO provider-neutral 验证边界 | [oidc.md](oidc.md) |
| #67 Entitlement / admission 边界 | [entitlements.md](entitlements.md) |
| CLI 文档入口 / 参数审查 | [cli.md](cli.md) / [cli-ux-review.md](cli-ux-review.md)（建议未实施） |
| Windows/Linux | [os.md](os.md) |
| Account / 订阅 | [platform.md](platform.md) |

---

## 4. 已拍板（摘要）

架构（v0.4）：头经 Runtime 出站；peers 只在 Runtime；首次不签发 grant；v0 人机靠 CLI；策略只在 Runtime；无短配对码。

产品（v0.5）：

1. 品牌 **XallorRemote**，CLI `xallor-remote`。
2. 默认官方 hosted Relay：`wss://api.xallor.com/remote`；自托管随时可换。
3. Windows named pipe / Unix socket，不对 localhost 随机开端口。
4. Runtime/Relay/CLI/TUI = Go；MCP = TypeScript；GUI = Tauri 2。
5. 默认 workspace 为用户目录下 `XallorRemote/workspace`，启动打印，禁止静默用盘符根。

安全（v0.6）：Grant 为 bearer；revoke 仅本机；审计不还原命令全文；write 原子+上限；Relay 可见飞行载荷但不落盘。见 [decisions.md](decisions.md) §6。

数据平面（v0.7）：Relay = 鉴权实时路由器；`exec_id` 串起 invoke→stdout/exit；一条 client 绑一个目标；终态唯一；洪水则 truncated。见 [dataplane.md](dataplane.md)。

实现骨架（v0.8）：Runtime 中枢；MCP/CLI/TUI/GUI 同级 IPC 客户端。复用官方 MCP SDK、`coder/websocket`、cobra、bubbletea、Tauri 2、SQLite。见 [heads.md](heads.md) / [stack.md](stack.md)。
