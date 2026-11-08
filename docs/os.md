# 操作系统差异

SSOT：Windows / Linux / macOS 行为。协议与 tool 名字相同；差异只在 Runtime。Relay 不解释路径。

---

## 1. 矩阵

| 能力 | Windows x64 | Linux x64 glibc | macOS |
| --- | --- | --- | --- |
| Runtime + MCP + CLI | v0 Must | v0 Must | v0.1 |
| TUI | 可选 | v0.1 Must（无 GUI） | 可选 |
| GUI | v0.1 Should | 可选 | v0.1 |
| 开机启动 | Should | Should | Should |
| SYSTEM/root 服务 | 不做 v0 | 不做 v0 | 不做 v0 |
| ARM | 以后 | 以后 | 以后 |
| WSL 当 Linux 目标 | 用 Linux 包 | — | — |
| 截屏 / 键鼠 | 不做 | 不做 | 不做 |

双 OS 都过才算支持。v0 无 GUI Linux 走 CLI，不依赖 TUI。

---

## 2. Shell

各头按 [protocol.md](protocol.md) 传 script 或 program。目标 Runtime 的 `internal/execution` 校验并构造命令，脚本通过 `internal/shell` 选择解释器，`internal/job` 拥有启动与清理。Windows 支持 powershell/pwsh；Linux 支持 bash/sh/pwsh。`auto` 在 Windows 使用 powershell，在 Linux 优先 bash，只有找不到 bash 时才选 sh。显式选择不会回退，不接受任意可执行路径或带参数的 shell 字符串；不支持 cmd。已开始执行后无论解析失败、非零退出还是断线，都不会换壳重试。

PowerShell 禁用 profile 和交互，脚本按 UTF-16LE/Base64 放入固定 ASCII 启动表达式后解码执行，避免原始脚本参与原生命令行转义；UTF-8 输出初始化位于外层启动表达式，保留用户块中 `using` / `param` 声明所需的首部位置。不用会使 Windows PowerShell stderr 变成 CLIXML 的 `-EncodedCommand`。使用目标 PowerShell 的语法树把状态检查放到最后执行块内，支持命名的 begin/process/end，保留 dynamicparam 参数绑定和受支持版本的 clean 清理；不按正则猜测，也不把普通语句拼在命名块之后。保留显式 exit，最后执行块的最后语句失败返回 1，成功返回 0，不提升之前原生程序遗留的 LASTEXITCODE。bash 禁用 profile/rc 并用 `-c`，sh 用 `-c`。不翻译 bash↔PowerShell，cwd 相对 workspace。配置操作见 [clients.md](clients.md)，能力和错误字段见 [protocol.md](protocol.md)。

