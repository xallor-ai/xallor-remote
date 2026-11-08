# 治理候选验收与后续依赖（#56 / #76）

这份表描述当前 `codex/security-stage-one` 候选的真实边界。#76 是一个治理候选 PR，不能替代各子 Issue 的独立验收、审查和后续 PR；“代码测试通过”也不能把未接入的来源认证、回滚或产品能力记为完成。

## 当前候选

| Issue | 候选交付 | 真实状态与边界 |
| --- | --- | --- |
| #57 | MCP SDK/Zod 锁定、npm ci/test/build/audit 门禁 | 已实现；未来漏洞仍由 CI 审计发现 |
| #58 | Relay 内置 TLS、loopback 明文开发、安全 HTTPS 反代模式 | 已实现并有握手/证书/公网监听拒绝测试；未部署生产 Relay |
| #59 | trusted proxy、XFF 链归属、Origin allowlist、IPv6/mapped CIDR | 已实现并有真实 HTTP/WebSocket/限流测试；管理员仍须正确配置代理网络 |
| #60 | SBOM、release index、GitHub provenance 配置、外部验证入口 | 部分完成；真实 OIDC 正向签发依赖 release workflow，不能由本地 mock 替代 |
| #73 | exec 参数解析 JSONL 错误通道、双语 README/CLI 文档、真实参数保真回归 | 单点缺陷已修复；命名、help 语言策略、查询 JSON 一致性、完整 PowerShell/Linux 原生矩阵仍开放 |
| #74 | 固定 trust policy、独立 `runtime verify`、安装/升级/回滚前 raw binary/manifest provenance 验证、MCP vendor/PATH 候选拒绝、rehash 拒绝 | install/upgrade/rollback/MCP rejection 已接入；真实 signed-vendor 正向链路、离线根轮换/撤销仍未确认 |

安装器的关键顺序已经是：读取输入到私有快照 → 用独立信任的系统 `gh` 验证 manifest 和 binary → 认证成功后复制、`Control.Check`、保存 pending、停止旧 Runtime；任何失败都不能写安装目录或停止旧 Runtime。`runtime verify` 本身不安装，也不是永久安装凭证。

`packaged_install.py` 只验证 npm 包布局、vendor manifest、CLI version/help 入口，不在未签名本地 tgz 上调用 `runtime install`。真实 installer gate 使用 `release_install_source_e2e.py`；release workflow 只有在 raw provenance 已产生后，才用对应 runner/平台 binary 和 signed bundle 做安装回归。Windows/Linux runner 不共享错误平台的可执行文件。

## 后续 Issue → PR 依赖

| 顺序 | Issue / PR 主题 | 必须依赖 | 允许的验收边界 |
| --- | --- | --- | --- |
| 1 | #57–#59 维护性修复 | 当前代码与文档 | 依赖门禁、入口安全、代理/Origin 回归；不引入新 Runtime 后端 |
| 2 | #60 完整 release provenance | #57；受保护 tag、GitHub OIDC 权限与签发 workflow | raw binary/manifest、zip/tgz、SBOM、错误 signer/篡改负例；不能把 attestation 配置当运行时安装认证 |
| 3 | #74 完整 installer trust | #60 的真实 raw-file provenance；独立 bootstrap 信任根政策 | 首次安装、upgrade、rollback、MCP vendor/PATH、错误签名/撤销/轮换/离线恢复；未完成前不宣称端到端来源认证 |
| 4 | #73 CLI 收敛 | 当前 JSONL 修复；不改 Request/IPC/MCP/WSS | 目标参数/授权副作用/命令语义、help/错误语言、PowerShell 5/pwsh/Linux sh/bash 实跑；不扩展后端能力 |
| 5 | #61–#64 Runtime 能力与策略 | #57–#59；#73 的 Agent 机器契约；#74 的可信发布路径 | shell capabilities、执行后端、策略矩阵、恢复证据；每个 Issue 独立设计/测试/文档，不能从 #76 umbrella 继承“已完成” |
| 6 | #66–#71 identity/entitlement/relay control-plane | #61–#64 的执行契约和安全边界；另立 PR | 设备生命周期、配额、审计、registry/failover、控制面拆分；本候选不实现租户权益、支付或高可用 |
| 7 | #72 GUI | #73 的稳定 CLI/Runtime 入口；#61–#64 的能力契约 | 只做连接配置、终端/文件入口和连接列表；不把 GUI 变成 Runtime/Relay 控制面 |

当前 #76 之后的开发门槛是：先完成 #60/#74 的真实来源链路和 #73 的双轨 CLI 验收，再讨论 #61–#72 的新能力。任何后续 PR 都应链接对应 Issue 的代码、回归测试、文档和真实平台证据；不能以 #76 的 umbrella PR 合并状态代替子 Issue 关闭条件。
