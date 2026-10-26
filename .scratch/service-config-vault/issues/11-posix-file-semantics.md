# Linux 与 macOS 文件替换、锁与刷新语义核查

Type: research
Status: resolved
Labels: wayfinder:research
Mode: AFK
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)

## Question

针对本地配置文件，Linux、macOS 和当前 Go 工具链对同文件系统替换、文件与目录刷新、进程锁及路径安全的保证分别是什么？

依据 POSIX、Linux 正式手册、Apple 正式文档、Go 1.27.2 源码及发行支持说明，核查 rename 的可见性与持久化差异、fsync 与 macOS 的完整刷新选项、flock 类锁的生命周期和建议锁边界、替换目标 inode 后锁身份变化、权限与符号/硬链接风险。

核对支持的 Go/操作系统与 amd64/arm64 架构边界；说明本地文件系统、网络共享及同步工具造成的假设差异。可用正式 Go 锁库源码作补充，但不得替用户定策略。报告来源能证明的事实，不声称在 Linux/macOS 已运行测试。

## Answer

按用户最新简化指示关闭范围：POSIX 锁、文件与目录刷新及崩溃保证不再是“复制 vault 与凭据即可使用”目标的交付前置。

[第一方资料研究](../research/posix-file-semantics.md)已完成，仅保留作参考，未在 Linux/macOS 运行或验收产品行为。本票列入地图范围外，不列入走过路线的决策索引。
