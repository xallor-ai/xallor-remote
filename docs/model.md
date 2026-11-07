# 产品模型：一个 Node

SSOT：安装形态、幂等、双向、本机进程。头之间谁听谁见 [heads.md](heads.md)。选型见 [decisions.md](decisions.md)。

---

## 1. 一台电脑

用户装的是 **XallorRemote**，不是三套互不相认的软件。

```text
本机 Runtime（Go，长驻，一个）
  ├── 数据：identity、inbound、issued grant、peers、policy、workspace
  ├── IPC：见 [ipc.md](ipc.md) / [decisions.md](decisions.md)
  ├── 连接 A：hello_device → Relay
  └── 连接 B：hello_client → Relay
        ▲
   MCP（TS 短驻）   CLI / TUI / GUI
（四个头同级，都只连 IPC；GUI 不是中枢）
```

Relay 默认官方 hosted（`wss://api.xallor.com/remote`），可改 URL 自托管。同域的 `/api` 仍是账号/壁纸 API。

---

## 2. 通用包与幂等

- 主安装面：npm **`xallor-remote-mcp` 一个包**（MCP + 内嵌 Runtime）。控制端装一次即可；被控端也可用同一包的 `xallor-remote start` / `grant` / `inbound`。
- 纯 Go 发行（无 Node）仍保留：GitHub Releases 的 `xallor-remote` 单文件。
- 第一次被 Cursor 拉起：ensure → identity 已有则不重建 → **不** grant issue → 经 Runtime 干活。
- `xallor-remote mcp merge-config` 只幂等追加本产品条目。

| 再做一次 | 必须 | 禁止 |
| --- | --- | --- |
| 安装 / 开 MCP | 仍一个 Runtime，identity 不变 | 第二套服务、新 device_id |
| 小版本升级 | 换二进制，留数据目录 | 当全新安装 |
| Runtime 已在跑 | 不杀，除非强制重启 | 每次开 Cursor 闪断入站 |
| 用户改过 workspace / 策略 | 保留 | 恢复出厂 |
| mcp.json 已有本 server | 不重复插入 | 每次保存再追加一条 |

身份破坏：`xallor-remote revoke` 销 Relay 登记；`reset --yes` = 本机清空 + revoke。`grant rotate` 只换码，不换 `device_id`。

---

## 3. 双向（未认领设备的共享模式示例）

```text
B 执行 grant issue  →  A peer add B  →  A 控 B
A 执行 grant issue  →  B peer add A  →  B 控 A
```

两份 grant。入站默认关。ensure 后 Runtime 保持经初始设置确认的 device 连接（可 online 且 inbound_disabled）。

Grant 是 bearer：多 Cursor / CLI 共享被控机这一份码，不绑控制端身份。签发、rotate、inbound、revoke **只在被控本机**经 IPC；MCP 和对端不能改授权。见 [credentials.md](credentials.md)。

无桌面 Linux：`xallor-remote peer add` + `exec`，或本机也跑 MCP。

---

## 4. 控制路径

```text
Cursor ──stdio──► MCP ──IPC──► Runtime ──WSS──► Relay ──► 对方 Runtime
人     ──CLI/TUI/GUI ────────► Runtime ──WSS──► Relay ──► 对方 Runtime
```

mcp.json 单台 `XALLOR_REMOTE_DEVICE_ID` + `GRANT` 由 Runtime 收编进 `peers.json`。MCP 不持有长期 WSS。
