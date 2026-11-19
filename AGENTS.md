# AGENTS.md — AI 协作说明

本文件面向在本仓库中协助开发的 AI 代理与人类开发者。

**分层（避免把同一段抄三份）**：铁律正文在 [`.cursor/rules/`](.cursor/rules/)；本文件是索引 + 本仓库独有约定（PowerShell、目录）；产品规格在 [`docs/`](docs/)（索引 [`docs/README.md`](docs/README.md)）。改口径只改一份。

## 首要原则（代理必读）

1. **未经用户明确要求，不得执行 Git 提交（`git commit`）或代替用户决定「现在该提交」**。若用户只要求改代码、排查或设计方案，完成改动后**停止**。唯一例外：用户原话明确要求提交（或「提交一下」「commit」）时方可执行。

2. **中枢是 Runtime，不是 GUI / MCP。** 实现 Relay 从 [`docs/dataplane.md`](docs/dataplane.md) 进，不要从 `architecture.md` 推断状态机。全文：[`.cursor/rules/ssot-and-product.mdc`](.cursor/rules/ssot-and-product.mdc)。

3. **改动后由代理在本对话内构建/测试使变更生效**，禁止只写「请 go test / 请 npm run」却不执行。对照表：[`.cursor/rules/post-code-change-build.mdc`](.cursor/rules/post-code-change-build.mdc)。

4. **官方 Relay / 生产主机默认只读。** 禁止用本机开发配置覆盖生产；改配置或部署须用户明确授权并先备份。全文：[`.cursor/rules/protect-production-environment.mdc`](.cursor/rules/protect-production-environment.mdc)。

5. **本机默认壳是 Windows PowerShell（不是 bash）** — 部署、转义、多行命令按 bash 写会经常失败。详见下节。

6. **产品尚未正式对用户发布。** 改协议字段、错误码、IPC 信封时一次切到长期形状，不为未发布客户端保留双写。全文：[`.cursor/rules/no-unpublished-client-compat.mdc`](.cursor/rules/no-unpublished-client-compat.mdc)。

7. **先定模块再写代码。** 禁止把新功能塞进已有大文件后再拆。全文：[`.cursor/rules/modular-file-design.mdc`](.cursor/rules/modular-file-design.mdc)。

8. **先评估复用，替换实现时同步清理，验证后再交付。** 开始选型、重构/迁移及结束代码改动前，代理必须主动阅读并执行 [`.cursor/rules/reuse-and-cleanup.mdc`](.cursor/rules/reuse-and-cleanup.mdc)，不等待用户提醒。不自动加载 `.mdc` 的工具也须按此链接读取正文。

其它 alwaysApply 铁律：未证实不改 [`.cursor/rules/no-unverified-assumptions.mdc`](.cursor/rules/no-unverified-assumptions.mdc)；用户向文案 [`.cursor/rules/user-facing-ui-copy.mdc`](.cursor/rules/user-facing-ui-copy.mdc)。

---

## Windows PowerShell（本机默认壳）

本仓库在 **Windows + Windows PowerShell 5.x** 下开发（控制台代码页多为 **GBK/CP936**）。Agent 的 Shell 工具走的就是这个壳，**不是** bash / zsh。复杂命令若把 bash 习惯（heredoc、三层引号、`python -c` 内嵌远程脚本）直接塞进一行，会反复在「转义 / 编码」上翻车，看起来像环境坏了。

### 怎么选执行方式

| 任务 | 做法 |
|------|------|
| 单条、无嵌套引号 | 直接跑：`go test ./...`、`git status`、`npm test` |
| 多行、SSH、远程 bash、JSON、正则、Python 内嵌 | **写成 `scripts/*.py`（或已有脚本）再 `python scripts/foo.py`**，不要 `python -c` / bash heredoc |
| git 多行提交说明 | 用户规则里的 `$(cat <<'EOF')` 是 **bash** 示例；本机用 PowerShell here-string `@""@` 或 `-m "第一行"`，需要正文时写临时文件再 `-F`。PS 5.1 **没有** `&&`，多步用 `;` 或分次调用 |

### 禁止（已踩过，勿再犯）

