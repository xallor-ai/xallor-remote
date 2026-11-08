# XallorRemote

[English](README.en.md)

**XallorRemote**：每台电脑一个 Node。凭设备 ID + 授权码，经中转把任务发到对方 Windows / Linux，实时回流。同一套可反向互控。无公网 IP、无 SSH。

治理门槛：#57、#58、#59、#60、#73、#74。

计划书从 [docs/README.md](docs/README.md) 进入。给代理的铁律与 PowerShell 教训：[AGENTS.md](AGENTS.md)。Relay：[docs/dataplane.md](docs/dataplane.md)。头与栈：[docs/heads.md](docs/heads.md)、[docs/stack.md](docs/stack.md)。

默认官方中转：`wss://api.xallor.com/remote`。人侧安装优先 **一个 npm 包** `xallor-remote-mcp`（内含 Runtime）；见 [docs/mcp.md](docs/mcp.md) / [docs/stack.md](docs/stack.md)。

安装前先完成下述发行来源验证，再安装已验证的 tgz；尚未发布到 npm registry 时不要使用 `npx -y xallor-remote-mcp`：

```text
npm install -g ./xallor-remote-mcp-<version>.tgz
```

安装后的最短共享码路径：在被控机运行下列命令。`grant issue` 会签发新码并同时打开入站，再次运行也会换码；只查看已有授权码用 `grant show`。授权码只交给需要控制该设备的人或 Agent。

```text
xallor-remote grant issue
xallor-remote status --json
```

在控制机填入对方实际的 9 位设备 ID 和授权码（替换示例中的尖括号占位符）：

```text
xallor-remote peer add --device <device-id> --grant <grant>
xallor-remote exec --device <device-id> --program whoami
```

这些业务命令会自行确保本机 Runtime 在后台运行，不必另行先跑 `start`。`start` 是前台常驻命令；`ensure` 是幂等的后台确保命令。双方须使用同一个 Relay；自托管时分别设置下述 Relay URL。

Agent 调用使用查询的 `--json`、执行的 `--jsonl`，检查进程退出码与最后的 exit/error 事件。复杂输入推荐本地 UTF-8 `execution.json`，以数组保留空参数和 Unicode，避免 PowerShell/Linux 多层转义；`--` 不能阻止本地 Shell 提前展开。该文件例如：

```json
{"program":{"executable":"git","args":["status","--short"]},"timeout_ms":30000}
```

```text
xallor-remote exec --device <device-id> --request-file ./execution.json --jsonl
```

程序和工作目录属于对方，request-file 属于本机；请求文件模式不叠加 `--shell`、`--cwd`、`--timeout-ms`。`exec --jsonl` 的未知 flag、缺值或类型错误也在 stdout 返回唯一的 `invalid_execution` error，退出 1，并保留 stderr 诊断，不启动 Runtime；JSONL 可在错误 flag 前后，`--` 后的字面参数不启用格式，`--jsonl=false` 关闭。`--help` 仍显示普通帮助。Agent 不能把缺少终态当作成功。PowerShell 5 的默认 `>` 生成 UTF-16，不应用它生成 UTF-8 请求文件。MCP 的工具、配置与流式行为见 [docs/mcp.md](docs/mcp.md)，CLI 当前用法见 [docs/cli.md](docs/cli.md)，机器字段见 [docs/protocol.md](docs/protocol.md)。

目标能力由对方 Runtime 的 `info` 决定；`remote.device_info` 会返回目标 `shell`、`execution_modes` 和 `execution_backend`。当前后端沿用目标当前用户权限，Windows/Linux 分别报告 Job Object/进程组回收，`sandbox` 为 `none`；控制端不能按自己的操作系统猜测目标能力，也不能把回收边界当成低权限沙箱。

`xallor-remote policy --json` 与目标 `info.operation_policy` 返回同一份操作矩阵，供 CLI、MCP 和 Agent 读取；GUI 后续接入本机 IPC 后复用同一字段。矩阵是静态边界说明；Relay 仍会在每次调用复验设备访问、成员/租户授权、grant、入站和版本状态。

账号凭据与租户边界见 [OIDC/SSO 边界](docs/oidc.md)：当前 CLI 使用 Relay 运维签发的本地 credential，不执行浏览器登录；OIDC provider token 不能直接当作 grant 或 tenant 授权。