能力探测仅查找目标可执行文件，不启动 profile 或版本探针；不承诺 shell 版本、插件、环境工具或后续命令一定可执行。PowerShell 相关行为依据 [官方 CLI 说明](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_powershell_exe?view=powershell-5.1)及[上游重定向 stderr 问题](https://github.com/PowerShell/PowerShell/issues/5912)，并以实际进程测试验证。

program 模式绕过 Shell 选择，直接启动程序，参数不经过变量、通配符、管道或重定向展开。裸程序名从目标 Runtime 的 PATH 查找，不隐式搜索 workspace；带目录的相对路径基于目标 cwd，绝对路径按目标系统解释，不在控制端转换。Windows 的 `C:foo` 盘符相对路径和 `\foo` 缺少盘符的根路径拒绝；Linux 的反斜杠是文件名字符。

Windows 启动 API 实际接收一条命令行，Go 按标准 Windows 参数规则编码；自定义命令行解析器仍可能有不同语义，不能承诺任意程序都与标准 argv 一致。`.bat` / `.cmd` 与 cmd.exe 不接受 program 模式；需要时写明确的 PowerShell 脚本调用并自行遵循批处理转义规则。原生程序的输出编码由程序决定，不保证 GBK 等输出自动转码。CLI 脚本文件仅避免控制端转义，仍使用上述目标启动方式和长度上限，不能据此认为支持任意长度 PowerShell 脚本。

---

## 3. 路径

Workspace 默认：`%USERPROFILE%\XallorRemote\workspace` / `~/XallorRemote/workspace`。打开限制用 `os.Root`：`..`、盘符跳转、junction / 逃出根的链接一律 `policy_deny`。写入全程在 Root 内完成，禁止先截断再当唯一检查。Windows 大小写不敏感（先规范化）；Linux 敏感。

---

## 4. 编码

上送永远 UTF-8。Windows 按控制台代码页解码再转（中文 stderr 不得乱码）。Linux 优先 UTF-8。二进制管道 v0 不承诺。

---

## 5. 进程列表

Windows：当前用户可见进程，pid + name。Linux：`/proc` 或 `ps`，pid + comm/cmdline 截断。不要求系统服务全家桶。

### 执行所有权

`internal/process` 统一拥有前台命令、交接 CLI 及其预检进程。父进程正常退出也要回收受监督子进程；取消、超时和 Runtime 正常停止同样等待回收。不能把父进程退出码 0 当作全部进程已结束。需要持续后台执行的任务应有明确的独立生命周期，不能依赖前台命令留下孤儿进程。

Windows 先创建带 `KILL_ON_JOB_CLOSE` 的 Job，通过 `PROC_THREAD_ATTRIBUTE_JOB_LIST` 在创建暂停进程时绑定 Job，再恢复保留的主线程句柄；文件、普通 reader/writer 与缺省标准流使用同一路径，不保留创建后再纳入 Job 的回退。普通流先适配为管道，缺省流使用 NUL；`HANDLE_LIST` 只允许三个标准流句柄继承，其他继承句柄、令牌和父进程替换等未支持属性在启动前拒绝。监督建立失败则终止并回收根进程，不降级为 taskkill 或只杀父进程。终止后查询 Job 的活动进程数，确认清空，再完成输出排空。Runtime 崩溃时最后的 Job 句柄关闭会终止已原子纳入的进程；本机持久占用的自动恢复仍待实现，不能从进程启动修复推断账本可清空。该创建属性要求 Windows 10 / Server 2016 及以上，见 [Windows 进程创建属性](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute)。

Linux 创建独立进程组，先用 `waitid(WNOWAIT)` 观察父进程退出但不回收，保留组 ID；终止进程组并检查 `/proc` 中不再有该组的活进程，再回收父进程。僵尸不再执行，由其 OS 父进程回收。`/proc` 无法读取或组未在期限内清空均视为回收未确认。此机制不能约束主动 `setsid/setpgid` 脱组、通过外部服务创建进程或 Runtime 崩溃后的任意进程；严格隔离需 cgroup/独立执行环境。macOS 执行监督尚未实现。

前台回收未确认返回执行错误；后台交接标记 `unknown`，不报告完成。`info.execution_backend` 只报告当前可用的 `native_user` 后端及其 `job_object` / `process_group` 清理边界；低权限用户、Windows 令牌降权和 Linux cgroup/container 仍是未实现的后端，不能把它们写进请求或自行假定启用。该进程组/Job 边界不是 OS 沙箱，尚不构成持久作业租约或会员额度释放凭证。OS 文档：[Windows Job Objects](https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects)。

---

## 6. 默认 denylist（策略在 Runtime）

Windows：`%USERPROFILE%\.ssh`、凭据/DPAPI、浏览器 Profile、`\\.\`；`Format-Volume`、盘根递归删、`shutdown` / `Stop-Computer` / `Restart-Computer`。

Linux：`~/.ssh`、`~/.gnupg`、`/etc/shadow`、`/dev/sd*`；`rm -rf /`、`mkfs`、`dd of=/dev`、`shutdown`/`reboot`、开头 `sudo`。

错误码都是 `policy_deny`。

---

## 7. 安装形态

主路径：**一个 npm 包** `xallor-remote-mcp`（内嵌本平台 Runtime）。控制端配 mcp.json；被控端用同包的 `xallor-remote` 子命令。

备选：GitHub Releases 的单个 `xallor-remote` exe/二进制（无 Node）。Windows 注意防火墙出站与杀软。Linux amd64（deb 可选）；不依赖桌面；写明最低 glibc（如 Ubuntu 22.04+）。macOS：公证成本高，故 v0.1。

官方中转默认 `wss://api.xallor.com/remote`（与 Tab 的 `/api` 同域分流）。

---

## 8. 验收（每 Must OS）

按 [PRD.md](PRD.md) 四类。最低集：

**Identity：** ensure/start 有 ID、默认 workspace 已打印、无授权码、入站关；再 ensure 同一 ID。

**Transport：** 断网 offline、插回 online；换自托管 URL 仍能 exec。

**Authorization：** issue 后对端三项即可流式 whoami；错码 `unauthorized`；入站关 `inbound_disabled`；**从控制端不能 rotate/revoke**；本机 rotate 后旧配置失败。

**Execution：** 中文不乱码；结束前至少两帧 stdout；workspace 外 write 失败；write 超 1 MiB `too_large`；高危 `policy_deny`；cancel 自己的 `sleep`/`Start-Sleep` 得 `exit.status=cancelled`；无 DISPLAY 用 CLI 走完 peer/流/cancel。服务端闭环见 [dataplane.md](dataplane.md) 文末验收。

Linux 无头：审批类高危必须是 deny，不能挂起。
