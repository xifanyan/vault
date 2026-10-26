# Windows 文件替换、锁与权限语义核查

Type: research
Status: resolved
Labels: wayfinder:research
Mode: AFK
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)

## Question

针对本地 vault 的完整读改写和手动分发，Windows 与当前 Go 工具链实际提供哪些替换、锁、路径与权限保证，哪些失败状态不能被称作原子提交或断电持久化？

依据 Microsoft 正式文档、Go 1.27.2 源码与发行支持说明，核查 `os.Rename`、MoveFileExW、ReplaceFileW、适用的句柄重命名 API、文件共享删除语义、LockFileEx、刷新与 ACL。必须记录特殊失败状态、持有读句柄的影响、同卷条件、路径别名和重解析点风险。

调查稳定旁路锁文件及 Go 锁库的真实行为可以作为补充，但不因库名带 atomic 就推导其保证。只用第一方来源，区分规范保证、实现事实、推论和未执行的运行观察；不替用户选择产品策略。

## Answer

按用户最新简化指示关闭范围：只要求 vault 和解锁凭据能够复制到其他主机使用，不再将本票涉及的锁、ACL、强原子提交或持久化协议作为交付前置。

[第一方资料研究](../research/windows-file-semantics.md)已完成，保留为历史参考，不表示采用其所有约束或验证了 Windows 产品行为。原先准备的文件锁实验没有执行。本票列入地图范围外，不列入走过路线的决策索引。
