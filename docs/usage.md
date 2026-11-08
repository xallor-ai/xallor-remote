# Privacy-preserving usage / audit boundary

`internal/activity` 的 `activity.json` 是本机 bounded metadata history：只帮助本机 UI/CLI 展示请求阶段和终态，不是防篡改审计、不可否认日志、tenant ledger 或账单来源。它不保存命令、路径、prompt、stdout、grant、account token 或支付数据；损坏时报告不可用，不伪造空历史。

`internal/usage` 定义独立 usage event：`id`、时间、幂等键、tenant/membership/device scope、operation、outcome 和 units。`completed`、`failed`、`unknown` 是明确结果；保留窗口内相同幂等键与相同 event 是 no-op，冲突复用拒绝。At 必须为有界的规范 UTC RFC3339Nano；Append 省略 At 时生成一次，重复提交沿用已保存的时间，磁盘中缺失或非法 At 则拒绝整个 ledger。字段、时间范围和数字上限见 [protocol.md](protocol.md)。

retention 按追加顺序保留最近事件；被淘汰的幂等键只留下 SHA-256 tombstone，与事件在同一版本化 envelope 中原子写入。淘汰键的任何重用都返回 `ErrConflict`，包括原样重放，因为原 event 已不保留，不能再证明相同。tombstone 数量也有硬上限；达到上限后需要再次淘汰的 Append 返回 `ErrRetentionFull`，不删除 tombstone、不自动重置文件或推进新的幂等命名空间。已有事件的重复 no-op、读取与尚余的事件槽位仍可使用。该去重边界持续到同一 ledger 被显式替换或清除；清理文件不具备“安全开始下一周期”的语义。

`CompletedUnits` 仅汇总当前保留窗口内匹配 tenant/membership 的 completed 事件；unknown/failed 事件永远不计入 completed。int64 溢出返回 `0, ErrOverflow`，不返回环绕值、饱和值或部分总数，也不删除原始观察。它不是终身累计或账单来源。事件没有命令、路径、输出、IP、凭据或付款字段，tombstone 不是防篡改证明。

磁盘格式为 version 1 的 `{version,events,retired_keys}`，事件、tombstone 及总字节数同时受限。旧的 array-only 格式、未知版本、损坏或矛盾的状态均 fail-closed，保留原文件供处理，不自动迁移为空历史。一个 ledger 必须由单一写入者持有；原子替换和进程内互斥不提供跨实例/跨进程并发去重。写入失败后当前实例停止读写，重新打开以磁盘上的完整 envelope 为准，不自动重放观察。

`xallor-remote usage contract`、target `info.usage_event_contract` 与 MCP `remote.device_info` 返回同一 version 2 契约，含时间、retention、溢出与隐私规则，不读取余额、生成账单或证明审计完整性。真实 usage provider、跨 Relay 去重、租约/计费周期、不可篡改审计存储、账单和商业 retention policy 仍未实现；任何商业系统必须在独立管理面基于已确认终态和 tenant/membership 授权接入。