设备 enrollment、claim、transfer、revoke、reset 的前置和失联处理可用 `xallor-remote device lifecycle` 查看；它是契约说明，不会从控制端远程修改设备归属。

会员权益 feature/value 说明可用 `xallor-remote entitlement contract` 查看；它不读取余额，且不把 Relay inflight/IP 限额或本机 job 容量当作会员额度。

隐私最小化 usage event 契约可用 `xallor-remote usage contract` 查看；本机 activity 历史不是防篡改审计，也不直接产生账单。

单 Relay sticky registry 契约可用 `xallor-remote relay registry` 查看；它不代表多地域 HA 或 failover。

多地域 failover 的 health/ownership/unknown/no-replay 契约可用 `xallor-remote relay failover` 查看；当前命令只报告规则，不执行自动切换。

Control/data plane 分离契约可用 `xallor-remote relay planes` 查看；管理凭据只在管理入口使用，MCP/GUI 经本机 Runtime IPC，实时 invoke 只走 WSS 数据面。

本机开发先在一个终端运行 Relay：

```text
go run ./cmd/xallor-remote relay --listen 127.0.0.1:8443
```

另开 PowerShell 终端运行本机 Runtime：

```powershell
$env:XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:8443"
go run ./cmd/xallor-remote start
```

Linux sh/bash 的对应 Runtime 命令（同样另开终端）：

```sh
XALLOR_REMOTE_RELAY_URL="ws://127.0.0.1:8443" go run ./cmd/xallor-remote start
```

同机第二个 Runtime 需设置不同的 `XALLOR_REMOTE_DATA_DIR`，并使用相同 Relay URL。跨电脑先配置下述安全自托管入口，让双方使用同一个可达 URL，再运行 `grant issue` / `peer add` / `exec --program whoami`。当前 CLI 用法见 [docs/cli.md](docs/cli.md)。

上述明文入口仅供本机开发。公网自托管使用 `relay --listen :8443 --tls-cert <PEM> --tls-key <PEM>` 和 `wss://`，或配置可信 HTTPS 反代；非 loopback 无保护监听会启动失败。代理信任、浏览器 Origin allowlist（含 IPv6）与健康检查配置见 [docs/relay.md](docs/relay.md#5-运维)。

版本与本机升级：先按 [发行来源验证](docs/release-security.md) 核对下载附件与预期标签/commit，成功后再解压和安装。`runtime install`、upgrade 和 rollback 会在使用已安装版本前复验 raw binary/manifest provenance；MCP vendor/PATH 选择也会对候选执行同一固定策略，真实 signed vendor 正向链路仍需 release CI 验证，#74 仍在完成中。

另有只读来源验证入口，使用独立信任的系统 GitHub CLI 和固定签发政策；它不会执行候选或安装，未签名/开发构建默认拒绝。安装、rollback 和 MCP vendor 选择复用同一验证器，成功输出不是安装凭证，后续目录变更或绕过入口仍不受保护。离线信任根轮换/撤销和真实 signed vendor 正向链路仍未完成，#74 未完成；方案与信任根/恢复边界见 [安装信任根设计](docs/install-trust-design.md)。

```text
xallor-remote runtime verify --bundle <bundle-dir>
```

```text
xallor-remote version --json
xallor-remote runtime install --bundle <bundle-dir>
xallor-remote runtime status
xallor-remote runtime rollback
```

安装会重启本机 Runtime，并保留设备 ID、授权码、连接配置和入站选择；请先结束正在执行的任务。失败时自动恢复已纳管的旧版本。首次从 0.1.x 迁移时，先停止旧服务，再用新包安装。构建与发行步骤见 [docs/stack.md](docs/stack.md)。

连接异常时先检查：

```text
xallor-remote status --json
xallor-remote doctor
xallor-remote doctor --device <device-id> --json
```

诊断只读取状态，不启动服务、不改授权或入站选择。按输出建议处理后再试；“未确认”表示没有收到完整应答。

上述 `<bundle-dir>` 为已验证后解压的目录，`<device-id>` 为对方的 9 位设备 ID。当前 CLI 的帮助、参数和跨系统原生 Shell 验收仍有待收敛，见 [#73 审查与未完成项](docs/cli-ux-review.md)；双语 README 对齐不表示 #73 已全部完成。