- **`$name:value` 插值**：`"try$i:$r"` 会被解析成驱动器限定变量，报 `InvalidVariableReferenceWithDrive`。改用 `"try{0}:{1}" -f $i, $r`。
- **`python -c` + PowerShell here-string + 远程 `r'''…'''`**：PowerShell 先拆引号，Python 再报 `unterminated triple-quoted string`。改写 `.py` 文件。
- **把 bash heredoc / `&&` 链 / `$(…)` 当通用语法**：PS 5.1 不是 POSIX shell。多步就多次 Shell，或一个 `.py`。
- **`curl` 无后缀**：PowerShell 里 `curl` 常是 `Invoke-WebRequest` 别名。HTTP 冒烟用 **`curl.exe`**。
- **默认 GBK 打印 Unicode**：中文日志、勾号会让 Python 报 `UnicodeEncodeError: 'gbk' codec can't encode`。Python 脚本开头：`sys.stdout.reconfigure(encoding="utf-8", errors="replace")`；调用前可设 `$env:PYTHONUTF8='1'`。
- **Docker 引擎没起来就 compose**：`open //./pipe/dockerDesktopLinuxEngine` 失败时先启动 Docker Desktop，等 `docker info` 通再重建。不要把管道错误当成 compose 写错。

### 编码与国内网络

- 中文 Windows 上 Python/`subprocess` 不指定 encoding 就会按 GBK 编解码。读写文件、打日志一律 **UTF-8 + `errors="replace"`**。
- Docker / `npm install` 直连 `registry.npmjs.org` 可能 `ECONNRESET`。需要时走用户级 `%USERPROFILE%\.npmrc` 的镜像，**不要**为「再试一次」把生产 registry 或密钥写进仓库 `.npmrc`。

### Cursor 工具层（命令还没进仓库就失败时）

若**所有** Agent 命令都在 `%TEMP%\ps-script-*.ps1` 约第 34 行失败（`FromBase64String(''{1}'')`、`Missing ')' in method call`），是 **Cursor 终端包装脚本的转义 bug**，不是本仓库命令写错。社区 workaround：Settings → Agents → 打开 **Legacy Terminal Tool**，`Terminal: Kill All Terminals`，再重启 Cursor。[论坛讨论](https://forum.cursor.com/t/powershell-execution-error-string-escaping-bug-in-temporary-script-generation/145894)。用户路径里若有 `'`（如 `Hector's PC`）也会打崩同一包装层。

这与「命令已执行、但 PowerShell/Python 转义失败」要分开看：后者按上表改写法即可。

---

## 项目是什么

- **名称**：XallorRemote（CLI `xallor-remote`）。
- **形态**：Agent 远程设备执行层。凭设备 ID + 授权码，经出站 Relay 把任务打到 NAT 后的 Windows/Linux，流式回流。
- **读法**：产品从 [`docs/README.md`](docs/README.md) 进。栈与复用 [`docs/stack.md`](docs/stack.md)。头关系 [`docs/heads.md`](docs/heads.md)。

## 技术栈（改代码时）

| 部分 | 选型 |
| --- | --- |
| Runtime / Relay / CLI / TUI | Go，单一二进制 |
| MCP | TypeScript，`xallor-remote-mcp`，官方 `@modelcontextprotocol/server` |
| GUI v0.1 | Tauri 2，只连本机 IPC |

库与「不要造的轮子」只在 [`docs/stack.md`](docs/stack.md) 改。

建议目录（落地时）：

```text
cmd/xallor-remote/
internal/ipc/  runtime/  relay/  protocol/
packages/mcp/
apps/gui/          # v0.1
docs/
```

---

## 编码约定

- 先定模块再落盘，见 `modular-file-design.mdc`。
- 协议字段、错误码只在 [`docs/protocol.md`](docs/protocol.md) 改；过程只在 [`docs/dataplane.md`](docs/dataplane.md)。
- 用户向文案（CLI 输出、GUI、MCP 人话）见 `user-facing-ui-copy.mdc`。
- **每出现一个已定位的 bug，必须补至少一个回归测试**（可用更高层测，不要求只测单一 case）。
- 测试命名建议 `should_<expected>_when_<condition>`（或同等结构），名称体现行为与触发条件。
- 测试函数前补简短注释：目的、关键前置、预期。

## 给代理的操作原则

- Git / 本地构建 / 生产 / Shell：以 **「首要原则」** 为准，正文在对应 `.cursor/rules/`，不要在本文件再抄一遍。
- 改动范围尽量贴合需求；不要编辑本文件来替代 README。用户向外的「如何运行」以 [README.md](README.md) 为准。
- **文档**：改口径只改 [`docs/README.md`](docs/README.md) 表里的那一份；不要每次改完去同步所有相关文档。
- 不要把 `host-execution-mcp` 的 transport 揉进本 Runtime；v0 不要为对齐去改那边的 tool 名。
