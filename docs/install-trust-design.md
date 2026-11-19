# #74 安装信任根方案（强制接入与剩余验收）

> **状态（2026-11-17）**：以下内容区分“当前 WIP 已实现的代码路径”和“正式发行验收”。Go 的 install/rollback/activation 与 MCP/bootstrap 已有固定来源验证和私有快照代码及本机回归；当前工作树未发布、未部署，真实签名双版本生命周期、完整 owner 接管、撤销/轮换、历史恢复和远端 CI 验收仍未完成。任何本地报告、受控 provenance 夹具或 draft release 都不能单独关闭 #74。

## 已核对的事实与本轮边界

`internal/release` 的 Stage/Install 现在先创建私有快照，对原始 manifest 和 binary 调用独立来源验证，再复制到安装目录、调用 Control.Check 或停止旧 Runtime；snapshot、rollback、ActivePath 和 MCP 的 managed/vendor/PATH 选择仍需要分别审计。首次运行一个恶意 CLI/npm launcher 时，它自己的检查不能保护用户，因此 bootstrap 的可信来源必须独立于待安装制品。

Go install/upgrade 与显式 rollback 已接入 `withSourceSnapshot`，分别验证原始 manifest 和 binary。MCP 的 `source-trust.ts` 也分别认证 manifest 与 binary，任一失败即拒绝；managed/vendor/env/PATH 复用该入口。2026-11-11 本地候选修复了此前仅认证 binary、只检查 manifest 存在的缺口；使用 v0.2.6 的真实 Windows/Linux vendor 文件验证原始文件对通过、manifest 仅追加空白也被拒绝，Windows 显式候选选择同样拒绝该改动。这只证明当前验证入口的签名正向与篡改拒绝，没有执行候选程序，也不代表该修复已发布。MCP 仍未采用私有执行快照，读取的 metadata、认证时文件与后来实际执行路径的一致性仍须解决。ActivePath、自动失败恢复亦须分别审计。**真实双版本升级回滚、完整 signed-vendor 启动、离线轮换/撤销未完成，#74 保持开放。** 历史候选证据见 [attested-candidates.md](attested-candidates.md)，不以 mock、重算 manifest 或 dirty=false 替代认证。

## 长期信任根

Go 的 `ActivePath`（本机 ensure 重新选择托管 Runtime 的入口）拒绝所有 pending 安装状态，并按 `current_activation` 对选定副本的原始 manifest/binary 执行来源认证，要求认证后的 manifest 与安装记录相等，再检查选定副本的内容摘要。缺 activation 记录时拒绝，不以版本缓存补足执行选择；来源验证期间副本发生可见变化也拒绝。它仍返回 activation 中的可变路径，调用方必须在执行边界绑定原预期摘要；各平台与所有头的启动保护尚未全部完成，不能据此宣称端到端替换/ABA 窗口已消除。

严格拒绝策略已确定：若运行中的旧 Runtime 或已停止的当前版本缺少原始 manifest/provenance、manifest 与状态不一致或来源验证失败，`runtime install` 在 `Check`、写入候选版本、停止旧进程之前拒绝。恢复路径同样重新认证旧版本后才允许启动；不通过时保持当前状态，不用本机 hash、Runtime 自报身份或重新生成 manifest 补信任。该策略要求历史未纳管版本先完成独立可信迁移，迁移设计未完成前不能升级它。

MCP 托管选择还要求 `state.current` 与磁盘 manifest 的解析内容完全一致，JSON 属性顺序和空白不参与该比较。平台、profile、protocol 与 hash 等准入检查不能使用与被认证 manifest 不同的安装记录。来源验证返回后再次检查 manifest/state 原始字节；验证期间出现待处理升级或持久文件变化时拒绝返回候选。这是选择一致性检查，不是执行快照：检查之间被换出后恢复（ABA）及返回路径后被替换仍需单独防护。

