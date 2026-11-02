# Runtime：被控与本地枢纽

SSOT：本机执行、策略、workspace、本地数据。CLI 见 [clients.md](clients.md)。denylist 见 [os.md](os.md)。授权谁能改见 [credentials.md](credentials.md)。

---

## 1. 职责

长驻、用户态（v0 不做 SYSTEM/root）。ensure / CLI / GUI 装起，幂等。

- `hello_device` + heartbeat
- 身份；`grant issue` 前入站关
- 收 invoke → 策略 → 执行 → 回推事件
- cancel；断线杀无主进程
- 对本机头提供：identity、peers、审批、**仅本机**的 grant/inbound/revoke
- 代发 `hello_client`（控别人）

不做：MCP stdio、屏幕采集、经 MCP 签发或吊销 grant。不做命令语义沙箱（容器 / 受限 token）。

### 本机作业占用

`internal/job` 管理前台 exec、后台 handoff（包括预检）的共享容量。启动进程前，在 profile 的 `jobs.json` 原子持久化占用，OS 文件锁 `jobs.lock` 保证同一 profile 只有一个账本写入者；写入失败不授予启动能力。所有进程统一通过该作业的 Owner 启动，真实监督回收完成并持久更新后才释放。容量和大小见 [protocol.md](protocol.md)。

账本只含随机内部作业 ID、Relay 推导的稳定 scope、作业种类、内部关联键和 occupied/uncertain；不保存命令、提示、路径、凭据或输出。前台内部关联键为本次路由 ID，handoff 为会话 UUID。交接回执继续负责结果和请求去重，不能拿它的 completed/failed/unknown 推断资源占用。非零退出但回收确认可以释放；回收或释放持久化未确认时，前台返回错误，handoff 为 unknown/job_release_unconfirmed。

Runtime 重启把遗留占用标为 uncertain，保留额度，不重放任务；不会因为原父进程不在或回执已标 unknown 就清除占用。同一受信作用域的未确认交接会话不能换 request_id 再次启动。当前没有自动核实/解除不确定占用的恢复入口，不能手工删除 jobs.json 来宣称已确认清理；后续需真实 OS 作业恢复与授权恢复操作。正常停止会等待前台与交接收尾，再释放账本文件锁。

这是本机执行容量，不是会员权益、租户全局配额或 Relay 租约；此阶段尚无费用归属或计量。升级前需停止旧 Runtime 并确认其任务已结束，不能从旧回执猜测补造占用。OS 监督边界见 [os.md](os.md)。

---

## 2. 数据目录

```text
Windows: %APPDATA%\XallorRemote\
Linux:   ~/.config/xallor-remote\
```

IPC 报文见 [ipc.md](ipc.md)。头关系见 [heads.md](heads.md)。

可用绝对路径环境变量 `XALLOR_REMOTE_DATA_DIR` 选择独立本机 profile；默认 workspace 位于该目录的 workspace 子目录，Go 与 MCP 使用相同 profile 对应的独立 IPC 端点。首次加载时创建目录。该设置用于隔离实例和测试；不设置时保留上面的用户级位置。不同 profile 的身份与授权互不复用。

| 文件 | 内容 |
| --- | --- |
| `identity.json` | device_id |
| `device_secret` | 仅当前用户可读 |
| `grant` | 仅 issue 之后；明文只为再显示 |
| `peers.json` | 对方 id + grant（**密钥库**） |
| `config.json` | relay、workspace、shell |
| `policy.json` | capability 开关、approval 列表（可选） |

---

## 3. Workspace

未指定则使用默认沙箱并在首次创建（见 [decisions.md](decisions.md)）。启动 / status 必须打印当前路径。所有 read/write 与默认 cwd 在此树内。穿越 → `policy_deny`。目录丢失 → `workspace_missing`，不要退回盘符根。

---

## 4. 循环

```text
hello_device (inbound 按是否已 issue)
loop heartbeat
on invoke: 策略 → exec|read|write|processes|info|cancel
on disconnect: 退避重连；不恢复旧子进程
```

改授权的消息只从本机 IPC 进来，再由本进程用 **device 连接**发给 Relay。

---

## 5. exec

禁止 `run()` 完再整包发送。必须 Popen，按行/块推 `stdout`/`stderr`，最后一条 `exit`。管道要持续读。帧大小、截断、cancel、断线杀进程见 [dataplane.md](dataplane.md)。

- Windows：Job Object 杀树。Linux：process group。
- 字节转 UTF-8 再上送，见 [os.md](os.md)。
- 继承用户环境；不接受任意远程 env。
- **v0 无交互 stdin。** 不要做成 PTY。
- 并发见 [protocol.md](protocol.md)。

---

## 6. read / write / 进程 / info

**read：** workspace 内（`os.Root`）；`head`/`tail` 按行（去掉文件末空段）；`offset`/`limit` 按字节；默认当 UTF-8 文本；明显二进制 → 拒绝或 `too_large` 策略（v0 可直接拒绝）。上限见 protocol。超过单帧则分帧上送。

**write（写死）：**

- 仅 workspace 内（`os.Root`；junction / 逃出根拒绝）
- **整文件覆盖**；不 append、不 patch、不部分写
- 先写同目录临时文件再替换（原子替换）；失败则旧文件保留
- 上限 1 MiB，超则 `too_large`，磁盘上不留半份新内容
- 并发写同一路径：后完成的替换胜出，不提供锁

**processes：** 当前用户可见即可。**info：** os/arch/hostname/version/workspace；可选探测 docker / nvidia-smi / git（false 即可）。

---

## 7. 策略合同（诚实模型）

v0 **不**解析「这是 `npm install` 还是 `rm`」来当沙箱。命令分类器一定会被绕过。

**grant 持有者 ≈ 这台机器上的当前用户，作用域缩在 workspace + denylist。** Policy 是减伤，不是隔离。

判定顺序（任一拒绝即停）：

```text
1. Relay 已校验 grant / inbound（本步不再验码）
2. 该 op 的 capability 已打开；未知 capability → deny
3. 文件类：规范化后的路径必须在 workspace 内
4. 命中 [os.md](os.md) denylist → policy_deny
5. 命中「需审批」规则：
     有 TTY 或本机 UI → 等待；超时 → approval_timeout
     无 TTY 且无 UI（典型无头 Linux）→ **直接 policy_deny**
     不要挂死等一个不存在的人
6. 否则 allow
```

v0 capability：

| Capability | op |
| --- | --- |
| `device.inventory` | info |
| `shell` | exec / cancel |
| `filesystem.workspace` | read / write |
| `process.list` | processes |
| `system.info` | info 内 |

默认：上述开启；不默认 sudo / 提权。无头机不要指望审批流，把 denylist 当主防线。

Relay 已鉴权不能替代本步。

---

## 8. 卸载

断开；本机 `revoke` 通知 Relay；删数据目录。只删二进制会留下 identity，重装抢同一 ID。
