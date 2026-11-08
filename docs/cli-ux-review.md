# CLI 易用性审查（#73，建议稿）

结论：当前 CLI 能覆盖执行路径，但对初次使用者和 Agent 的参数组织还不够友好。生命周期命名、目标参数统一、授权副作用提示和查询输出一致性仍待收敛。本次仅修复已确认的 exec 参数解析阶段 JSONL 错误通道，不重命名参数、不改变 Request/IPC/MCP 契约或冻结的 tool 名。

治理门槛：#57–#60 与 #73 共同治理已有能力；#60 与 #73 的未完成验收不能被本次审查替代。在这些门槛通过前，不启动新的 Runtime 后端、租户权益或 Relay 高可用功能。Agent 友好与用户友好是同一产品的两条验收线，须同时通过。

## 验证范围

在 `origin/main d33d0fa` 加本阶段候选上生成实际 root、ensure、start、peer add、exec、grant issue、inbound、relay、status、doctor、runtime help，保存于本地 `dist/verification/cli-help/`。用隔离 profile 和 loopback Relay 实跑 ensure 重复调用、重复 start、签发/重签授权与 inbound 开关；未用默认 profile 或官方 Relay。源码对照 `internal/cli/{root,exec,ensure_runtime}.go`、`internal/noderun/device_settings.go` 和 MCP `exec-input.ts`。

| 入口 | 实测现状 | 使用成本 / 后续建议 |
| --- | --- | --- |
| `ensure` / `start` | ensure 后台确保且幂等；start 前台常驻，已有进程时失败；help 没有成功示例 | 普通调用本就自动 ensure，不应把显式启动当作必经步骤。建议下一版 `start` 表示幂等后台启动、`run` 表示前台；这会改命令语义，单独实施 |
| `peer add --id` / `exec --device` | 同一个对方号码用两个参数名；MCP 是 `device_id` | 建议统一 CLI 为 `--device`，peer 指本地保存的对方记录，device 指设备 ID，避免再引入 target 同义词。MCP/IPC 保持 `device_id` |
| `grant issue` | 会生成新码并打开入站；已有授权时再次 issue 也换码 | help 只写“签发（仅本机）”，没有说明两个副作用；issue 与 rotate 区别不直观。下一版 issue 已有时不隐式换码，rotate 专门换码；开启入站须明确 `--enable-inbound` 或单独 `inbound on` |
| `inbound on` | 缺授权时自动生成码；off 保留码 | 建议开关仅改变开关；缺码给出明确签发下一步。若保留快捷组合，应在命令与返回中明确后果 |
| 执行输入 | 单个脚本参数、script-file、program、request-file；三个显式模式互斥；program 不接受 shell；request-file 不叠加 cwd/timeout/shell | 互斥实现合理，help 需先给最短 `--program ... -- args`，然后脚本文件，最后 JSON 高级模式。保留同一 Request，不再建第二套执行协议 |
| `--cwd` / `--shell` | 指对方目录/解释器，已在 flag 描述说明；script-file/request-file 是本地输入 | help 和示例应持续明确本地/对方。复杂文本优先 UTF-8 文件；程序参数以数组发送，不靠 Shell 重拼 |
| `--timeout-ms` | 0 无期限，最长 3600000ms | 人用可增加 `--timeout 30s` 并在 CLI 转换；Agent 的 JSON 继续使用 timeout_ms。两种 CLI 超时选项不能同时指定 |
| JSON / JSONL | status/doctor 有 JSON，exec 有 JSONL；ensure/peer list 仍是人话；本次已把未知 flag/缺值/类型错误接入 exec 的唯一 JSONL error，保留退出码和 stderr | 参数错误通道已修复；查询 JSON 一致性仍待收敛。Agent 始终检查退出码与明确终态，不解析人话 |
| 高级管理命令 | root 同时展示 account/device/relay；principal 已在 relay 下 | root help 分组展示常用操作、本机授权、服务管理、自托管管理；不改 Runtime 中枢，不把普通流程引向账号或租户配置 |

## 最短成功路径与高级路径