Go release 的 activation state 现在保存 `current_activation`、`previous_activation` 和 `pending_activation`。已验证的 binary 与原始 manifest 进入 `runtime/activations/<id>/`；Go release 安装、恢复、回滚和 ActivePath 解析并重新验证对应 activation，`runtime/versions/<sha>/` 不作为这些操作的执行选择。运行中或已停止的托管基线保留原 activation ID；选定副本损坏时，即便缓存完好也拒绝升级，缓存损坏不能阻止完好的选定副本恢复。未纳管的运行基线只能从原始 manifest/binary 的已认证隔离快照建立恢复副本，不在认证后重读来源路径。MCP 托管选择尚未迁移到统一 owner，不能把 Go release 的这项收敛当作所有头已完成。

这组 activation 基线回归先复现六个失败，再验证修复：停止状态下的损坏副本拒绝、缓存损坏时使用完好副本升级、候选失败时保留原副本恢复、恢复入口独立于缓存、缺 activation 记录拒绝、认证期间副本变化拒绝。它们采用真实临时文件和受控 Control/来源验证器，证明状态机与选择边界；不证明密码学、真实进程清理或最终二进制替换防护。现有真实签名生命周期报告早于这组修改，需在最终候选重跑。

新建 activation 的清理使用当次创建对象的内存记录，并在安装锁内检查 current/previous/pending 引用与记录完整性；仍被引用、退出不确定、对象已被替换或包含未知内容时保留。安装、首次引导和未纳管基线接管在确定性预检失败时回收尚未发布的副本；恢复成功后可回收已确认失败的当次候选。删除通过打开的目录逐项进行，不递归删除未知子树，也不扫描历史目录。Windows 与 Linux 回归已验证这些状态与文件边界。

清理仍未完成并发攻击验收：对象身份检查与 unlink/rmdir 不是同一个内核操作，绕过安装锁的写入者可能在两步之间替换路径。`os.Root` 限制遍历范围，不提供按已核对 inode 原子删除的承诺。当前保护范围是遵守安装锁的状态转换及清理前能观察到的对象替换；不能据此声称所有并发替换防护或历史副本回收已完成。

