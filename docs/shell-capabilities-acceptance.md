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

现有 CI shell E2E 在同一 OS runner 上运行 controller 和 target。它证明了各平台上的目标侧行为，但**不能宣称** Linux→Windows 或 Windows→Linux 的真实跨 OS 端测已经完成；跨 OS 测试还需要隔离的异构 runner/容器组合。PowerShell 5、pwsh、bash、sh 的版本差异和 stdin/file 原生矩阵也保留为后续验收项。

## English

Issue #61 is implemented by the target-side shell adapter chain. Runtime capability discovery and command construction use the target GOOS, PATH, and configuration; the controller sends structured requests and never reinterprets target scripts through its own shell. Existing tests cover PowerShell/pwsh, bash/sh, Unicode, stderr, non-zero exits, native argv, missing interpreters, and persistence.

The current E2E matrix runs controller and target on the same OS runner. It therefore does not prove real Linux→Windows or Windows→Linux execution. Heterogeneous runners and broader PowerShell 5/pwsh/bash/sh stdin/file/version matrices remain explicit follow-up validation.
