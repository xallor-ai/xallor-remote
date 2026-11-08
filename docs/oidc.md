# OIDC / SSO 边界

当前实现提供 provider-neutral 的本地验证与映射契约，代码位于 `internal/oidc`。它验证一个已经由管理面取得的 compact JWT 和已经独立配置的 JWKS key set：issuer 必须精确匹配 HTTPS URL，audience 必须命中配置值，只接受带 `kid` 的 `RS256`，并检查 `sub`、`exp`、`nbf`、`iat`。未知算法、未知 key、坏签名、坏 claim、过期或尚未生效的 token 一律 `ErrUnauthorized`。

`KeySet.Replace` 是原子 key rotation 边界；Runtime 不联网发现 issuer，不抓取 JWKS，不接受 token 中的 `tenant_id`、`principal_id`、role 或 membership 作为本地授权。验证成功只产生 `{issuer,subject,audience,expires_at}`。`MappingService` 为本机控制面提供独立的 issuer 配置和 `(issuer,subject)` 唯一映射，SQLite adapter 保存版本及撤销状态。issuer 和映射必须 active、未过期，membership 必须 active 且 tenant/principal/version 与当前本地成员精确匹配；成员记录本身没有有效期字段。issuer/映射版本只能递增，撤销后保留版本，旧写入不能恢复旧绑定。每次 lookup 都重新检查，返回行的 issuer/subject 也必须匹配请求。

映射保存创建时的 `issuer_version`；写入事务复验当前 issuer 的 active/expiry/version，查询也要求该版本仍是当前版本。issuer 撤销或轮换后旧映射失效，重新启用 issuer 不会恢复旧绑定；管理员须明确以更高 mapping version 重新绑定。schema 7 的旧映射在迁移后使用无效的 issuer version 0，须经同样流程重建。

issuer discovery、JWKS HTTPS 获取与缓存、浏览器 redirect/callback、PKCE、logout、SSO session、管理员 API 和真实 IdP 集成仍未实现。CLI 的 `relay oidc` 只输出静态映射契约；配置和撤销目前是管理服务的 Go API，尚未接入用户可调用的 IPC 登录或凭据兑换流程。MCP/GUI 不自行实现 OIDC，也不能把 provider token 直接当作 grant 或 device authorization。没有完成 token 验证、本地映射和成员状态复验时，不能建立 Relay client 连接或访问设备。`Lookup` 只接受已验证的 subject key，本身不验证 token 或签发账号凭据；调用方必须使用当前 issuer 配置验证 token，不能缓存验证结果绕过策略轮换。

`internal/oidc` 的内存 fake store 单测覆盖配置/映射、issuer+subject 隔离、active/expiry 边界、成员 scope/version 不符、撤销后的旧写入及存储失败。真实本地 RSA token → 验证 → 本地映射 → 撤销测试证明 provider 的 tenant/principal/membership/role claims 不会成为本地授权；SQLite 集成另验证本地成员停用/版本变化。上述测试均不访问真实 IdP，不代表浏览器或完整登录链路已完成。

租户边界仍由本地 `Tenant`、`Principal`、`Membership`、`Credential`、设备归属和 Relay 每次调用的 access 验证决定。外部 subject 不是 tenant ID，设备显示 ID、grant hash、IP 和 Agent 名称也不构成 OIDC 身份。成员停用、凭据撤销、版本变化和设备转移继续使旧连接失效；OIDC key rotation 也不会自动改变本地 membership。
