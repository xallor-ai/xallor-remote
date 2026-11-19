# CLI 使用入口

安装、最短共享码执行路径与 Agent 入口：[中文 README](../README.md) / [English README](../README.en.md)。

当前命令、输入模式与输出语义以 [clients.md §3](clients.md#3-cli) 为准；协议字段以 [protocol.md](protocol.md) 为准。

`runtime reconcile` 通过本机 IPC 发送当前 profile 的 namespace、事务 revision 和
record digest，并打印 JSON。它只做 observation，不启动 Runtime、不安装或回滚，也不写
resolution；当前没有 owner-native evidence collector 时结果保持 unresolved。

参数错误 / Argument errors：`exec --jsonl --unknown-option` 返回一条 `type=error, code=invalid_execution` JSON 到 stdout、stderr 诊断和退出码 1，不启动 Runtime。`--jsonl` 可在错误 flag 前或后；`--jsonl=false`、其它选项的值或 `--` 后的字面参数不启用格式。帮助仍为普通文本。完整字段只在协议文档定义。

With JSONL selected, exec argument parsing failures (unknown flags, missing values, invalid value types) produce exactly one `invalid_execution` error event on stdout, retain stderr diagnostics, and exit 1 before starting Runtime. Format selection works before or after the bad flag; literal values and arguments after `--` cannot select it. `--jsonl=false` disables JSONL and `--help` remains plain help. This does not change Request/IPC/MCP contracts.

参数易用性审查、真实验证和未实施的长期建议见 [cli-ux-review.md](cli-ux-review.md)。建议稿中的新参数尚不可用。

`session request` 是当前非 GUI WIP 的实验性入口；它通过本机 Runtime IPC 请求接收方批准，
授权码只从 `XALLOR_REMOTE_ACCESS_CODE` 环境变量读取。一次性/限时/长期授权码和断线恢复
尚未成为稳定发布能力，不能把该命令当作生产授权流程。
`exec` 可用 `--session-id <id>` 复用 `session request` 返回的已批准逻辑会话；Runtime
会把该 ID 作为 invoke envelope 的授权绑定，不能在 CLI 侧扩大 scope 或延长有效期。
`session resume` 使用 `XALLOR_REMOTE_RESUME_CAPABILITY` 通过本机 Runtime 消费一次性
resume capability，并返回轮换后的 capability；CLI 不会把 capability 放进参数或日志。

`runtime verify --bundle <bundle-dir> [--attestation-bundle <proof.json>]` 为已实现的只读来源验证入口：使用固定 GitHub/Sigstore 签发政策与可信系统 gh，成功输出当次验证 JSON，失败非零退出。`runtime install`、rollback 和 MCP vendor 选择复用同一验证器；可选证明文件不是信任根。真实 signed-vendor 正向链路和离线信任根轮换/撤销仍待 release CI/后续工作确认。见 [install-trust-design.md](install-trust-design.md)；#74 未完成。本次 #73 仅修复上述参数错误通道，其它收敛另行验收。
