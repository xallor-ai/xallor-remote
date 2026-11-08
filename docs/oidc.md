# OIDC / SSO 边界

当前实现提供 provider-neutral 的本地验证与映射契约，代码位于 `internal/oidc`。它验证一个已经由管理面取得的 compact JWT 和已经独立配置的 JWKS key set：issuer 必须精确匹配 HTTPS URL，audience 必须命中配置值，只接受带 `kid` 的 `RS256`，并检查 `sub`、`exp`、`nbf`、`iat`。未知算法、未知 key、坏签名、坏 claim、过期或尚未生效的 token 一律 `ErrUnauthorized`。

`KeySet.Replace` 是原子 key rotation 边界；Runtime 不联网发现 issuer，不抓取 JWKS，不接受 token 中的 `tenant_id`、`principal_id`、role 或 membership 作为本地授权。验证成功只产生 `{issuer,subject,audience,expires_at}`。`MappingService` 在本机控制面保存独立的 issuer 配置和 `(issuer,subject)` 唯一映射；issuer、映射和 membership 都必须 active、未过期且版本精确匹配，版本只能递增。停用、撤销或轮换后，每次 lookup 都重新检查并 fail-closed。

issuer discovery、JWKS HTTPS 获取与缓存、浏览器 redirect/callback、PKCE、logout、SSO session、管理员 API 和真实 IdP 集成仍未实现。CLI 的 `relay oidc` 只输出静态映射契约；实际配置和撤销属于本机管理控制面。MCP/GUI 不自行实现 OIDC，也不能把 provider token 直接当作 grant 或 device authorization。没有完成本地映射和成员状态复验时，不能建立 Relay client 连接或访问设备。

租户边界仍由本地 `Tenant`、`Principal`、`Membership`、`Credential`、设备归属和 Relay 每次调用的 access 验证决定。外部 subject 不是 tenant ID，设备显示 ID、grant hash、IP 和 Agent 名称也不构成 OIDC 身份。成员停用、凭据撤销、版本变化和设备转移继续使旧连接失效；OIDC key rotation 也不会自动改变本地 membership。
