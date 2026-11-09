# 技术栈与复用

发行签名、SBOM、provenance、下载后验证及尚未完成的安装器强制验签边界见 [release-security.md](release-security.md)。本地 manifest 校验不等于来源认证。

#74 的长期 bootstrap/安装/回滚/MCP 信任根方案见 [install-trust-design.md](install-trust-design.md)。`runtime verify`、install/rollback 与 MCP vendor 自动选择复用可信系统 GitHub CLI 验证原始 binary/manifest provenance；真实 signed-vendor 正向路径和离线信任根轮换/撤销仍待确认。

SSOT：语言、库、仓库形态、发行。语言级选型结论在 [decisions.md](decisions.md) §4。头关系见 [heads.md](heads.md)。IPC 见 [ipc.md](ipc.md)。

原则：**协议与产品行为是我们的；传输、MCP 语法、TUI 框架、打包不是。** 能依赖稳定社区实现就不要自写。

---

## 1. 语言与进程

| 部分 | 语言 | 产物 |
| --- | --- | --- |
| Runtime / Relay / CLI / TUI | Go（当前稳定版，`go.mod` 钉次新或最新） | 单一二进制 `xallor-remote` |
| MCP | TypeScript，Node **20+** | npm：`xallor-remote-mcp`（内嵌本平台 Runtime；另提供 `bin`：`xallor-remote`） |
| GUI v0.1 | Tauri 2 + 薄 Web 前端 | 另发安装包，逻辑仍走 IPC |

无头被控机可以 **只装 Go 二进制**，也可以 **只装同一个 npm 包**（用 `xallor-remote start`，不跑 Cursor）。控制端装一次 npm 即可 ensure。

---

## 2. 复用（写死）

| 问题 | 用这个 | 不要 |
| --- | --- | --- |
| 本机 MCP stdio / tools / JSON-RPC | **`@modelcontextprotocol/server` v2** + stdio transport（官方；v1 的 `@modelcontextprotocol/sdk` 已拆包） | 自己实现 MCP 帧、手写 JSON-RPC |
| WSS 客户端/服务端 | **`github.com/coder/websocket`**（Go 作者推荐路径的后继；原 `nhooyr.io/websocket`） | 自写握手；不必上 gorilla，除非 coder 卡住 |
| CLI 子命令 | **`spf13/cobra`** | 从零解析 argv |
| TUI v0.1 | **Charm `bubbletea` + `lipgloss` + `bubbles`** | 自绘 ncurses、另写一套 Node TUI |
| GUI | **Tauri 2** 调本机 IPC | Electron；GUI 里自连 Relay |
| 日志 | Go `log/slog`；TS 侧少日志、可 `debug` | 自研日志平台 |
| ID | `github.com/google/uuid` | 自写随机串当 UUID |
| Windows Job Object / Unix 进程组 | `golang.org/x/sys` | 只 `kill` 父进程留孤儿 |
| Windows named pipe | **`github.com/Microsoft/go-winio`** | localhost TCP 冒充管道 |
| TLS | `crypto/tls` + 系统根证书 | 自签当默认；跳过证书校验 |
| grant / secret 哈希 | **SHA-256**（高熵，不必 bcrypt） | 自创哈希；明文落 Relay |
| Relay 落盘 | v0：**SQLite**，纯 Go 驱动 **`modernc.org/sqlite`**（devices / grants / audit） | v0 上 Postgres；自写 wal；stdout 进库 |
| JSON | 标准库 `encoding/json` / `JSON.stringify` | 新 IDL（protobuf/connect）——v0 不值得 |
| 发行 Go | **GoReleaser** 出各 OS 文件 + checksum | 手写三套 zip 脚本当正途 |
| PR 合入校验 | **GitHub Actions** `.github/workflows/pr-check.yml`（Go 双 OS + MCP + GUI typecheck） | 只靠本机测就合 main |
| 前端（GUI） | 任选 React 或 Svelte，**无业务逻辑** | 在 Web 里实现策略/授权 |

PTY、终端仿真、远程桌面协议：**不引入** `creack/pty`、xterm 当产品能力（v0 无交互 stdin）。

---

## 3. 仓库怎么摆

建议单仓库：

```text
cmd/xallor-remote/     入口：runtime / cli / tui / relay
internal/ipc/
internal/runtime/      策略、Popen、identity
internal/relay/        连接表、inflight、sqlite
internal/protocol/     WSS JSON 类型
packages/mcp/          xallor-remote-mcp（TS）
apps/gui/              v0.1 Tauri
docs/
```

