# 目标 Shell capability 验收 / Target shell capability acceptance

Issue #61 的核心实现已存在于 Runtime shell adapter 链路，本页把实现位置、验证证据和边界固定下来，避免把控制端平台误当成目标平台。

## 实现事实

- `internal/shell` 提供目标侧 adapter 和 `Describe` 能力；`info.shell` 来自目标 Runtime 的 GOOS、PATH 和持久配置。
- `internal/noderun` 在目标进程内调用 `shell.Describe` 与 `execution.Build`，控制端只发送结构化 script/program、cwd、timeout 和可选 shell 名称。
- PowerShell/pwsh 使用目标侧 launcher，保留声明块、动态块、stderr、显式 exit 和最后语句状态；bash/sh 使用目标侧 `--noprofile/--norc/-c`。
- program 模式绕过 shell，argv 按数组传递；缺少目标解释器时 fail closed，不回退到控制端 shell。
- IPC/MCP 继续复用既有 Request/`device_id` 形状，没有新增执行协议。

## 已有证据

- shell adapter unit/integration tests cover configured/selected/available capability, missing interpreter, persistence/restart recovery, Unicode, quoting, PowerShell declarations, stderr, non-zero and native argv.
- `tests/shell_adapter_e2e.py` and the PR workflow run the real Runtime/Relay path on Windows and Linux runners.
- Protocol and CLI descriptions are in `docs/protocol.md`, `docs/os.md`, `docs/clients.md`, and `docs/mcp.md`; they state that shell selection belongs to the target.

## 明确边界

现有 CI shell E2E 在同一 OS runner 上运行 controller 和 target，不能单凭它宣称跨 OS 完成。2026-11-12 的本机验收另使用 Windows 独立 profile 与授权 Linux/amd64 专用容器，完成 Windows→Linux（bash/sh）及 Linux→Windows（PowerShell 5/pwsh）的实际 IPC→Relay→目标执行。Relay 仅绑定容器 loopback，Windows 通过 SSH docker-exec 字节隧道连接，无宿主机公开端口或生产服务变更。

6 个场景覆盖两方向的 Unicode stdout/stderr、明确 completed/exit 7，以及原生程序的空参数、引号、Shell 特殊字符、Unicode、类似选项的字面参数和目标工作目录。另核对两端实际运行二进制摘要、目标 OS/shell capability 和解释器版本；清理错误为空。输入使用 CLI `--request-file -` 的 UTF-8 JSON stdin，此证据不是控制端原生 Shell 命令行转义矩阵，也不是交互 stdin/PTY。

构建基线为 `2d4f41a9dacfa62692b25e807127b905894e74c7` 加本地未提交变更，制品显式 `dirty=true`，不是发布版本。Windows SHA-256 为 `a3986c11e7aa01999334dc7e926bb818f233d2c0b8d4e3f86977f5b030ef6a5f`；Linux 为 `a37a4bc5ddc970d88764522a7eb4649351cc68791154f50861d08e3127273c40`。本地报告 `non-gui-release/bidirectional-shell.json` 记录实际版本、终态和摘要，脚本 `run_bidirectional_shell.py` 及其配套 agent/pipe/info 文件保留复现路径。

后续仍须把异构验收纳入可持续运行的测试环境；script-file/声明块/缺解释器/重启及原生命令行的进一步本机验证见下方扩展矩阵。`info.shell` 当前仍仅返回 configured/selected/available，不包含本次通过执行查询获取的解释器版本。#61 保持开放。

## 2026-11-12 扩展矩阵（部分完成）

后续本机异构实测已有 18 个场景通过：在上述 6 项基础上，补齐 bash/sh 及 PowerShell/pwsh 的 UTF-8 BOM 脚本文件（Windows 目标同时覆盖 using/param/begin/end），两方向各自的缺解释器拒绝、不回退、恢复配置、重启后保留身份与 shell 配置，以及真实 Bash/pwsh 控制端命令行的参数保真。文件名和目标 cwd 含空格、引号及中文。该结果不再把这些本机跨 OS 场景列为“从未测试”；持续异构 CI 与更广版本矩阵仍未完成。

Windows PowerShell 5 作为**目标解释器**的执行已通过；它作为**控制端**运行本轮 `.ps1` 测试驱动时，被本机执行策略以 SecurityError/UnauthorizedAccess 拒绝，尚未进入产品 CLI。本轮没有修改执行策略或使用 Bypass。原生 PS5 控制端的 request-file 与直接 argv 观察仍未验收，不能从 PS5 目标成功推导控制端也成功。

报告 `non-gui-release/bidirectional-shell-expanded.json` 明确为 `passed=false`、`acceptance_complete=false`，保留 18 项通过记录、环境阻塞及 `cleanup_errors=[]`。独立审查确认范围，并发现报告汇总原先可能忽略最后一项之后的异常；已用先失败后通过的门禁测试修复为任何记录的运行失败或清理失败都不能标通过。#61/#73 保持开放。

## English

Issue #61 is implemented by the target-side shell adapter chain. Runtime capability discovery and command construction use the target GOOS, PATH, and configuration; the controller sends structured requests and never reinterprets target scripts through its own shell. Existing tests cover PowerShell/pwsh, bash/sh, Unicode, stderr, non-zero exits, native argv, missing interpreters, and persistence.

CI still uses same-OS controller/target runners. A separate 2026-11-12 local acceptance run exercised both real Windows→Linux and Linux→Windows paths using isolated profiles and a dedicated container, with six passing Unicode/stderr/nonzero-exit and native-argv/cwd cases. Both live binary hashes and remote target capabilities were checked. These are explicitly dirty local builds; JSON stdin requests do not prove native controller-shell quoting or interactive stdin. Persistent heterogeneous CI, script-file/declaration/missing-interpreter/restart coverage and version capability remain follow-up requirements.
