# Cursor CLI 会话交接试点

本试点把评审材料交给目标机器上**明确指定的 Cursor CLI 会话**。Runtime 管理接收、执行与回执；Relay 只使用既有鉴权和转发。它不是通用任务平台，也不导入 Cursor 私有数据库，不承诺 IDE 窗口自动同步或完整记忆迁移。

## 目标机器准备

在目标机器上安装并登录官方 Cursor CLI，使用官方 `create-chat` 创建会话，记录会话 ID。在目标 Runtime 的 workspace 内准备干净的 Git 仓库，记录完整 HEAD SHA。身份、授权码和入站规则仍按现有配对流程配置。

仅本机可配置适配器，远程请求不能选择可执行文件：

```powershell
xallor-remote handoff configure --executable C:\tools\cursor-agent.exe
```

若官方 Windows 包使用 Node 入口：

```powershell
xallor-remote handoff configure --executable C:\tools\cursor-cli\node.exe --prefix-arg C:\tools\cursor-cli\index.js
```

此配置属于当前 Runtime 数据目录。登录凭据由 Cursor 自己管理，XallorRemote 不读取或复制登录密钥。每次执行前检查 CLI 帮助和版本；启动成功不代表登录有效或请求完成。

## 提交与查询

交接 JSON 的完整字段与限额以 [protocol.md](protocol.md#cursor-交接载荷与回执) 为准。必填目标仓库绝对路径、完整提交 SHA、会话 UUID 和模式。`ask` 用于只读问答；`plan`、`agent` 必须显式选择，不根据材料文本升级模式。`trust_workspace` 单独决定是否传递官方 `--trust`；不传 `--force` 或自动批准 MCP 的参数。

```powershell
xallor-remote handoff submit --device 123456789 --request request.json
xallor-remote handoff get --device 123456789 --request-id review-001
```

提交只返回已保存的回执，不等待 Agent 完成。查询不会启动 Agent。网络中断后保留原请求 ID，先查询；相同 ID、相同内容返回已有回执，内容冲突拒绝覆盖。不得用新 ID 自动重试未知结果。

请求 ID 作用于 Relay 推导的主体、租户、设备实例与归属实例。不同主体可使用相同 ID，但不能查询或覆盖彼此的回执。凭据续签和重新授予同主体访问权保持幂等；设备转移（包括转回原主体）进入新作用域。未认领设备的无账号访问按共享授权实例隔离，同码持有人属于同一组，不能区分为个人。具体 scope 字段见 [protocol.md](protocol.md)。

## 结果含义

`received` 表示请求已持久化；`accepted` 表示仓库、附件与 CLI 检查通过；`running` 只在 CLI 返回匹配的初始化事件后出现。只有匹配会话与 workspace 的初始化、匹配会话的成功结果事件、进程正常退出、受监督进程回收确认以及作业占用释放已持久化，才记为 `completed`。回收或占用释放未确认记为 `unknown` / `job_release_unconfirmed`；作业占用见 [runtime.md](runtime.md)，进程边界见 [os.md](os.md)。这是 CLI 回执，不验证答案正确性或承诺修改已合入。

检查失败或明确错误结果为 `failed`；缺失终态、会话不匹配、超时或执行中断为 `unknown`。Runtime 重启将未结束回执标为 `unknown`，绝不恢复执行。关闭控制端连接仅结束提交/查询，不取消已持久化的交接；目标 Runtime 停止或请求超时会终止它负责的 CLI 进程树。

Runtime 应用 Relay 的当前设备作用域后，认领、转移、未认领设备共享授权轮换或关闭入站会取消不再匹配的后台 worker，并拒绝迟到的旧作用域提交。取消中断记为 `unknown` / `device_authorization_changed`；进程回收或占用释放未确认仍优先保留对应 unknown 诊断与占用。管理成功只确认设备状态同步和取消请求发出，不等于后台已停止。普通短暂断线可在最后许可有效期内恢复；重连以当前作用域和原始委托重新核对，丢失的归属变更确认在重连时补齐。已认领设备轮换共享码不改变账号作业作用域。后台 worker 启动前取得执行许可，运行中持续复验原凭据、成员版本和分享实例；撤销、到期或网络分区导致许可无法延续时取消。恢复成员、续签凭据或重新分享不能复活原委托。许可失效与回收仍是两件事，数值和诊断见协议。

工作区检查不是文件系统快照。检查后外部程序仍可能修改仓库；需要稳定评审时，应使用专用 worktree 并避免其他写入。Ask/Plan 行为依赖 Cursor 官方模式，不是 OS 沙箱。

## 留存与运维边界

记录存于目标数据目录 `handoff/requests/`，文件名为可信 scope 与请求 ID 的组合摘要，内部保存 `{scope,receipt}`。公开回执包含有限长度结果、会话、仓库、SHA 与时间，不包含 scope、原始 prompt、凭据或 CLI stderr。结果本身可能包含用户内容。存储损坏或达到总容量时停止接收新请求，不静默清空或淘汰旧 ID。当前没有在线清理/迁移 API；删除记录会丢失去重和会话归属，因此不得作为自动恢复手段。

此前无可信身份的 `handoff/receipts/` 文件保持原样，不读取、不自动迁移、不猜测所有者。切换前应先停止接收交接并核实在途任务；既有任务不能因新作用域查不到回执就自动重提。此存储切换不提供跨旧格式的去重保证。

首次持久接收交接即将会话绑定到该 scope；即使检查失败、任务结束或 Runtime 重启，其他 scope 也不能用新请求 ID 恢复此会话。跨身份或设备归属变更后须创建新会话。同一会话仍只允许一个在途请求，跨会话并发也有上限。

这些保障限于本 Runtime 管理的交接记录，不读取 Cursor 私有历史，也不能判断首次引入的外部会话原属谁。其他 CLI、另一 Runtime、拥有本机数据目录权限的用户或获授任意 exec/read 权限的主体仍可能访问目标 OS 用户的数据；这不是 OS 沙箱或跨进程全局会话锁，也不提供 exactly-once 执行保证。严格的 OS 用户隔离需独立执行账户/沙箱。

## 验证边界

`tests/handoff_e2e.py` 使用真实 CLI→IPC→Runtime→Relay→目标 Runtime 链路和确定性 Cursor 协议替身，覆盖去重、冲突、仓库绑定、异常流及重启。真实官方 CLI 另需登录后在新建测试会话执行 Ask 请求，并验证再次恢复同一会话；替身测试不能替代这一步。

`tests/handoff_scope_e2e.py` 验证认领前后、跨主体同 ID、会话绑定、凭据续签、重新分享、Runtime 重启和归属转移；Relay 的真实 TLS WebSocket 回归验证伪造 scope 和凭据字段不会传到目标。

官方接口参考：[CLI 参数](https://cursor.com/docs/cli/reference/parameters)、[输出格式](https://cursor.com/docs/cli/reference/output-format)、[认证](https://cursor.com/docs/cli/reference/authentication)。