公开 API 只有：CLI、MCP tools、WSS 协议、IPC。`internal/` 不给别人 import。

---

## 4. 二进制怎么到用户手里

| 渠道 | v0 |
| --- | --- |
| GitHub Releases | `xallor-remote` 各平台文件 + SHA256SUMS（纯 Go / 无 Node 场景） |
| npm | **`xallor-remote-mcp` 一个包**：MCP 头 + 嵌入的本平台 Runtime（`vendor/`）；`bin` 含 `xallor-remote-mcp` 与 `xallor-remote` |
| 查找顺序 | `XALLOR_REMOTE_BIN` → 已校验的安装版本 → **npm 包内 `vendor/<platform>/`** → `PATH` → 数据目录 `bin/`（MCP 与 GUI 共用此顺序；GUI 见 `apps/gui/src-tauri/src/ensure.rs`） |
| 找不到 | 报错 + 安装说明，**禁止静默联网下载** |

打 npm 包时用仓库脚本把已构建二进制拷进 `vendor/` 再 `npm pack`。以后可再拆 `optionalDependencies` 平台包并校验 checksum。不要把 secret 打进包。

构建入口：`python scripts/build-release.py`，默认拒绝未提交源码；本地验证可显式 `--allow-dirty`，制品的 dirty 标志会保留。版本源为 `internal/buildinfo/version.txt`，构建会检查 npm 元数据一致；Git SHA、构建时间和 dirty 标志写入 Runtime。每个平台目录包含二进制和 `manifest.json`，zip 另附 SHA-256。manifest 证明内容完整性，不代替发行来源认证。打包脚本校验 manifest 后才复制进 vendor。GitHub 工作流生成可下载制品；版本标签另外生成待确认的 draft release，不部署 Relay 或自动发布 npm。

本机安装：`xallor-remote runtime install --bundle <解压目录>`。先在私有快照验证原始 binary/manifest provenance，再复制到数据目录下独立版本目录、检查候选身份并停止旧 Runtime；保存 pending 状态后验证新进程的 PID/hash，最后原子切换版本记录。rollback 在激活旧版本前复验来源；MCP vendor 选择拒绝未认证候选。真实 signed-vendor 正向路径和离线轮换/撤销仍待完成。设备身份、授权码、peers、workspace 和入站选择不属于升级文件。

`runtime rollback` 恢复前一版本；`runtime status` 同时返回安装记录与实际运行制品。`version --json` 查看所调用程序，`ensure` 优先启动已选版本。升级中断后执行 rollback，不自动重试候选版本。历史 0.1.x Runtime 没有可验证的运行制品信息：首次迁移须先停止旧服务，再从新包安装，数据目录保持原样；此首次迁移没有自动保存旧二进制的保证。已纳管版本支持验证后升级和回滚。

MCP 与 Runtime **主版本必须一致**（IPC `status` 里带 `runtime_version`）。大版本不匹配则拒绝干活，避免半套协议。

---

## 5. 配置与密钥文件

只存在数据目录，格式 JSON / 原始密钥文件。不要引入新的配置语言。字段见 [runtime.md](runtime.md)。`peers.json` 权限同密钥。

---

## 6. 明确不自造 / 推迟

| 项 | 态度 |
| --- | --- |
| MCP 协议本身 | 官方 SDK |
| E2E 加密、自定义握手 | 不做 v0 |
| 消息总线、gRPC、NATS | 不做；WSS + IPC 足够 |
| 容器里再套一层「安全壳」 | 不做；策略见 runtime |
| 开机服务 / SYSTEM | v0 不做；用户会话进程 |
| GUI 自动更新、代码签名体系 | v0.1 再定；macOS 公证本就在 v0.1 |
| OpenTelemetry 全套 | v0 用 slog 即可 |
| 把 host-execution-mcp 链进来 | 不揉 Runtime |

---

## 7. 建议开工顺序（实现，不是再写 PRD）

1. `internal/protocol` + Relay：hello、inflight、echo 级转发（可用两个 Runtime 本机测）
2. Runtime：identity、IPC `status`/`exec`、Popen 流
3. CLI：`start` `ensure` `grant` `peer` `exec`
4. MCP：官方 SDK 包一层 IPC；Cursor 打通 `remote.exec` 流式
5. 策略、write 原子、cancel、断线杀树、truncated
6. 官方 Relay 预算与审计字段
7. v0.1：bubbletea TUI、Tauri GUI

前 4 步没有 GUI 也能交付 v0。GUI 不得成为第 1 步。
