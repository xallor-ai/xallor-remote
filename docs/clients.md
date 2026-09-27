# 客户端头：CLI / TUI / GUI

SSOT：人怎么操作。命令前缀 `xallor-remote`。四个头与 Runtime 的关系见 [heads.md](heads.md)。GUI 选型见 [decisions.md](decisions.md)。

可视化 = 设备、授权、流式执行、审批；不是 VNC。v0 用 CLI（完整人机），v0.1 用 TUI / Tauri GUI（同一 IPC 的壳，不是 MCP 的上级）。

---

## 1. 原则

人能做的，CLI 在 v0 必须能做。会话 = 流式终端。审批打在被控机本地；无头则 denylist，不等待。

`grant issue/rotate`、`inbound`、`revoke` / `reset` **只在被控的那台本机**执行。控制端可以 `peer add` 和 invoke（exec/read/write/…），**不能**改对方授权。

---

## 2. 信息架构

| 屏 | v0 | 以后 |
| --- | --- | --- |
| 本机 | CLI Must | 用量、探针 |
| 授权 | CLI Must | 多 grant、绑账号 |
| Peer | CLI Must | 分组 |
| 会话 | CLI Must | 分屏、文件树 |
| 审批 | CLI/本机提示 Must | 推送 |
| 审计 | Should | 导出 |
| 账号 / 订阅 | 不做 | [platform.md](platform.md) |

---

## 3. CLI

```text
xallor-remote ensure
xallor-remote start | stop | status
xallor-remote grant issue | show | rotate
xallor-remote inbound on | off
xallor-remote revoke
xallor-remote peer add --id … --grant …
xallor-remote peer list
xallor-remote exec --device … -- <command>
xallor-remote approve
xallor-remote mcp print-config
xallor-remote mcp merge-config
xallor-remote relay [--listen :8443 --data <dir>]
xallor-remote reset --yes
```

`inbound off` 保留身份；`revoke` 销毁登记。尚无授权码时 `inbound on` 等同 `grant issue`。`approve`：本机交互确认高危命令；无 TTY 时不要跑。

---

## 4. TUI（v0.1）

`xallor-remote tui`，做进同一 Go 二进制。SSH 可用。无 TTY 则退回 CLI；approval 无 UI 则 deny。

---

## 5. GUI（v0.1）

**Tauri 2**，调本机 Runtime。5 分钟路径在「连接」一屏完成：看见本机 ID、签发/分享授权码、开关入站、添加对方。会话里对流式执行。左栏：连接 / 会话 / 审批 / 审计 / 设置。Relay 不对用户展示；workspace 在设置里改。不做键鼠。不做 Electron。

工程：`apps/gui`。本机需 Rust + MSVC；开发 `npm run tauri:dev`，发行物 `xallor-remote-gui.exe`（先有本机 Runtime）。

---

## 6. 环境默认

| 环境 | v0 | 之后 |
| --- | --- | --- |
| Windows + Cursor | 一个 npm：`xallor-remote-mcp`（内含 Runtime）+ CLI | Tauri GUI |
| Linux 无 GUI | 同上 npm，或纯 Go 二进制 + CLI | TUI |
| Linux 有桌面 | 同 Windows | GUI 可选 |
