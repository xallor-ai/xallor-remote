# 发行来源验证（第一阶段最小切片，#60 未完成）

> **状态（2026-11-17）**：本页同时记录已实现的验证入口和未完成的发行验收。CI provenance/SBOM、`runtime verify`、Go/MCP 来源验证路径属于当前未发布 WIP；它们不等于正式 release、完整签名 install/upgrade/rollback、MCP vendor 正向启动或 trust-root rotation/revocation 已验收。

当前 WIP 新增 `internal/releasetrust/update_metadata.go` 的最小签名元数据
原语：Ed25519 threshold signatures、metadata version/expiry/revocation、
rollback rejection，以及目标文件长度/SHA-256 绑定。它尚未接入 install、bootstrap
或自动更新；root rotation、离线导入、TUF root/targets/snapshot/timestamp
发布流程仍未实现，不能把该原语称作完整 TUF 或正式更新系统。

该原语还要求 root rotation 同时满足旧 root role 与新 root role 的 threshold
签名，并拒绝把 revoked key 留在新 role；这只是轮换验证基础，不等于已部署
离线 root 仓库、2-of-3 私钥托管或撤销公告流程。

`VerifyBundle` 现在可只读验证完整的 timestamp → snapshot → targets 链，并
允许调用方传入已接受的各角色最低版本。它不持久化版本、不修改安装状态、不执行
目标文件；安装器接入前仍需原子持久化最低版本、过期/撤销来源、离线 root 导入和
回滚保护策略。

`MetadataPolicy` 提供该最低版本的原子本地状态写入闸门：损坏或未知字段的状态
拒绝加载，旧版本 bundle 拒绝，只有完整 bundle 验证成功且写入成功后才更新接受
版本，并保存当前已接受的 root metadata，后续调用方必须提供同一 root 或经过
交叉签名的下一版本。它仍未接入 install/bootstrap，也不负责私钥、root repository
或撤销公告的运营托管。

#74 当前交付为 [安装信任根设计](install-trust-design.md)、只读 `runtime verify --bundle <bundle-dir>`，以及 install/upgrade/rollback/MCP vendor selection 对同一 raw binary/manifest 快照执行固定来源验证。固定 repository/workflow/issuer/tag/commit 政策和独立信任的系统 gh 验证失败默认拒绝。真实 signed-vendor 正向链路与离线信任根轮换/撤销仍未完成；#74 未完成，不推进 #73。

发行 CI 在 Windows/Linux zip 与含两平台 Runtime 的 MCP tgz 构建后，先校验 zip checksum、内嵌 manifest、npm vendor 字节和 npm/Runtime 构建身份一致性，再用 `scripts/release_evidence.py` 生成：

- `release-index.json`：制品名称、大小、SHA-256、两平台构建身份。
- `runtime-windows-amd64.cdx.json`、`runtime-linux-amd64.cdx.json`：从实际二进制读取的 Go module 和工具链 CycloneDX 1.5 清单。
- `mcp.cdx.json`：npm 原生 `npm sbom --omit=dev --sbom-format=cyclonedx` 输出；npm 依赖与两个 vendor Runtime BOM 合起来描述 tgz。当前不覆盖 OS 库许可证、完整依赖关系或漏洞分析。

`release.yml` 用固定 SHA 的 `actions/attest` 为 zip、tgz、checksum、SBOM 和 index 生成 Sigstore 签名的 SLSA provenance，保存 `provenance.sigstore.json` 并上传 GitHub attestation 服务。签名由 GitHub OIDC 身份绑定，无仓库私钥。所需权限仅在发行构建 job 开放。PR 不申请签名权限，验证生成及篡改拒绝逻辑。发行 CI 在上传/draft 前用真实 `gh attestation verify` 校验全部文件，再验证篡改字节与错误 signer workflow 必须失败。审计 high/critical 会阻止发行。

候选工作流另把 Windows/Linux 原始 binary 与原始 manifest 纳入同一 provenance subject 集合，并在 tag release 中运行 native 只读验证；这些 raw-file 证明用于 `runtime verify` 和 install 的同一验证器。新增 release 的真实签发正向链路仍需 release CI 运行确认；当前已对真实 draft release 完成 provenance、离线 bundle、MCP vendor 和篡改拒绝验证，但旧 draft 的 native install/upgrade/rollback 不能被当前 owner protocol 接受，必须由新 clean candidate 重跑。

