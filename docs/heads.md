# 头：MCP / CLI / TUI / GUI 怎么挂在 Runtime 上

SSOT：本机进程关系。人侧命令清单见 [clients.md](clients.md)。MCP tools 见 [mcp.md](mcp.md)。IPC 报文见 [ipc.md](ipc.md)。栈与库见 [stack.md](stack.md)。

**拍板：中枢是 Runtime，不是 GUI，也不是 MCP。**

---

## 1. 不要做成这样

下面这种「GUI 当控制面、MCP 去接 GUI、CLI 是简化 GUI」**明确不做**：

```text
❌  Cursor → MCP → GUI → ？
❌  CLI = 精简版 GUI
❌  GUI 里嵌一个 Relay / 自己持有 WSS
```

理由：无头 Linux 没有 GUI；Cursor 关窗口不能把入站打挂；授权与身份必须活在长驻进程里。GUI 只是 v0.1 才出现的一个头。

---

## 2. 做成这样

```text
                    ┌── MCP（TS，短驻，stdio ← Cursor）
                    ├── CLI（同一 Go 二进制的「客户端模式」）
本机 Runtime  ◄──── ├── TUI（v0.1，`xallor-remote tui`）
（Go，长驻，一个）    └── GUI（v0.1，Tauri，同样只连 IPC）
        │
        ├── WSS device → Relay（自己被控）
        └── WSS client → Relay（去控别人）
```

四个头 **对 Runtime 同级**：都是本机 IPC 客户端。谁也不做另一个的父进程。

Control/data plane contract 由 target `info.plane_contract` 和 Relay `/health.plane` 返回：管理操作经本机 IPC 或已认证 device control message；数据面只走 Runtime↔Relay WSS invoke/事件。MCP/GUI 不持有 Relay WSS，Relay 不提供公开管理 HTTP API。

| 头 | 寿命 | 对用户 | 对模型 | 改本机授权 |
| --- | --- | --- | --- | --- |
| Runtime | 长驻 | 无 UI | 无 | 执行（被头请求） |
| MCP | Cursor 会话 | 无 | **唯一** tool 面 | **不暴露**（不提供 tool） |
| CLI | 一条命令 | **v0 完整人机** | 无 | 本机可以 |
| TUI | 一次会话 | v0.1 | 无 | 本机可以 |
| GUI | 一次窗口 | v0.1 皮肤 | 无 | 本机可以 |

CLI **不是** GUI 的简化版。反过来：CLI 先覆盖人能做的全部；GUI/TUI 是同一套 IPC 的可视化壳。GUI 缺的能力，以 CLI 为准补，不要在 GUI 里另造协议。

---

## 3. 同一条 Go 二进制的三种进程角色

`xallor-remote` 一个文件，按子命令分角色（不要拆三个发行包）：

| 角色 | 子命令 | 干什么 |
| --- | --- | --- |
| Runtime 守护 | `start` / `ensure`（拉起） | 听 IPC、连 Relay、执行 |
| IPC 客户端 | `status` `grant` `peer` `exec` `tui` … | 连上已有 Runtime |
| Relay | `relay` | 中转进程；与 Runtime **不是**同一个进程 |

`ensure`：已有 Runtime 则复用；没有则以后台拉起再连。MCP 启动时只做 ensure + IPC，**不**自己当 Runtime。

GUI：Tauri 进程只当 IPC 客户端（可用 sidecar 或本机已 ensure 的 Runtime）。**禁止** GUI 直接 `wss://relay`。人侧安装见 [stack.md](stack.md) / [mcp.md](mcp.md)：优先一个 npm 包。

---

## 4. 能力切分

| 能力 | 走哪 |
| --- | --- |
| `remote.*` tools、stdio MCP | 只 MCP |
| 签发 / rotate / inbound / revoke | CLI / TUI / GUI → IPC；MCP 不提供对应 tool |
| `exec` / read / write / 流式 | 所有头，经 IPC；WSS 只 Runtime↔Relay |
| 审批提示 | 被控机本机的 CLI/TUI/GUI 订阅 IPC；无订阅者 → deny |
| 自托管中转 | `xallor-remote relay`，独立进程 |

模型就算想开入站：没有 tool，调不到。本机用户用 CLI 开，与「MCP 进程理论上也能发同一 IPC」同权（同一用户）——防的是 **模型误触**，不是防本机用户。

---

## 5. 典型组合（v0）

| 场景 | 跑什么 |
| --- | --- |
| Windows + Cursor 控对面 | MCP + 本机 Runtime（ensure）+ 对面 Runtime |
| 无桌面 Linux 被控 | 只 Runtime（`start`）；人用 SSH 上 CLI |
| 人在本机敲命令 | CLI → Runtime，不必开 Cursor、不必开 GUI |
| 以后看会话屏 | GUI 或 TUI → **同一个** Runtime，Cursor 可同时开 |

关 Cursor ≠ 停 Runtime。关 GUI 同上。`stop` / `revoke` 才动守护进程。