停止与失败恢复现在单独处理不确定性。Go `runtimeControl.Stop` 在同一 IPC 连接查询身份并发送停止，发出请求前取得 Windows 进程同步句柄或 Linux pidfd，收到确认后继续等待该进程真正退出；不以端点消失替代退出证据。端点已缺失时仍检查 Runtime 生命周期锁，锁被占用则拒绝确认停止。公开 CLI `stop` 也复用此路径，确认后才打印“已停止”。观察机制只等待，不通过 PID 发送信号；参见 [Windows WaitForSingleObject](https://learn.microsoft.com/en-us/windows/win32/api/synchapi/nf-synchapi-waitforsingleobject) 与 [Linux pidfd_open](https://man7.org/linux/man-pages/man2/pidfd_open.2.html)。其它平台未实现时拒绝观察，不降级为裸 PID 探测。

启动失败的清理由唯一的进程 Wait 结果确认退出，并核对映像句柄释放；最多请求一次终止，等待有期限，已观察到退出则不再发信号。`ErrCleanupUncertain` 会阻止自动启动旧版恢复并保留 pending。显式回滚在停止被拒绝时保留原记录，在确认原候选退出前不覆盖其 pending 身份；只有确认停止后，才记录恢复目标并执行。这里的证明对象是 Runtime 主进程，尚不能代替任务子树、崩溃后的遗留作业或完整签名生命周期验收。

Go 的 `Control.Check` 与 `Control.Start` 现在都接收调用方已知的预期摘要，通过 `internal/runtimeimage` 在执行前检查同一打开句柄中的普通文件类型、大小与 SHA-256。此前 Start 会先执行摘要错误的候选，再由就绪检查拒绝；回归子进程实际写出的标记文件复现了这个副作用，修复后在执行前拒绝。Windows 使用仅允许共享读取的文件句柄，并将该句柄显式继承给子进程，避免短驻启动器退出导致保护提前释放；版本探测后或启动失败后释放父进程句柄。真实 Windows 文件测试覆盖覆盖写入、文件/父目录/祖先目录重命名、删除及硬链接写入，子进程测试校验继承句柄的摘要并确认退出后可以重新写入。共享访问和继承的依据为 [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)；这些是本地文件和进程边界证据，尚未重跑真实签名版本的完整升级流程。

Linux 的执行边界现在先通过非阻塞描述符读取有大小上限的普通文件，复制到可执行 memfd，添加并检查禁止写入、增长、缩小及修改封印的内核 seals，随后才对封存字节核对预期摘要。启动时使用继承描述符的 `/proc/self/fd/<n>`，不再打开原磁盘候选路径；保留已有 ExtraFiles。系统不支持 memfd/seals、策略不允许执行或 `/proc` 不可用时拒绝，不退回未封存路径。Linux 6.3 以前不认识的可选 MFD_EXEC/F_SEAL_EXEC 标志仅在 EINVAL 时去掉，必需的内容封印不降级，权限错误不回退。语义依据为 [memfd_create](https://man7.org/linux/man-pages/man2/memfd_create.2.html) 与 [文件封印](https://man7.org/linux/man-pages/man2/F_ADD_SEALS.2const.html)。

Linux `buildinfo.Running` 保留内核报告的 memfd 映像名，摘要读取 `/proc/self/exe`；不把原磁盘候选路径伪装成实际加载路径。memfd 名称包含原 activation 绝对路径的摘要，供 `MatchesPath` 将运行观察与选定副本关联；名称只是诊断标签，仍需独立核对内容 SHA、构建身份、受控 PID 与来源证明。Release 的基线核对已使用该路径关联。

2026-11-12 的隔离 Linux 容器测试先复现没有内容封印、替换磁盘路径后启动错误，修改后验证封印位、读写描述符上的写入/扩缩容拒绝、可写共享映射拒绝、原路径替换不影响已封存映像、父进程关闭句柄后子进程仍可执行、原有 ExtraFiles 保留以及实际加载摘要。另在该容器运行 CLI 正常/超时/失败版本探测与错误摘要执行前拒绝。报告为 `runtimeimage-linux-red.json`、`runtimeimage-linux-green.json` 和 `cli-image-linux-green.json`，两份通过报告的清理错误均为空；这些是当前 dirty 工作树的编译测试证据，不是正式签名升级验收。

其他非 Windows/Linux 平台目前仍只有摘要检查，不能计为替换防护完成。Runtime owner 的全流程接管、MCP/bootstrap 的直接 spawn 替换、pending/清理不确定性和最终签名生命周期仍是未完成项。历史 signed lifecycle 报告与按磁盘路径观察进程的验收夹具不能直接用于 memfd 候选，须分别补齐实际加载映像和选定磁盘副本的检查后重跑；旧候选若无法报告加载映像摘要，应拒绝而不是降级执行。

Runtime 的 `runtime.ensure/status` 已从无条件 ready 改为读取实际安装选择、匹配运行身份并重新验证来源。验证前后安装记录必须稳定；pending、记录损坏、缺 activation、来源拒绝、运行摘要/构建/位置不符都不能报 ready，也不在查询时修复记录。activation ID 从选定记录取得，兼容真实的 Linux memfd 观察而非猜目录名。并发查询只合并正在进行的验证，不永久缓存证明；耗时验证不阻塞 IPC 读循环。CLI/MCP 均检查完整 ready 身份及协议/profile，不能只看 `ok:true`。这些单元与 IPC 测试验证的是状态与客户端准入，不是新签名候选的完整发布验收；启动器与 owner 的全部执行权尚未迁移完成。

CLI 的缺失 owner 分支已改用 Go `Store.EnsureRunning`：已有选择只用已认证 manifest 的摘要启动，不在认证后对可变路径重新计算“预期摘要”；无选择时先认证初始 bundle，才写 activation、探测或启动。Start 前持久化 pending，失败保留 pending，后续自动 ensure 不重放启动；并发出现的 owner 只能验证并复用，不能为完成引导而停止它。成功后 CLI 仍须通过同一 IPC ready 检查。MCP 对连接超时、权限错误及未知错误也不启动备用进程，只在明确端点缺失/拒绝连接时调用一次引导。

MCP/npm CLI 现在只向共享 `bootstrap-launcher` 传 argv，不接收可执行路径；发现阶段只枚举未信任的位置，不读取 managed state 或无上限解析候选 metadata。`bootstrap-snapshot` 把有上限的普通 binary 与原始 manifest 复制到唯一私有目录，在副本上认证并再次检查内容和文件身份；原路径在认证期间被替换不会改变副本。私有复制不能让原候选树里的 gh 变成可信工具，验证器搜索同时排除原目录与副本目录。只有最初查询候选本身得到缺失，才允许寻找下一个位置；已观察到候选存在之后，缺 manifest、文件消失或验证失败均直接拒绝。

引导进程只有在收到 `close` 后才正常释放副本；`exit`、kill 返回值均不能代替该证据，见 [Node 子进程契约](https://nodejs.org/api/child_process.html#event-close)。终止请求最多发送一次；已观察到 exit 的 PID 不再被发送信号；未确认关闭或清理失败报告 `runtime_cleanup_uncertain` 并保留副本。清理检查目录与各文件的对象身份、类型和链接数，拒绝未知内容，只 unlink 自己的文件再 rmdir，不递归删除可能被替换的目录。stderr 保留上限 64 KiB。

Node 首次引导现在通过最小原生绑定建立执行保护：Windows 打开禁止共享写入/删除的句柄，由当前 Node 的 libuv 转成其自身文件描述符；Linux 捕获并封存 memfd。保护建立后核对保留摘要，再通过子进程 fd 3 继承；Linux 从该描述符执行，Windows 的被锁路径不能被替换。父进程直到 close 才释放自身句柄，不确定时保留。实现使用固定版本 Koffi 调 OS API，不增加版本选择或激活状态机；库依据见 [Koffi](https://koffi.dev/start)、[libuv 句柄转换](https://docs.libuv.org/en/v1.x/misc.html#c.uv_open_osfhandle) 和 [Node 描述符继承](https://nodejs.org/api/child_process.html#optionsstdio)。Linux 的 argv0 只定位与 memfd 标签匹配的原始 metadata，Go 仍独立验真后才能激活。其他平台在 Node 引导处拒绝，不回退到普通路径执行。

原生替换测试先复现校验后修改导致执行不同摘要，修复后 Windows 阻止修改、Linux 仍执行封存字节。继承测试使用非执行文件，证明父句柄关闭后子句柄仍提供保护，而非误把可执行映像自身的限制当作继承证据。当前 Windows MCP 测试 67 通过、两个 POSIX 用例跳过；Linux Node 20.20.0 的 39 项相关测试通过，容器清理无错误。真实官方 Windows v0.2.6 的受保护 version 启动及四项拒绝验证通过；这不是新的签名 owner 启动/升级/回滚验收。私有目录清理仍有路径检查/使用间隔，activation 保留和完整生命周期仍待闭合，#74 保持开放。

2026-11-12 Windows MCP 测试为 60 通过、2 个 POSIX FIFO 用例跳过；独立禁网 Linux Node 20.20.0 容器运行 32 个相关用例全部通过，包含 FIFO、真实子进程从副本执行和清理拒绝，`bootstrap-node-linux.json` 的清理错误为空。另用真实系统 gh 验证官方 v0.2.6 Windows 原始制品，当前 wrapper 的私有副本 version 执行及四类篡改/配对拒绝通过，报告 `mcp-private-signed-windows.json`。真实签名报告只覆盖 version 路径，不覆盖首次 Runtime owner 启动或升级/回滚；单元测试的私有 verifier 注入也不属于密码学证据。

MCP 的 `artifact-manifest.ts` 将来源证明与候选配套关系分开检查：manifest 必须对应本机受支持的平台/架构、匹配 MCP 协议与 profile 格式及版本范围，文件名、大小和 SHA-256 必须描述当前 binary，两者须位于同一解析后的目录。manifest 读取上限 64 KiB，binary 上限 256 MiB，binary 分块计算摘要；POSIX 非阻塞打开后检查普通文件类型，避免 FIFO 在检查前卡住。验签前后再次观察 manifest 字节与 binary 摘要，拒绝可见变化。

Go 与 MCP 现在都要求 `gh attestation verify --format json` 的已验证声明包含事先保留的原始 manifest/binary SHA-256；仅有退出码 0 不够。匹配 `verificationResult.statement.subject[].digest.sha256`，不信未验证 envelope，也不假定 subject 文件名唯一。TS 的引导调用方传入保留摘要，Go 使用内存中的原始 manifest 字节与其所声明且复制时已核对的 binary 摘要，不能在验签后从可变路径重算“预期值”。验签输出有 2 MiB 上限，超限或结构不符拒绝。该绑定解决验签期间换出文件再恢复的摘要关联缺口；它与前述执行句柄保护分别验证，不能由其中一个替代另一个。

Go 的私有快照读取同样通过 `openSourceFile` 保留 `os.Root` 约束，在 POSIX 以非阻塞标志打开后检查文件描述符是否为普通文件；Windows 保持只读打开。否则 FIFO 可在 `Stat` 及来源验证的 context 超时生效前卡住。Linux 回归以受外部期限保护的子进程覆盖 manifest/binary 两种 FIFO，要求未调用验证器或消费回调，同时保留符号链接越界拒绝与正常快照测试。该修复只处理特殊文件打开阻塞，不宣称能为任意故障文件系统 I/O 提供可取消期限。

2026-11-12 本地 Windows signed-vendor 测试还将同一真实发行版本的 Linux manifest 与 Windows binary 交叉组合：先由真实 gh 单独确认该 Linux manifest 的签名有效，再确认候选选择拒绝这个不配套组合。不能仅以“两个 subject 都有合法签名”判断可以启动。Linux FIFO 回归在隔离容器中先复现阻塞超时、修复后通过；它与 signed-vendor 验收分别记录，不用文件类型测试冒充密码学验证。

复用 #60 的 Sigstore/GitHub provenance，避免另造 detached-signature 发行协议。信任根由独立取得并信任的 bootstrap 验证器、可信系统 GitHub CLI 及其 Sigstore 根组成；生产验证政策编译在验证器中：GitHub host `github.com`，仓库 `xallor-ai/xallor-remote`，工作流 `.github/workflows/release.yml`，OIDC issuer `https://token.actions.githubusercontent.com`，SLSA provenance v1，拒绝 self-hosted runner，固定 release tag 与完整 source commit。制品携带的公钥、manifest、checksum、以前的“验证成功”JSON 均不是信任根。

从不可信 manifest 读取 version/revision 仅用于提出待验证的 `refs/tags/v<version>`/commit 断言；必须由两份真正的来源证明（binary、原始 manifest bytes）分别确认，且两者绑定同一来源。必须拒绝 dirty、unknown revision、非版本 tag、错误 signer/workflow/ref/commit。manifest 哈希仍负责内容一致性，不代替证明。不能只签外层 zip 后要求安装器信任任意解压目录；发行 CI 同时 attests 原始二进制和 manifest，zip/tgz 仍有原有证明。

`gh` 必须来自操作者信任的系统安装。PATH/本机管理员、bootstrap 或解释器被攻击者控制属于信任根已失陷，不能由候选自检解决；验证器禁止从待验目录内运行 gh，不允许候选指定 host、根证书或 signer。自托管发行需另行审查并独立分发带其政策的可信 bootstrap，不能用候选内 policy 或运行时的随意环境变量覆盖正式政策。

## 四条路径的最终方案

| 路径 | 独立信任锚 | 强制检查应发生的位置 |
| --- | --- | --- |
| 首次安装 | 用户先独立信任 bootstrap/系统 gh，再对下载附件作外部验证；npm 本身先验证 tgz，不能靠包内脚本自证 | 提取到私有隔离区；binary + 原始 manifest 验真、绑定身份/平台/hash；完成之前不能向安装目录复制、执行 version 或停止旧服务 |
| 升级 | 已信任的正在运行安装器政策，不接受候选更新政策 | 在 Stage/Control.Check 之前认证隔离快照；校验后使用同一快照落盘，验证缓存命中也不能略过；认证当前可恢复版本后才能停止它 |
| 旧版本/故障回滚 | 当前可信安装器政策及仍有效的旧版本证明 | rollback 在激活旧版本前重新验证私有快照；验证失败保留当前进程和可恢复记录，不重试候选。真实双版本 signed rollback 仍待 release CI 确认 |
| MCP managed/vendor/env/PATH | 用户已独立信任的 npm launcher + 同一内嵌政策；绝不执行待选 Runtime 来验证自身 | 所有候选路径在返回可执行路径前完成同一来源认证；候选存在但无证明即拒绝，不悄悄回退到其它 unsigned PATH 或 XALLOR_REMOTE_BIN |

需将 Store 的“完整性 Verify”与“来源认证”命名区分，避免当前 Verify 被误当成签名校验。未来强制接入应通过单一认证快照接口，把已认证文件句柄/私有快照传到复制与执行步骤；不能把新 `runtime verify` 的输出当永久许可证再对原始可变目录安装。

## 本轮只读验证入口

```text
xallor-remote runtime verify --bundle <bundle-dir>
xallor-remote runtime verify --bundle <bundle-dir> --attestation-bundle <provenance.sigstore.json>
```

读取有上限的 manifest 和二进制到私有临时快照，检查平台/格式/大小/hash，调用真实系统 gh 对原始 manifest 和 binary 验证来源，返回 JSON 后删除快照。不会写本机数据目录、停止 Runtime、执行候选或安装。`runtime install` 和 rollback 复用同一快照验证后才会写安装目录、调用 Control.Check、停止旧 Runtime 或激活旧版本；MCP vendor 选择对每个候选执行固定策略。可选 bundle 是**待验证的证明载荷**，不是自定义信任根；格式、签名、证书链或政策不符仍拒绝。GitHub CLI 缺失、服务/根获取不可用、证明缺失/坏签名均非零退出，无 checksum-only 降级。

Windows/Linux 使用同一政策和参数向量，不经 Shell 拼接；平台差异只在二进制名称/平台校验。当前用户只应把入口用于验证带新增 raw-file provenance 的真实 release；本地 dirty 构建会被拒绝。

## 开发、失效与恢复

源代码开发可继续显式构建并运行可信源码；未签名开发构建不能获得正式来源认证，不引入生产 allow-unsigned 环境变量。现有 --allow-dirty 只控制本地构建/测试，不改变验证入口。MCP 自动选择分别验证 binary 与原始 manifest，但验证到执行的快照边界仍待补齐。

本轮不缓存可永久复用的验证结果；成功 JSON 仅是当次文件/政策/时间的观察。在线证明存在不等于具备产品级撤销机制。完整接入前还需由发行方定义可信更新的撤销/禁止摘要政策、政策版本防回退、根轮换审批、旧版本支持窗口与离线可信根更新流程。新政策须由现有可信政策/独立 bootstrap 分发链授权，不能来自待安装包。不能把 GitHub tag 可改写或删除当成充分撤销保障。

离线恢复需提前保存证明与经过独立验证的信任材料，并使用仍可信的验证器。没有可用信任根、撤销状态或受支持旧版本时拒绝激活，保留原服务与 profile；不能临时把制品里的 key 设为根，也不能为恢复自动执行未认证候选。系统 gh 读取 bundle 时仍可能更新其根/服务信息，本轮不承诺完全无网络离线验证。

## #74 完成前必须补齐的验收

1. 受保护的真实 release/tag 上签发并验证 raw binary/manifest、zip/tgz；同时验证 Windows/Linux 两平台。没有真实签发权限/发布授权时，不能本地伪造代替。
2. 同一发行政策下两个实际已签版本的首次安装、升级、旧版本回滚、失败恢复；区分已有 install/upgrade/显式 rollback 验证与尚需审计的 ActivePath/自动恢复，补齐 MCP 认证后实际执行的字节一致性及完整启动验收。
3. 真实签名正确、坏签名、未知 signer、错误 workflow/ref/commit、撤销、篡改后重算 manifest、缓存命中被换包、服务不可用、认证后路径交换；拒绝发生在停止旧服务、执行候选或安装复制之前。
4. Bootstrap/npm launcher 的独立分发与验证说明，可信根轮换/吊销及离线恢复政策，MCP/原生代码共享政策的构建一致性门禁。

纯参数/拒绝路径测试和已有 fakeControl 状态机测试不证明密码学正确；报告须把这些测试与真实 gh 密码学校验、真实产品签名正向链路分开。#74 未完成前 #73 保持待处理，不以新参数重构扩大本轮范围。