来源认证依赖 [GitHub attestation](https://github.com/actions/attest) 和 [GitHub CLI verifier](https://cli.github.com/manual/gh_attestation_verify) 的信任根；缺少服务支持、权限、CLI 或有效证明时失败，不降级成仅校验 checksum。私有仓库可能需要相应 GitHub 套餐，未在本地执行 OIDC 签发。

## 下载后的验证

将全部发行附件放入一个目录。使用来自可信源码/已审查版本的验证脚本，独立核对期望标签及完整 commit，再执行：

```text
python scripts/verify_release.py --directory <附件目录> --source-ref refs/tags/v0.2.2 --revision <完整40位commit>
```

验证器先认证 index，再逐项验证大小、SHA 和来源证明；固定检查 `xallor-ai/xallor-remote/.github/workflows/release.yml`、预期 ref/commit，并拒绝自托管 runner。它使用精确 certificate identity 与 signer digest；GitHub CLI 不允许同时传 `--signer-workflow` 和 `--cert-identity`，因此验证入口只保留前者策略所需的精确证书身份。自托管发行可显式给 `--repo owner/repo`，该身份不能来自不可信制品。任一错误非零退出，不解压、不执行、不安装。脚本是验证入口，**不把验证结果转成可持久安装凭证**。当前在线验证会读取 GitHub attestation 服务；保存的 bundle 可用于人工 `gh attestation verify <artifact> --bundle <bundle> ...` 离线核验，但自动离线信任根更新与吊销未实现。

仅在验证成功后解压 zip 或安装 tgz，安装前再次确认目录未被其他进程修改。对 Windows PowerShell，下一步必须由退出码保护：

```powershell
python scripts/verify_release.py --directory <附件目录> --source-ref refs/tags/v0.2.2 --revision <完整40位commit>
if ($LASTEXITCODE -ne 0) { throw '发行来源验证失败，停止安装' }
# 此后再解压已验证 zip 或 npm install <已验证tgz>。
```

## 尚未完成的安装安全边界

来源验证的临时快照按本次创建的目录和文件身份清理，只删除已知成员及空目录。
未知内容、对象被替换、文件被占用或关闭失败会保留清理不确定性，不返回成功的验证结果；
原验签或安装错误也会保留。清理可能发生在安装回调之后，因此该错误不证明安装从未执行，
不能据此重试或自动回滚。路径检查与删除并非对并发同用户替换的原子防护；此机制也不能
为旧事务重建遗失的资源所有权或证明其可安全结束。

**#60/#74 仍不是完整端到端来源认证。** `runtime install --bundle`、升级和 rollback 在复制、Control.Check、停止旧 Runtime 或激活旧版本前强制验证同一 raw-file provenance 快照；攻击者替换二进制并重算 manifest 会被拒绝。MCP managed/vendor/PATH 选择也拒绝未认证候选；手工 `npm install` 的包获取与离线信任根轮换/撤销仍需独立完成。当前 draft 的 MCP signed-vendor 正向路径已通过，但 native signed install/upgrade/rollback 必须在新 owner-compatible clean candidate 上由 release CI 重新确认。

后续 P1：已有显式 rollback 和 MCP 候选来源验证不等于安装/启动链路已全部验收；继续审计自动失败恢复及认证到执行的字节一致性，补真实签发方错误、撤销、篡改后重算 hash、升级失败恢复、历史已签版本回滚测试。MCP manifest 认证修复和真实制品检查的范围见 [install-trust-design.md](install-trust-design.md)。明确开发构建与正式发行的可信策略，不能静默跳过；本机单点检查不替代 release CI 与完整安装器端测。

发行失败时保留旧版本，不绕过验签重发同一标签，不自动发布 npm 或部署 Relay。修复源码并审查后创建新版本/标签，重新运行全部门禁。已签发恶意制品的吊销、信任根轮换及过期策略仍待实现，不能靠重写 checksum 恢复信任。
