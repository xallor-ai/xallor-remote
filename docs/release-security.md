# 发行来源验证（第一阶段最小切片，#60 未完成）

#74 当前交付为 [安装信任根设计](install-trust-design.md)、只读 `runtime verify --bundle <bundle-dir>`，以及 install/upgrade/rollback/MCP vendor selection 对同一 raw binary/manifest 快照执行固定来源验证。固定 repository/workflow/issuer/tag/commit 政策和独立信任的系统 gh 验证失败默认拒绝。真实 signed-vendor 正向链路与离线信任根轮换/撤销仍未完成；#74 未完成，不推进 #73。

发行 CI 在 Windows/Linux zip 与含两平台 Runtime 的 MCP tgz 构建后，先校验 zip checksum、内嵌 manifest、npm vendor 字节和 npm/Runtime 构建身份一致性，再用 `scripts/release_evidence.py` 生成：

- `release-index.json`：制品名称、大小、SHA-256、两平台构建身份。
- `runtime-windows-amd64.cdx.json`、`runtime-linux-amd64.cdx.json`：从实际二进制读取的 Go module 和工具链 CycloneDX 1.5 清单。
- `mcp.cdx.json`：npm 原生 `npm sbom --omit=dev --sbom-format=cyclonedx` 输出；npm 依赖与两个 vendor Runtime BOM 合起来描述 tgz。当前不覆盖 OS 库许可证、完整依赖关系或漏洞分析。

`release.yml` 用固定 SHA 的 `actions/attest` 为 zip、tgz、checksum、SBOM 和 index 生成 Sigstore 签名的 SLSA provenance，保存 `provenance.sigstore.json` 并上传 GitHub attestation 服务。签名由 GitHub OIDC 身份绑定，无仓库私钥。所需权限仅在发行构建 job 开放。PR 不申请签名权限，验证生成及篡改拒绝逻辑。发行 CI 在上传/draft 前用真实 `gh attestation verify` 校验全部文件，再验证篡改字节与错误 signer workflow 必须失败。审计 high/critical 会阻止发行。

候选工作流另把 Windows/Linux 原始 binary 与原始 manifest 纳入同一 provenance subject 集合，并在 tag release 中运行 native 只读验证；这些 raw-file 证明用于 `runtime verify` 和 install 的同一验证器。新增 release 的真实签发正向链路仍需 release CI 运行确认；本地已对真实 draft release 完成正向安装和 rehash 拒绝回归。

来源认证依赖 [GitHub attestation](https://github.com/actions/attest) 和 [GitHub CLI verifier](https://cli.github.com/manual/gh_attestation_verify) 的信任根；缺少服务支持、权限、CLI 或有效证明时失败，不降级成仅校验 checksum。私有仓库可能需要相应 GitHub 套餐，未在本地执行 OIDC 签发。

## 下载后的验证

将全部发行附件放入一个目录。使用来自可信源码/已审查版本的验证脚本，独立核对期望标签及完整 commit，再执行：

```text
python scripts/verify_release.py --directory <附件目录> --source-ref refs/tags/v0.2.2 --revision <完整40位commit>
```

验证器先认证 index，再逐项验证大小、SHA 和来源证明；固定检查 `xallor-ai/xallor-remote/.github/workflows/release.yml`、预期 ref/commit，并拒绝自托管 runner。自托管发行可显式给 `--repo owner/repo`，该身份不能来自不可信制品。任一错误非零退出，不解压、不执行、不安装。脚本是验证入口，**不把验证结果转成可持久安装凭证**。当前在线验证会读取 GitHub attestation 服务；保存的 bundle 可用于人工 `gh attestation verify <artifact> --bundle <bundle> ...` 离线核验，但自动离线信任根更新与吊销未实现。

仅在验证成功后解压 zip 或安装 tgz，安装前再次确认目录未被其他进程修改。对 Windows PowerShell，下一步必须由退出码保护：

```powershell
python scripts/verify_release.py --directory <附件目录> --source-ref refs/tags/v0.2.2 --revision <完整40位commit>
if ($LASTEXITCODE -ne 0) { throw '发行来源验证失败，停止安装' }
# 此后再解压已验证 zip 或 npm install <已验证tgz>。
```

## 尚未完成的安装安全边界

**#60/#74 仍不是完整端到端来源认证。** `runtime install --bundle`、升级和 rollback 在复制、Control.Check、停止旧 Runtime 或激活旧版本前强制验证同一 raw-file provenance 快照；攻击者替换二进制并重算 manifest 会被拒绝。MCP managed/vendor/PATH 选择也拒绝未认证候选；手工 `npm install` 的包获取与离线信任根轮换/撤销仍需独立完成，真实 signed-vendor 正向路径尚未由 release CI 确认。

后续 P1：把同一来源验证接入 rollback、MCP vendor 选择和 npm 安装/启动入口；补真实签发方错误、撤销、篡改后重算 hash、升级失败恢复、历史已签版本回滚测试。明确开发构建与正式发行的可信策略，不能静默跳过。新增安装器的真实 OIDC 正向验证须由 release CI 运行确认，不以本地 mock 代替。

发行失败时保留旧版本，不绕过验签重发同一标签，不自动发布 npm 或部署 Relay。修复源码并审查后创建新版本/标签，重新运行全部门禁。已签发恶意制品的吊销、信任根轮换及过期策略仍待实现，不能靠重写 checksum 恢复信任。
