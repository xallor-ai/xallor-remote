# XallorRemote

**XallorRemote**：每台电脑一个 Node。凭设备 ID + 授权码，经中转把任务发到对方 Windows / Linux，实时回流。同一套可反向互控。无公网 IP、无 SSH。

计划书从 [docs/README.md](docs/README.md) 进入。给代理的铁律与 PowerShell 教训：[AGENTS.md](AGENTS.md)。Relay：[docs/dataplane.md](docs/dataplane.md)。头与栈：[docs/heads.md](docs/heads.md)、[docs/stack.md](docs/stack.md)。

默认官方中转：`wss://api.xallor.com/remote`。人侧安装优先 **一个 npm 包** `xallor-remote-mcp`（内含 Runtime）；见 [docs/mcp.md](docs/mcp.md) / [docs/stack.md](docs/stack.md)。

本机自托管试跑（两台或两个终端）：

```text
go run ./cmd/xallor-remote relay --listen 127.0.0.1:8443
$env:XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:8443"
go run ./cmd/xallor-remote start
```

另一台同样设 Relay URL 后 `grant issue` / `peer add` / `exec -- whoami`。

版本与本机升级（使用可信来源下载并解压的发行包）：

```text
xallor-remote version --json
xallor-remote runtime install --bundle <解压目录>
xallor-remote runtime status
xallor-remote runtime rollback
```

安装会重启本机 Runtime，并保留设备 ID、授权码、连接配置和入站选择；请先结束正在执行的任务。失败时自动恢复已纳管的旧版本。首次从 0.1.x 迁移时，先停止旧服务，再用新包安装。构建与发行步骤见 [docs/stack.md](docs/stack.md)。

连接异常时先检查：

```text
xallor-remote status --json
xallor-remote doctor
xallor-remote doctor --device <对方的9位ID> --json
```

诊断只读取状态，不启动服务、不改授权或入站选择。按输出建议处理后再试；“未确认”表示没有收到完整应答。
