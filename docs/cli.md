# CLI 使用入口

安装、最短共享码执行路径与 Agent 入口：[中文 README](../README.md) / [English README](../README.en.md)。

当前命令、输入模式与输出语义以 [clients.md §3](clients.md#3-cli) 为准；协议字段以 [protocol.md](protocol.md) 为准。

参数错误 / Argument errors：`exec --jsonl --unknown-option` 返回一条 `type=error, code=invalid_execution` JSON 到 stdout、stderr 诊断和退出码 1，不启动 Runtime。`--jsonl` 可在错误 flag 前或后；`--jsonl=false`、其它选项的值或 `--` 后的字面参数不启用格式。帮助仍为普通文本。完整字段只在协议文档定义。

With JSONL selected, exec argument parsing failures (unknown flags, missing values, invalid value types) produce exactly one `invalid_execution` error event on stdout, retain stderr diagnostics, and exit 1 before starting Runtime. Format selection works before or after the bad flag; literal values and arguments after `--` cannot select it. `--jsonl=false` disables JSONL and `--help` remains plain help. This does not change Request/IPC/MCP contracts.

参数易用性审查、真实验证和未实施的长期建议见 [cli-ux-review.md](cli-ux-review.md)。建议稿中的新参数尚不可用。

`runtime verify --bundle <bundle-dir> [--attestation-bundle <proof.json>]` 为已实现的只读来源验证入口：使用固定 GitHub/Sigstore 签发政策与可信系统 gh，成功输出当次验证 JSON，失败非零退出。`runtime install` 复用同一验证器并在写安装目录、执行候选或停止旧 Runtime 前拒绝未认证快照；可选证明文件不是信任根。rollback/MCP 强制认证仍未接入，不能将 `verify` 输出当作它们的安装许可。见 [install-trust-design.md](install-trust-design.md)；#74 未完成。本次 #73 仅修复上述参数错误通道，其它收敛另行验收。
