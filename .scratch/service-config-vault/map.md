# 本地服务配置 Vault：实现就绪规格路线图

Labels: wayfinder:map
Status: resolved

## Destination

完成[本地服务配置 Vault：设计规格](spec.md)，核心验收是将 vault 与独立解锁凭据复制到另一受支持主机后可直接打开；保留层级配置、CLI 管理、Go 读写和 JSON 导出，不把复杂文件系统保证当作交付前提。
本路线图只解决设计决策，不编写产品代码，也不把未执行的验收场景视为已通过。

## Notes

- 已批准的需求与安全边界以[设计规格](spec.md)为准，领域术语以 [GLOSSARY.md](../../GLOSSARY.md) 为准；本轮不重新讨论已确认的产品范围。
- 用户最新要求优先简化为可迁移的 vault 与解锁凭据文件；沿用既定口令模式，不增加机器绑定或强制安装流程。与旧平台讨论冲突时，以最新便携使用结论为准。
- 每次开始工作先读取 Wayfinder；处理讨论型票据时读取 grilling 和 domain-modeling。修改 agent 消费的文档时遵循 writing-for-agents。
- 使用[本地 Markdown 追踪约定](../../docs/agents/issue-tracker.md)。子票据位于本目录的 `issues/`，按文件名前缀排序；从 `Status: open` 且所有 `Blocked by:` 前置票据均已 `resolved` 的票据中选择第一项。
- 先把选中票据改为 `Status: claimed`，再开始讨论。解决后在该票据追加 `## Answer`，改为 `Status: resolved`，在本地图追加具名链接与一句结论，并同步设计规格。细节记录在票据，地图只保留索引。
- `grilling` 票据需要用户参与，`research` 票据由后台 agent 核查第一方事实；它们不是实现任务，均不使用 triage 标签。算法安全性与平台行为由 agent 提供证据，不让用户猜测。
- 绘图会话不领取或解决票据；推进会话先领取再讨论，每次最多解决一个非研究票据。
- 研究沿用用户确认的本地 Markdown 模式，结果由票据链接；不创建或推送研究分支，也不为此初始化 Git。

## Decisions so far

- [配置键路径与 JSON 保真](issues/01-key-paths-json.md#answer)：确认对象根、JSON Pointer、精确数值及配置层写入边界。
- [敏感标记与脱敏规则](issues/02-secret-metadata.md#answer)：确认默认敏感、标记保留、结构遮挡保护和不透明脱敏视图。
- [口令输入与凭据生命周期](issues/03-passphrase-input.md#answer)：确认互斥输入来源、标准输入独占、跨平台口令文本及新建换口令的确认流程。
- [成熟加密容器与 Go 集成核查](issues/08-existing-encryption-format.md#answer)：核实 age v1 的认证与成本接口，并以本机烟测确认惰性明文交付边界。
- [Go 密码原语与认证前资源上限核查](issues/09-go-crypto-resource-limits.md#answer)：核实 KDF、AEAD 使用条件及认证前资源风险，区分源码事实与运行保证。
- [加密封装与文件格式契约](issues/04-encrypted-file-format.md#answer)：选定 age 口令固定档位及严格 JSON schema，明确大小、深度和完整认证边界。
- [Vault 与解锁凭据的便携使用契约](issues/05-file-operations.md#answer)：复制两份文件即可迁移，串行修改与复制，移除复杂协调和部署前提。
- [JSON 查看与导出契约](issues/06-json-export.md#answer)：只导出整库保存的层级，默认脱敏并标注路径，文件输出默认不覆盖。
- [CLI 与 Go 库接口契约](issues/07-cli-go-contract.md#answer)：固定可读写 SDK、内存编辑与显式保存、便携凭据初始化，以及命令、输出和错误约定。

## Not yet specified

当前无实现前必须新增的决策问题；创建管理、程序读写及复制迁移三条流程的收敛检查见[最终接口契约](issues/07-cli-go-contract.md#answer)。产品实现与实际验收不是本规划地图已完成的内容。

## Out of scope

沿用[设计规格的范围外约定](spec.md#范围外)。本路线图不加入中央或 HTTP 服务、配置历史与回滚、自动同步、主机级覆盖、环境权限隔离或自动热更新。

- 复杂跨平台替换、锁与持久化保证已退出简化目标；[Windows 文件替换、锁与权限语义核查](issues/10-windows-file-semantics.md#answer)和[Linux 与 macOS 文件替换、锁与刷新语义核查](issues/11-posix-file-semantics.md#answer)按范围关闭，报告仅保留作历史参考，不是实现前置。
