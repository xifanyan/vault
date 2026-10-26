# POSIX 文件替换、锁与刷新语义核查

> 历史参考：用户最新目标是复制 vault 与解锁凭据即可使用。本报告的锁、刷新和文件系统讨论不作为交付前置；现行范围见[便携使用契约](../issues/05-file-operations.md#answer)。

## 范围、版本与结论边界

本报告查证 POSIX、Linux 正式手册、Apple/XNU 文档与源码，以及 Go **1.27.2** 的相关接口实现；只归纳来源能支持的语义，不是 Linux/macOS 实测报告，也不替用户选择锁库或定义超出已确认约束的产品策略。已确认的边界：支持 Linux、macOS、Windows 的 amd64/arm64 六个平台组合；仅支持受控目录中的本地常规文件系统；同一写锁争用必须立即返回 busy，不排队等待；拒绝最终路径是符号链接或硬链接数大于 1 的文件；父目录必须是真实路径且可信；每台机器上 CLI 与业务程序以同一指定账户运行，只授权该账户，不以文件权限隔离管理员；完整读改写过程互斥。手动复制不遵守进程锁协议，也不是跨主机锁。

本文所称“原子”限于同一文件系统内对一个目录项的 `rename` 可见性，不等同于掉电持久性、多个路径操作的事务、网络共享保证或手动复制不会出现部分写入。POSIX 事实来自 IEEE Std 1003.1-2024（Issue 8）；Linux 手册版本为 man-pages 6.19（页面说明于 2026-09-09 获取）；Apple 来源为 Apple 的 XNU 开源仓库 `main` 当前手册及源码，其中部分手册正文注明日期，仓库分支不是 macOS 发布版本的行为证明。Go 版本按 Go 官方发布历史于 2026-10-08 发布的 go1.27.2 标签；官方历史还列出 2026-10-19 发布的 go1.27.3，因此 go1.27.2 在本次核查日已不是最新补丁版。所有源码结论都只描述对应上游源码，不替代目标 OS/文件系统测试。[POSIX Issue 8](https://pubs.opengroup.org/onlinepubs/9799919799/)；[Linux man-pages 6.19 的 fsync 手册与版本说明](https://man7.org/linux/man-pages/man2/fsync.2.html)；[Go 发布历史](https://go.dev/doc/devel/release#go1.27.2)

## OS/架构与工具链支持范围

| 目标 | Go 官方支持事实 | 这不证明什么 |
|---|---|---|
| `linux/amd64`、`linux/arm64` | Go 1.27.2 的平台支持表包含这两个组合；Go Wiki 将它们列为 first-class port。Go 1.24 及之后的 Linux 最低内核版本为 3.2。[Go 1.27.2 平台表](https://github.com/golang/go/blob/go1.27.2/src/internal/platform/supported.go)；[Go First Class Ports](https://go.dev/wiki/PortingPolicy#first-class-ports)；[Go Minimum Requirements：Linux](https://go.dev/wiki/MinimumRequirements#linux) | Go 支持目标组合不保证每种 Linux 文件系统、挂载选项、设备缓存或容器/模拟器下的 rename、锁、fsync 行为相同。Go 的 first-class 状态也不是文件系统持久性认证。 |
| `darwin/amd64`、`darwin/arm64` | Go 1.27 要求 macOS 13 Ventura 或更新版本；Go 支持表及 first-class port 列表包含两种架构。[Go 1.27 Ports](https://go.dev/doc/go1.27#darwin)；[Go 1.27.2 平台表](https://github.com/golang/go/blob/go1.27.2/src/internal/platform/supported.go)；[Go First Class Ports](https://go.dev/wiki/PortingPolicy#first-class-ports) | 不意味着 Apple 对所有本地卷、驱动器或文件提供同一掉电保证，也不表示本任务已在任何 macOS 版本上构建或运行。 |

Go 官方源码构建说明的通用规则是：构建 Go 1.N 需要 1.M 引导编译器，其中 M 是 N−2 向下取偶数；因此 Go 1.27 源码构建要求 Go 1.24 系列引导工具链。该要求仅适用于从源码构建工具链；使用预编译 Go 发行版的应用开发者不需要另备引导编译器。Go 源码构建的 cgo 支持需 C 编译器；这也不意味着本项目必须启用 cgo。应用自身的 `go.mod` 最低语言/工具链声明未在本任务核查，不能仅从本机 Go 版本推断。[Go 安装源码：引导编译器规则及可选 C 工具链](https://go.dev/doc/install/source#install-go-compiler-binaries-for-bootstrap)；[Go 1.27.2 `internal/platform/supported.go`](https://github.com/golang/go/blob/go1.27.2/src/internal/platform/supported.go)

## 同目录 rename：可见性与 inode 身份

**规范保证。** POSIX `rename()` 在目标已存在时移除目标目录项并改名；在操作期间，目标名始终可见，并指向操作开始前的旧目标或源文件之一。这是对目录名绑定的原子性要求，不是对断电结果的陈述。POSIX 还规定 `rename()` 操作路径最后一项本身：源最后一项若为符号链接则重命名链接，目标最后一项若为符号链接则移除链接；不会因最终组件是符号链接而改写链接目标。源与目的跨文件系统可能以 `EXDEV` 失败。同目录创建完整临时文件后替换目标，避免跨文件系统这一类限制，但仍以源/目标实际属于同一文件系统为前提。[POSIX `rename()` 描述与 rationale](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html)

**Linux 与 Apple 文档。** Linux `rename(2)` 明言已有目标会被原子替换，不会出现目标缺失的时刻；打开的旧目标文件描述符不受影响，其他硬链接也不受影响。Apple XNU `rename(2)`（手册正文日期 2021-06-03）称系统崩溃发生在操作中时仍保证 `new` 有实例；它没有因此证明临时文件内容已落盘或新内容一定是崩溃后的版本。此 Apple 手册的崩溃措辞不能扩大为“旧或新内容必然持久”。[Linux `rename(2)`，man-pages 6.19](https://man7.org/linux/man-pages/man2/rename.2.html)；[Apple XNU `rename.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/rename.2)

**Go 1.27.2 实现。** Unix 下 `os.Rename` 最终调用 `syscall.Rename`；Go 源码在调用前会 `Lstat` 目标，目标为目录时另作检查。这个 Go 包装未提供通用“跨所有 OS、所有文件系统均原子”的承诺；这里的可见性事实来自 POSIX 与具体 OS 的 rename 接口。Go 的路径 API 是按字符串路径调用，不是相对已固定目录文件描述符的 renameat 操作。因此，已确认的“父目录真实路径且可信”是重要前提：若另一个不可信写者可以并发替换父路径组件，仅靠先 `Lstat` 再按路径 `Rename` 不构成对抗路径竞态的保证。[Go 1.27.2 Unix `os.rename`](https://github.com/golang/go/blob/go1.27.2/src/os/file_unix.go#L25-L57)；[Go 1.27.2 `os.Rename` API 文档](https://github.com/golang/go/blob/go1.27.2/src/os/file.go#L431-L438)；[POSIX `renameat()` 的目录 FD 用途与路径竞态 rationale](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html)

**读者观察。** 若读者对目标路径只打开一次，然后从该描述符读取，已打开旧文件者继续引用旧对象，新打开者在替换后看到新对象；它们各自读取完整的一代文件，不会因 `rename` 本身读到“被逐字节覆盖”的混合内容。若读者以路径分多次做 `stat`、`open` 等操作，操作间可能跨越替换边界；原子目录项替换不是多次路径访问或多文件读取事务。[Linux `rename(2)` 的 open-FD 说明](https://man7.org/linux/man-pages/man2/rename.2.html)；[Linux `open(2)` 的 open file description 与路径解绑说明](https://man7.org/linux/man-pages/man2/open.2.html)

**硬链接与替换后果。** 如果旧目标还有其他硬链接，rename 只替换目标这个名称；那些别名仍指向旧 inode/旧内容。旧 inode 也可能因其他硬链接或已打开句柄而继续存在。故拒绝 `st_nlink > 1` 与原子替换互补：否则用户看到目标路径已更新，不代表任何其他别名也更新，更不表示旧密文内容已从这些别名消失。[Linux `rename(2)`：其他硬链接不受影响](https://man7.org/linux/man-pages/man2/rename.2.html)；[POSIX `unlink()`：最后一个链接移除与打开引用的生命周期](https://pubs.opengroup.org/onlinepubs/9799919799/functions/unlink.html)

已确认的“拒绝最终符号链接”须在路径检查中区分 `Lstat` 与跟随链接的 `Stat`：POSIX rename 对最终组件本来就不会跟随，但这并不等于可忽略拒绝条件，也不保护路径中更前面的符号链接目录组件。检查与 rename 之间仍有时间窗口；可信且仅授权单一账户访问的父目录可以限制其他用户的竞态来源，但不能阻止同一账户内的非合作工具、管理员或已控制进程并发更改。Go 公共 `FileInfo` 没有可移植的硬链接数成员；Unix 的 `FileInfo.Sys()` 暴露平台专属 `syscall.Stat_t`，Go 1.27.2 Linux amd64/arm64 与 Darwin amd64/arm64 的 `Stat_t` 均有 `Nlink`，但字段宽度/类型按平台不同，因此通用跨平台接口必须自行封装平台化查询，不能把 `Nlink` 当成可移植字段。[Go 1.27.2 `os.FileInfo`、`Sys()` 与 `SameFile`](https://github.com/golang/go/blob/go1.27.2/src/os/types.go)；[Go Unix `FileInfo.Sys()` 实现](https://github.com/golang/go/blob/go1.27.2/src/os/types_unix.go)；[Linux amd64/arm64 `Stat_t.Nlink`](https://github.com/golang/go/blob/go1.27.2/src/syscall/ztypes_linux_amd64.go)；[Darwin amd64/arm64 `Stat_t.Nlink`](https://github.com/golang/go/blob/go1.27.2/src/syscall/ztypes_darwin_amd64.go)

## 同步与掉电持久性：数据与目录项是不同责任

对已确认的 sibling-temp 写入，标准的责任划分是：先完整写入同目录临时文件并同步临时文件，再 rename 替换目标，之后同步包含目标项的父目录。次序不能简化成“rename 成功即写入持久”，也不能用只同步临时文件替代父目录项同步。

| 操作 | 来源支持的责任范围 | 限制 |
|---|---|---|
| 临时文件 `fsync` / Go `File.Sync` | POSIX 要求请求把该打开文件的数据传送到相关存储设备，传送性质由实现定义；Linux `fsync(2)` 说明会冲刷文件数据和关联元数据。Go `File.Sync` 的文档说提交该文件当前内容到 stable storage。 | Linux 手册明确：文件 `fsync` 不一定把父目录中的目录项也写盘。POSIX 明确 `fsync` 的实现性质是实现定义，失败时未完成 I/O 不保证已完成。因此它不能单独持久化稍后发生的 rename。[POSIX `fsync()`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fsync.html)；[Linux `fsync(2)`](https://man7.org/linux/man-pages/man2/fsync.2.html)；[Go 1.27.2 `File.Sync`](https://github.com/golang/go/blob/go1.27.2/src/os/file_posix.go#L95-L105) |
| `rename(temp, target)` | 原子更新命名空间可见性（上一节）。 | rename API 本身没有把临时文件内容刷新到设备的含义；也不等同于目录项已持久写入。Linux 手册把目录项持久化明确交给目录 FD 上的 `fsync`。[Linux `fsync(2)`](https://man7.org/linux/man-pages/man2/fsync.2.html) |
| 父目录 `fsync` / Go `File.Sync` | POSIX Application Usage 明确：创建文件等修改目录的应用可对该目录调用 `fsync`，使目录项和文件属性同步；Linux 手册明确目录项需要目录 FD 的显式 `fsync`。[POSIX `fsync()` Application Usage](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fsync.html)；[Linux `fsync(2)`](https://man7.org/linux/man-pages/man2/fsync.2.html) | 规范称实现性质实现定义；具体文件系统是否接受目录同步及其掉电结果仍须目标环境核验。`rename` 成功后目录同步失败时，新目标已经可见，但崩溃后的目录状态未知；不能向调用方报告成“完全没写入”，也不能声称已耐久提交。 |

POSIX `fsync` rationale 谨慎说明：目的是把缓存变更推向物理设备并在崩溃后保留，但规范不定义 buffer cache、物理写、掉电或非易失介质，因而不同实现/配置对“安全到断电”的程度可能不同；它甚至允许不能保证非易失存储的实现采用空操作。Linux man-page 的说明比 POSIX 具体，称 `fsync` 会冲刷存在时的磁盘缓存并等待设备报告完成，但同时仍依赖设备和文件系统按接口正确兑现。此差别不是矛盾：POSIX 是抽象合同，Linux 手册是 Linux 接口说明。[POSIX `fsync()` rationale](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fsync.html)；[Linux `fsync(2)`](https://man7.org/linux/man-pages/man2/fsync.2.html)

### macOS 的 `F_FULLFSYNC` 与设备边界

Apple/XNU 的 `fsync(2)` 手册指出，普通 `fsync` 把数据从 host 冲刷到 drive，不保证设备缓存已写到介质，设备还可能重排写入；为更严格的完整性要求提供 `F_FULLFSYNC`，请求驱动器将缓存数据冲刷到永久存储。Apple/XNU 当前 `fcntl(2)` 手册进一步说明：`F_FULLFSYNC` 在 HFS、MS-DOS/FAT、UDF、APFS 上实现，可能耗时较久；已知某些 FireWire 驱动器会忽略刷新要求。因此“系统调用成功”也不能超越设备诚实报告和硬件能力，承诺物理层绝无丢失。[Apple XNU `fsync.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/fsync.2)；[Apple XNU `fcntl.2`：`F_FULLFSYNC`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/fcntl.2)

Go 1.27.2 macOS `os.File.Sync` 的底层实现不是简单 `fsync(2)`：`fd_fsync_darwin.go` 调用 `fcntl(F_FULLFSYNC)`；若返回 `ENOTSUP`，明确退回普通 `fsync`，源码评论举 SMB 挂载为例。Linux 及其他所列 POSIX 构建使用 `syscall.Fsync`。所以 Go 1.27.2 在 macOS 支持 `F_FULLFSYNC` 的卷上会请求较强刷新，但由于对 `ENOTSUP` 降级，`File.Sync` API 不能被上层误述为“总是完成 F_FULLFSYNC”。[Go 1.27.2 Darwin `fd_fsync_darwin.go`](https://github.com/golang/go/blob/go1.27.2/src/internal/poll/fd_fsync_darwin.go)；[Go 1.27.2 POSIX `fd_fsync_posix.go`](https://github.com/golang/go/blob/go1.27.2/src/internal/poll/fd_fsync_posix.go)

父目录同步在 Go 层也通过 `File.Sync` 调用同一平台实现；不过 Apple 手册没有明确陈述对**目录 FD**执行 `F_FULLFSYNC` 是否在所有受支持本地卷上成功，也没有给出其对 rename 目录项崩溃持久性的跨文件系统合同。应把“macOS 目录同步能否成功、何种错误可接受、降级后承诺如何表述、rename 后父目录同步失败如何向调用方表达”保留为待核实实现约束；不能仅凭 Go 代码存在该调用就宣布兑现了跨平台断电耐久保证。POSIX/Linux 文档支持数据文件与目录项分开同步这一责任模型，但不替目标 APFS/HFS 卷及设备作实测背书。

## `flock` / `fcntl`：锁绑定对象而非名字

`rename` 改变目录项的名称绑定；已有文件描述符仍引用其原打开对象。Linux `flock` 手册明确锁关联到 open file description；POSIX `fcntl` 锁操作描述的是以打开文件描述符所指文件为对象的建议记录锁；Apple 的 `flock(2)` 手册称锁在文件上而不是文件描述符上。由此可知，若锁住 vault 文件本身再将一个新 inode rename 覆盖目标，旧持有者/等待者仍锁旧对象，而后来按目标名打开者得到新对象；两个 cohort 不再争同一锁。目标文件锁不能自然变成跨 inode 替换稳定的互斥。[Linux `flock(2)`](https://man7.org/linux/man-pages/man2/flock.2.html)；[POSIX `fcntl()` 锁描述与生命周期](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fcntl.html)；[Apple XNU `flock.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/flock.2)；[Linux `open(2)`：描述符对路径后续变化不受影响](https://man7.org/linux/man-pages/man2/open.2.html)

| 锁类别 | 生命周期/范围事实 | 对契约的影响 |
|---|---|---|
| `flock`（BSD 接口，非 POSIX） | Linux 锁附着于 open file description；`dup`/`fork` 产生的描述符共享锁，最后一个引用关闭时释放；Linux 用独立 syscall 实现时与 `fcntl` 记录锁不互通。Apple 的 BSD 手册称锁在文件上，dup/fork 是同一锁的多个引用，fork 子进程显式解锁会连带解除父进程的锁；Apple `close(2)` 说明进程退出会释放描述符，最后关闭持有 advisory lock 的文件时释放锁。[Linux flock](https://man7.org/linux/man-pages/man2/flock.2.html)；[Apple flock](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/flock.2)；[Apple close](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/close.2) | `LOCK_NB`/非阻塞尝试与“争用立即 busy”相符；阻塞 `Lock` 不符合。锁是 advisory，只约束遵守同一协议的写者，不阻止任意进程直接写、rename 或删除。 |
| `fcntl` 传统 POSIX 记录锁 | POSIX 定义为 process-owned；该进程关闭**任意**指向同一文件的 FD 时，释放该进程对此文件的全部记录锁；进程退出也释放。Linux 与 Apple 手册均记载这一“任意 FD close”副作用。[POSIX `fcntl`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fcntl.html)；[Linux fcntl locking](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html)；[Apple XNU fcntl](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/fcntl.2) | 若实现采用此类锁，库内或同进程另一个组件不经意关闭同一文件的 FD 都可能释放锁；它不是“只要持锁 FD 还开着就一定互斥”的简单句式。不可把 Linux 特有 OFD-lock 支持推及 macOS。 |
| POSIX OFD 记录锁 | POSIX Issue 8 定义 OFD-owned lock，关联一个 open file description；所有引用它的 FD 都关闭时释放。Linux 手册称其在 Linux 3.15 起提供；它与传统 process lock 的关闭语义不同。[POSIX `fcntl`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fcntl.html)；[Linux fcntl locking](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html) | 规范纳入 OFD 概念不代表指定 Go syscall 包/API 为两个平台都提供相同能力。应按实际 OS API/绑定实现核验，不能把 Linux-only 结论当跨平台接口。 |

**为什么旁路锁文件不能随意删除。** 稳定的 sibling lockfile 让各写者打开同一路径对应的锁对象，锁生命周期与 vault 被替换的 inode 分离。Linux/ POSIX 规定 unlink 删除的是目录项；若旧文件仍有打开引用，它继续存在到最后引用关闭。因而一个持锁进程仍握有旧 lock inode 时，另一进程删除该路径并创建新文件，可以让后续写者锁新 inode；旧进程/等待者仍锁旧 inode，两组本应互斥的写者并行。这是由 unlink 与锁的对象身份推导出的竞态风险，不是锁 API 帮用户自动解决的情形。[POSIX `unlink()`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/unlink.html)；[Linux `unlink(2)`](https://man7.org/linux/man-pages/man2/unlink.2.html)；[Linux `flock(2)`](https://man7.org/linux/man-pages/man2/flock.2.html)

因此，锁文件仍存在不表示锁仍被占用：进程退出后文件描述符释放，OS 锁释放，但目录项可以留存。相反，锁文件被删掉/重建也不证明旧 inode 上没有活跃持锁者。安全模型应把 lockfile 当稳定的命名锚点，不在成功操作后随手删除。`gofrs/flock` v0.13.0 仅作源码示例、未被选定：其 Unix 代码用 `golang.org/x/sys/unix.Flock`，`TryLock` 传 `LOCK_NB`，默认新建权限 0600；`Unlock` 解锁并关闭 FD，但文档明确不移除磁盘上的锁文件，由应用自行负责；该版本 `go.mod` 要求 Go 1.24.0。[gofrs/flock v0.13.0 Unix 源码](https://github.com/gofrs/flock/blob/v0.13.0/flock_unix.go)；[通用实现/生命周期文档](https://github.com/gofrs/flock/blob/v0.13.0/flock.go)；[v0.13.0 `go.mod`](https://github.com/gofrs/flock/blob/v0.13.0/go.mod)

**互斥边界。** 锁调用仅约束主动使用同一锁协议的 CLI/Go 库写者；POSIX/Apple/Linux 均将这些文件锁称作 advisory。锁必须跨越完整读—修改—替换事务，否则两个写者仍可能各自从旧快照计算。手动文件复制、其他编辑器、直接路径写入不会自动争用 lockfile，因此即使同机也必须被视为非合作写者：若与本地 API 写操作并发，锁不能防止丢失更新，也不能保证手动复制期间读者只见完整文件。跨主机更不构成分布式锁。[Apple XNU `flock.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/flock.2)；[Linux `fcntl_locking(2)`：advisory lock 仅对合作进程有用](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html)

## 权限、临时文件与目录安全

Go 1.27.2 `os.CreateTemp` 用 `O_CREATE|O_EXCL` 创建临时文件，模式参数为 `0600`，Go 文档明确称这是 umask 之前的 mode。POSIX 和 Linux 规定 umask 清除新对象请求权限位；Linux 另有默认 ACL 时，创建权限可能按继承 ACL 处理而非简单 `mode & ~umask`。所以给创建函数传 0600 是禁止 group/other 权限的起点，不保证抵消所有平台 ACL、既有对象或父目录配置；应从实际文件状态确认安全前提，而不能仅检查程序请求的 mode。[Go 1.27.2 `CreateTemp` API/实现](https://github.com/golang/go/blob/go1.27.2/src/os/tempfile.go#L26-L56)；[POSIX `umask()`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/umask.html)；[Linux `umask(2)`：默认 ACL 例外](https://man7.org/linux/man-pages/man2/umask.2.html)

`rename` 替换的是目录项所指对象：新目标继承临时文件自身的 owner、mode、ACL 与其他 inode 属性，而不会自动保留原目标 inode 的属性。Apple 手册明确说把文件 rename/move 进有可继承 ACL 的目录，不会因此给被移动对象设置该目录的 ACL；临时文件在创建时的继承属性与替换目标语义不能混为一谈。[POSIX `rename()`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html)；[Apple XNU `rename.2` 的 ACL caveat](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/rename.2)

POSIX `rename` 要求对源、目标所在目录拥有写权限，并需要路径组件的搜索权限；Linux `open(2)` 说明新文件 owner 是有效 UID，mode 是创建参数受 umask/ACL 影响后的结果。单一指定账户的已确认授权范围与 owner-only 文件/目录权限目标一致（通常用文件 0600、私有目录 0700 表达），但这里的 Unix 权限位不会把管理员/root 排除在外，也不能防止该账户自己的其他进程读写。父目录权限控制查找、创建、删除和 rename 目录项；文件 mode/ACL 决定文件本身访问，二者都需在契约中检查。还需处理已有目标、已有 sidecar 锁文件的权限：创建时的 0600 不会自动修正一个早已存在的宽权限文件。Linux 默认 ACL 对 umask 的规则参见上文。[POSIX `rename()` 权限条件](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html)；[Linux `open(2)` owner/mode 规则](https://man7.org/linux/man-pages/man2/open.2.html)；[gofrs/flock v0.13.0 `New`/`SetPermissions`](https://github.com/gofrs/flock/blob/v0.13.0/flock.go)

最终符号链接/硬链接拒绝、真实可信父目录与目标账户约束已由用户确认。残余边界是同一账户内手动复制、其他非合作程序对文件或 lockfile 的更改仍绕开 advisory lock；权限位也不保护管理员。对最终路径的链接数检查需要 Unix 具体 `Stat_t.Nlink`，且检查随后 rename 的竞态只能通过已确认的受控父目录前提缩小，无法对同一账户并发操作者作绝对保证。

## 不可外推的情形与待核实项

- **只为本地常规文件系统建立契约。** POSIX 接口文档、Linux syscall 手册和 Go 源码不能证明 NFS、SMB/CIFS、FUSE、云同步目录或虚拟化共享文件夹遵循相同的原子替换、锁互操作或耐久性语义。Linux `flock(2)` 明列 NFS/SMB 的实现/版本差异；Go Darwin `File.Sync` 源码也特别指出 SMB 上 `F_FULLFSYNC` 可能返回 `ENOTSUP` 并回退普通 fsync。[Linux `flock(2)` NFS/CIFS notes](https://man7.org/linux/man-pages/man2/flock.2.html)；[Go Darwin `File.Sync`](https://github.com/golang/go/blob/go1.27.2/src/internal/poll/fd_fsync_darwin.go)
- **崩溃持久性尚不是跨平台既成事实。** POSIX 说明 `fsync` 性质依实现；Linux 文档支持文件 fsync、rename、父目录 fsync 的分工；macOS Go 实现优先 `F_FULLFSYNC` 但有 `ENOTSUP` 降级。具体要求的 Linux 本地文件系统/内核、macOS 本地卷格式、外接设备和目录 FD 刷新成功行为仍须按六个目标系统验证。不能仅以构建成功、系统调用成功样例或上游源码分支声明实现端到端抗断电保证。
- **原子读者验收的范围要钉牢。** POSIX rename 只给单目录项切换；手动复制为非合作写者，不由进程锁覆盖。产品要承诺复制替换与本地写不能并发，或将其明示为操作边界，不能宣称 advisory lock 会保护复制过程。
- **错误结果有“已可见但未确认持久”的窗口。** `rename` 成功而父目录 Sync 失败时，目标路径已切换；持久状态未知。实现/调用契约需区分 rename 前失败、rename 后同步失败，不可回报为原文件绝对未改，也不可交付强持久成功。
- **源版本边界。** Go 源码审核固定 go1.27.2；当前官方已有 go1.27.3。Apple XNU 手册仓库链接指 `main`，本文注明手册内部日期，不视为任一特定 macOS build 的锁/刷新保证。操作系统和文件系统行为尚未在 Linux/macOS 运行，报告中没有平台测试结论。

## 一手来源索引

- [IEEE/The Open Group POSIX Issue 8 (IEEE Std 1003.1-2024)：`rename`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/rename.html)、[`fsync`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fsync.html)、[`fcntl`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/fcntl.html)、[`open`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/open.html)、[`umask`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/umask.html)、[`unlink`](https://pubs.opengroup.org/onlinepubs/9799919799/functions/unlink.html)。
- [Linux man-pages 6.19：`rename(2)`](https://man7.org/linux/man-pages/man2/rename.2.html)、[`fsync(2)`](https://man7.org/linux/man-pages/man2/fsync.2.html)、[`flock(2)`](https://man7.org/linux/man-pages/man2/flock.2.html)、[`fcntl_locking(2)`](https://man7.org/linux/man-pages/man2/fcntl_locking.2.html)、[`open(2)`](https://man7.org/linux/man-pages/man2/open.2.html)、[`umask(2)`](https://man7.org/linux/man-pages/man2/umask.2.html)、[`link(2)`](https://man7.org/linux/man-pages/man2/link.2.html)、[`unlink(2)`](https://man7.org/linux/man-pages/man2/unlink.2.html)。
- [Apple 开源 XNU 手册：`rename.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/rename.2)（正文 2021-06-03）、[`fsync.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/fsync.2)（正文 1993-06-04）、[`fcntl.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/fcntl.2)（正文 2021-08-12）、[`flock.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/flock.2)、[`close.2`](https://github.com/apple-oss-distributions/xnu/blob/main/bsd/man/man2/close.2)。
- [Go 1.27.2 `os`/`internal-poll`/`syscall` 标签源码](https://github.com/golang/go/tree/go1.27.2/src)：[`os/file_unix.go`](https://github.com/golang/go/blob/go1.27.2/src/os/file_unix.go)、[`os/file_posix.go`](https://github.com/golang/go/blob/go1.27.2/src/os/file_posix.go)、[`os/tempfile.go`](https://github.com/golang/go/blob/go1.27.2/src/os/tempfile.go)、[`internal/poll/fd_fsync_darwin.go`](https://github.com/golang/go/blob/go1.27.2/src/internal/poll/fd_fsync_darwin.go)、[`internal/poll/fd_fsync_posix.go`](https://github.com/golang/go/blob/go1.27.2/src/internal/poll/fd_fsync_posix.go)。
- [Go 官方 Go 1.27 发布说明](https://go.dev/doc/go1.27)、[Minimum Requirements](https://go.dev/wiki/MinimumRequirements)、[Porting Policy](https://go.dev/wiki/PortingPolicy)、[源码安装与工具链引导要求](https://go.dev/doc/install/source)、[Go release history](https://go.dev/doc/devel/release)。
- [gofrs/flock v0.13.0 官方仓库 Unix 实现](https://github.com/gofrs/flock/blob/v0.13.0/flock_unix.go)、[通用 API/生命周期源码](https://github.com/gofrs/flock/blob/v0.13.0/flock.go)、[`go.mod`](https://github.com/gofrs/flock/blob/v0.13.0/go.mod)；仅为机制样例，不是库选择或跨平台锁保证。

## 本地状态

本报告仅写入 `.scratch/service-config-vault/research/posix-file-semantics.md`。没有在 Linux/macOS 构建、测试或运行平台文件操作；没有编辑票据、规格或其他研究文件。
