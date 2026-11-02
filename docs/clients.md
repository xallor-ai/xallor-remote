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
xallor-remote peer rename --id … --name …
xallor-remote exec --device … [--shell powershell] -- "完整脚本"
xallor-remote exec --device … --script-file <本地UTF-8文件或-> [--shell powershell]
xallor-remote exec --device … --program <对方程序> -- <arg1> <arg2>
xallor-remote exec --device … --request-file <本地JSON文件或->
xallor-remote config get
xallor-remote config set --shell auto|powershell|pwsh|bash|sh
xallor-remote approve
xallor-remote mcp print-config
xallor-remote mcp merge-config
xallor-remote relay [--listen :8443 --data <dir>]
xallor-remote reset --yes
```

`inbound off` 保留身份；`revoke` 销毁登记。尚无授权码时 `inbound on` 等同 `grant issue`。`approve`：本机交互确认高危命令；无 TTY 时不要跑。

`config` 修改当前本机 Runtime；要改变对方执行解释器，在对方设备执行。`get` 返回 JSON 配置，`set --shell` 持久保存并用于后续任务，不改变已运行任务。各系统支持项和自动选择规则见 [os.md](os.md)。`auto` 恢复自动选择，空参数不是清除操作。

`exec` 文本入口只收一个完整脚本参数，不把多个片段自动拼接。单次 `--shell` 只选择本次任务的目标解释器，不修改配置。程序模式的 `--` 后逐项作为参数，空字符串和特殊字符仍须先正确通过本地 Shell；跨系统复杂输入优先使用 `--script-file` / `--request-file`，避免本地 Shell 提前展开。两种文件均在控制端读取，可带 UTF-8 BOM，`-` 从标准输入读取。JSON 形状及上限见 [protocol.md](protocol.md)，其中不含 device_id，由 `--device` 选择目标。

`--cwd` 是对方 workspace 内的目录，`--timeout-ms` 设置本次执行期限。请求文件模式将这两项及 shell 写在 JSON 内，不再叠加同名 CLI 选项；程序模式不接受 `--shell`。脚本文件按内容发送，不会在目标落地，也不绕过目标命令行长度限制。CLI 文本输出和退出状态保持现有语义；此入口不是交互终端。

---

## 4. TUI（v0.1）

`xallor-remote tui`，做进同一 Go 二进制。SSH 可用。无 TTY 则退回 CLI；approval 无 UI 则 deny。

---

## 5. GUI（v0.1）

**Tauri 2**，调本机 Runtime。「连接」完成查看本机 ID、主动显示/复制授权码、开关入站、添加对方、保存设备备注和逐设备诊断。左栏：连接 / 任务 / 审批 / 审计 / 设置。任务保留流式输出和明确终态，可跨页取消；审批显示真实待确认操作；审计显示本机记录。授权码默认隐藏，显示 30 秒后或窗口失焦时自动隐藏。workspace 在设置里改。不做键鼠。不做 Electron。

工程：`apps/gui`。本机需 Rust + MSVC；开发 `npm run tauri:dev`，发行物 `xallor-remote-gui.exe`（先有本机 Runtime）。

---

## 6. 环境默认

| 环境 | v0 | 之后 |
| --- | --- | --- |
| Windows + Cursor | 一个 npm：`xallor-remote-mcp`（内含 Runtime）+ CLI | Tauri GUI |
| Linux 无 GUI | 同上 npm，或纯 Go 二进制 + CLI | TUI |
| Linux 有桌面 | 同 Windows | GUI 可选 |
