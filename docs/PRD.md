# PRD v0.8 — XallorRemote

| 字段 | 内容 |
| --- | --- |
| 状态 | 产品范围。读法见 [README.md](README.md)；头关系见 [heads.md](heads.md)；Relay 转发见 [dataplane.md](dataplane.md)；栈见 [stack.md](stack.md) |
| 产品名 | **XallorRemote** |
| 日期 | 2026-08-27 |

---

## 1. 一句话

任意一台授权电脑成为 Agent（以及人）可调用的执行节点：凭 **对方设备 ID + 授权码** 把任务送过去，**实时回流**。无需公网 IP、SSH、开放端口。

同一套安装既可被控，也可反过来控对方（两份授权）。不是远程桌面，不是 MCP 版 SSH。

---

## 2. 为什么做

Agent 能调 API，不能安全地用用户自己的电脑。SSH MCP 要端口和可达性；向日葵为人看屏幕；`host-execution-mcp` 管企业跳板 / k8s，不管家庭 NAT 后的个人设备。

用户要说的是：「去我那台 Windows 跑这个」，并在对话里看着输出往下走。

---

## 3. 原则

1. 主路径：ID + 授权码 → 任务 → 流式结果。
2. 一个 Node，多个头；禁止控制端专用包。
3. 安装幂等；身份直到本机 `revoke` / `reset` 才死（`rotate` 只换 grant）。
4. 双向 = 两份 grant；入站默认关；首次启动不签发授权码。
5. 控制头经本机 Runtime 再出站；Cursor 关 MCP 不影响入站。
6. 流式默认（含 GUI 会话屏）。
7. 设备只出站；Relay 转发载荷、只存元数据。
8. 签发、轮换、吊销、开入站只走**被控机本机**人或 CLI，不走 MCP，也不走持 grant 的对端。
9. Grant 是可复制的 bearer capability，不绑定控制端身份；泄漏则本机 rotate。
10. 账号与订阅是后一层，见 [platform.md](platform.md)。
11. OS 差异只写在 [os.md](os.md)。
12. 默认走官方 hosted Relay，自托管随时可换；Relay 看得见飞行载荷、v0 不 E2E，见 [decisions.md](decisions.md) / [relay.md](relay.md)。

---

## 4. 范围

### v0 Must

- 通用 MCP 包 `xallor-remote-mcp` + 幂等 ensure Go Runtime（MCP 只连 IPC，不连 GUI）
- Windows + Linux Runtime：身份、出站、入站默认关、`grant issue`、流式 exec、默认 workspace、进程、本地策略
- CLI 覆盖人侧全部操作
- Relay：官方 hosted 默认 URL + `xallor-remote relay` 自托管；按 [dataplane.md](dataplane.md) 做鉴权路由与流式转发
- 两台机器互换 grant 后都能 exec（可经官方 Relay，无需彼此公网 IP）

### v0.1

- Linux TUI、Tauri GUI、macOS Runtime、`xallor-remote mcp merge-config`

### v1

- hosted 登录入口、名下设备、云侧管理与订阅；未认领设备的 mcp.json 仍可使用共享码

### 不做（v0–v1 默认）

远程桌面 / 键鼠 / WebRTC / P2P / 目录同步 / K8s / Computer Use / 支付与组织 RBAC / MCP 签发或吊销 grant / 浏览器当第三条执行通道 / 短配对码 / Electron 壳 / PTY 与交互 stdin / 命令分类器当沙箱 / 绑定控制端的 grant / 载荷 E2E。

---

## 5. 成功标准

双 OS 都过才算 v0。清单按四类，细节命令在 [os.md](os.md)。

**Identity：** install / 再开 Cursor / 升级后 `device_id` 与 secret 不变；`reset` 才变；默认 workspace 打印且无授权码、入站关。

**Transport：** 断网 `device_offline`、恢复 online；Relay 重启后设备重连；官方 URL 与自托管 URL 都能跑通。

**Authorization：** 未认领设备共享模式下，`grant issue` 后对端凭 ID+码可 exec；错误 grant → `unauthorized`；入站关 → `inbound_disabled`；**对端 MCP 不能 rotate/revoke**；本机 rotate 后旧码立刻失效；反向第二份 grant 独立。已认领设备采用账号与设备访问授权，边界见 [platform.md](platform.md)。

**Execution：** stdout/stderr 流式（结束前至少两帧可见）；非零只走 `exit`；timeout / cancel（只能 cancel 自己的 exec，终态 `exit.status`）；workspace 外 write `policy_deny`；超大 write `too_large`；高危 denylist `policy_deny`；无头机审批类直接 deny；断线杀无主进程；`yes` 类洪水 `truncated=true` 仍有 `exit`；Relay 重启 inflight → `relay_error`。

无 DISPLAY 的 Linux：v0 用 CLI 完成 peer / 流式 / cancel。v0.1 另加 TUI/GUI 在结束前看到行。

---

## 6. 数据平面（核心）

产品发动机是 **A Runtime ↔ Relay ↔ B Runtime** 的 WSS 闭环，不是 MCP 本身。

Relay：**有鉴权的实时消息路由器**——device 连接、grant 校验、按 `exec_id` 转发、生命周期错误。不执行、不解释命令、不持久化 stdout。

六件事一次钉死（全文只在 [dataplane.md](dataplane.md)）：

1. 连接：两条出站长连接；v0 一条 client 只绑一个目标。
2. 请求：`invoke(exec_id)`；Relay 校验后登记 inflight 再转给 B。
3. 路由：`inflight[exec_id] → client_conn + device_id`。
4. 流：B Popen 推 `stdout`/`stderr`/`exit`；Relay 原样转发。
5. 取消：仅本 client 的 inflight → B 杀树 → `exit.status=cancelled`。
6. 断线/背压：不恢复旧 exec；洪水则 `truncated`，终态仍要送达。

---

## 7. 路线图

| 阶段 | 内容 |
| --- | --- |
| v0 | 执行闭环 + CLI + 官方 Relay 默认 + 自托管 |
| v0.1 | TUI / Tauri GUI / macOS |
| 受限试点 | [Cursor CLI 指定会话交接](handoff.md)：显式模式、持久回执、未知结果不重放；不等于通用 A2A |
| v1 | 托管登录入口与设备清单 |
| v1.x | 计量与套餐 |
| v2 | 组织、fleet、P2P 旁路 |
| 以后 | Desktop capability、A2A |

---

## 8. 和 host-execution-mcp

互补，不是替代。那边是 SSH / 跳板 / k8s；这边是个人设备 + 出站中转。Capability / 审计语义应对齐，**v0 不要为了对齐去改 host-execution 的 tool 名，也不要把两套 transport 揉成一个 Runtime。** 不要在 v0 重做异步大文件 copy。