当前已安装的共享码模式：被控机运行 `grant issue`，显示设备 ID 可用 `status`；控制机 `peer add --id <ID> --grant <码>` 后 `exec --device <ID> --program whoami`。每个业务命令会自行 ensure，**无需额外把 start 和 ensure 都执行一遍**。自托管先为双方配置可达的安全 Relay URL；localhost 只供同机开发，不能拿另一台电脑的 localhost 当目标。已有一台 peer 时 exec 可省 device；多台时应显式选择。

建议的长期参数形状（**尚未实现，不能照此运行**）：

```text
# 被控机：明确签发并允许入站
xallor-remote grant issue --enable-inbound
# 控制机：对方设备参数只用一个名字
xallor-remote peer add --device <ID> --grant <码>
xallor-remote exec --device <ID> --program whoami
# 复杂脚本与 Agent 请求仍复用现有执行形状
xallor-remote exec --device <ID> --script-file task.ps1 --shell powershell --timeout 30s
xallor-remote exec --device <ID> --request-file execution.json --jsonl
```

PowerShell 5.x 与 POSIX shell 的引号/空参数不能混用。`--` 只终止 CLI 自身的 flag 解析，不能阻止本地 Shell 先展开 `$`、反引号或丢掉空参数。UTF-8（可带 BOM）request-file 是 Unicode、空参数和跨系统调用的推荐路径；不要让 Agent 把 JSON 嵌入多层命令字符串。Windows PowerShell 5 的默认 `>` 编码是 UTF-16，不能用它生成这些 UTF-8 输入文件。

## 文档与验收缺口

Agent 线：现有 Request/IPC/MCP 复用、JSON/JSONL、非零退出码、request-file、原样 argv 已有真实验证；本次补齐 exec 参数解析阶段 JSONL，查询输出一致性仍待收敛。用户线：最短成功路径、命令含义和默认值已审查；`doctor` 的未配对/无权限/入站关闭/离线/传输错误均在本轮真实验证，双语 README 已补安装、首次执行、PowerShell/Linux 开发与错误恢复入口，本次 exec help 也补中英文参数错误说明；其余常用命令 help 仍缺一致的语言策略。两条线都不能仅凭现有单测通过宣告完成。

审查最初发现原仓库没有 `docs/cli.md` 或英文 README。现已增加 `docs/cli.md` 指针入口与 `README.en.md`，CLI 事实来源仍为 `docs/clients.md` §3。中英文 README 的代码示例与规范文档链接保持对齐，覆盖人用最短路径、Agent JSON/JSONL/request-file、授权副作用、安全 Relay、安装验证边界和诊断恢复；文档检查进入 CI。Cobra 标题/部分输出仍为英文、业务说明为中文，本地化 help 与命令/错误输出的收敛仍由 #73 单独实施。文档示例不等于已完成 Linux 或 PowerShell 原生转义矩阵实测。

本阶段已运行 Windows 上的 execution-input E2E（程序 argv、空参数、Unicode、UTF-8/BOM 脚本文件、request-file、工作目录、IPC/MCP 输入）、JSONL E2E、真实 GUI/MCP 和跨层路径。CLI 参数互斥与 JSONL 语义错误也由 Go 单测覆盖。Linux 当前仅交叉编译，未在 Linux 实跑这些行为；PowerShell 命令行原生转义矩阵也不能由 Python subprocess 的 argv 测试替代。

#73 剩余验收：冻结上述命名决策，一次同步 CLI help、中文/英文 README、CLI 文档及 MCP 的对应描述；覆盖 Windows PowerShell 5、pwsh、Linux sh/bash 原生命令行、stdin/file、Unicode/空参数。未知 flag 的 JSONL 单点修复已有平台无关的 Go 回归和真实 CLI 脚本，并接入 Windows/Linux CI；本地 Linux 执行仍取决于可用环境，不能把交叉编译当作 Linux 实跑。保留既有错误码、Request 与 Runtime IPC，不增加旧参数双读/双写兼容层。本阶段不将 #73 整体标为完成。
