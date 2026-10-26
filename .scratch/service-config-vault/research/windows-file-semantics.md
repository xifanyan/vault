# Windows 文件替换、锁与权限语义核查

> 历史参考：用户已将复杂跨平台文件保证移出交付前提。下文记录研究时的背景和事实，不作为现行产品要求；当前边界见[便携使用契约](../issues/05-file-operations.md#answer)。

## 范围、版本与结论

本报告依据 Microsoft Learn / Microsoft Open Specifications、Go **go1.27.2** 标签源码与 Go 官方支持资料，核查本地 Windows 文件替换、句柄共享、旁路锁、刷新、路径与权限语义。`os.Rename` 的 Windows 实现事实特指 Go 1.27.2；Microsoft Win32 文档为当前正式 API 文档（文档页面没有绑定到一个 Windows 小版本，具体适用下限见各页）。本机已知 Go 版本为 `go1.27.2 windows/amd64`，但本研究没有运行文件操作、竞争、崩溃或断电实验，不声称平台实测。

**最重要结论：现有第一方文档不能支撑“在本地 NTFS 上、即使替换失败也保持目标原样，且读者只可能看见完整旧/新文件”的通用原子提交保证。** Go `os.Rename` 的正式注释明确说：即使同一目录，非 Unix 平台也不保证原子操作。其 Windows 实现调用 `MoveFileExW(..., MOVEFILE_REPLACE_EXISTING)`，而 MoveFileExW 文档并没有给出 old-or-new 可见原子性合同；`ReplaceFileW` 文档列出会留下目标/临时文件位置或元数据已改变的特殊失败状态。Microsoft 还没有在所引 Win32 API 页面对本地 NTFS 上读者可见的原子性、操作失败后的完整状态或崩溃恢复给出足以覆盖该承诺的规范。因此，在“仅本地常规文件系统、受控目录”的已确认范围内，也不能仅从这些 API 名称/成功示例推导出产品级原子替换保证；若要承诺，需要收窄为特定实现与经验证环境的行为，并把未覆盖的系统版本/存储故障明确排除。即使正常运行时的命名空间可见性经实测符合预期，也不等于崩溃一致性或物理断电持久化。

本报告不替用户选 API。用户已确认的 Windows 边界包括：Go/OS 六平台组合中的 Windows/amd64、Windows/arm64；仅本地常规 FS、受控目录；同机写竞争立即返回 busy，不等待/重试；拒绝最终 vault 符号链接或多硬链接；父目录解析到真实路径且目录受信任；每主机 CLI 与业务程序使用同一指定账户，按目标机账户设置权限，但不主张隔离本机管理员；手动复制后按目标账户设置权限。

## 一手来源清单

- Microsoft [MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)、[ReplaceFileW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)、[SetFileInformationByHandle](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-setfileinformationbyhandle)、[FILE_RENAME_INFO](https://learn.microsoft.com/en-us/windows/win32/api/winbase/ns-winbase-file_rename_info)；[MS-FSCC FileRenameInformationEx](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/4217551b-d2c0-42cb-9dc1-69a716cf6d0c)（Windows 文件系统/SMB 协议规范，不等同所有本地文件系统驱动的合同）。
- Microsoft [CreateFileW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)、[LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)、[FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)、[File Buffering](https://learn.microsoft.com/en-us/windows/win32/fileio/file-buffering)。
- Microsoft [File Security and Access Rights](https://learn.microsoft.com/en-us/windows/win32/fileio/file-security-and-access-rights)、[SetNamedSecurityInfo](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setnamedsecurityinfoa)、[Reparse Points](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points)、[Reparse Points and File Operations](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points-and-file-operations)、[Hard Links and Junctions](https://learn.microsoft.com/en-us/windows/win32/fileio/hard-links-and-junctions)、[Naming Files, Paths, and Namespaces](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)、[GetFinalPathNameByHandleW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfinalpathnamebyhandlew)。
- Go [go1.27.2 `os.Rename` API 注释](https://github.com/golang/go/blob/go1.27.2/src/os/file.go#L431-L438)、[Windows `os.rename` 实现](https://github.com/golang/go/blob/go1.27.2/src/os/file_windows.go#L263-L268)、[Windows `openFileNolog`](https://github.com/golang/go/blob/go1.27.2/src/os/file_windows.go#L156-L165)、[`internal/syscall/windows.Rename`](https://github.com/golang/go/blob/go1.27.2/src/internal/syscall/windows/syscall_windows.go#L356-L370)、[`syscall.Open` Windows 共享参数](https://github.com/golang/go/blob/go1.27.2/src/syscall/syscall_windows.go#L364-L396)、[Go Windows `Chmod` 文档注释](https://github.com/golang/go/blob/go1.27.2/src/os/file.go#L628-L647)；Go 内部 Windows [`Renameat`/POSIX flags 实现与回退](https://github.com/golang/go/blob/go1.27.2/src/internal/syscall/windows/at_windows.go#L439-L510)。
- Go [发行历史/发布策略](https://go.dev/doc/devel/release)、[Go 官方最低系统要求：Windows](https://go.dev/wiki/MinimumRequirements)、[Go 1.26 Windows 端口说明](https://go.dev/doc/go1.26#ports)。Windows 最低系统要求由 Go Wiki 当前页面列出；1.26 发布说明明确移除 `windows/arm`，不能把源码环境表中遗留的 `windows/arm` 项当成 1.27 支持承诺。
- 锁实现参考仅核对 [gofrs/flock v0.13.0 Windows 源码](https://github.com/gofrs/flock/blob/v0.13.0/flock_windows.go) 与[通用源码](https://github.com/gofrs/flock/blob/v0.13.0/flock.go)，不从库名推导原子性或跨平台合同。

## 替换 API 与失败后的状态

### Go 1.27.2 `os.Rename`

Go 的通用公开注释说：目标若存在且不是目录则替换；不同目录可受 OS 限制；“即使在同一目录，非 Unix 平台上的 Rename 也不是原子操作”。这直接排除把 Go `os.Rename` 作为 Windows 可追溯“原子提交”保证的依据。Windows `os.rename` 经 `internal/syscall/windows.Rename` 转换 UTF-16 路径后调用 `MoveFileExW`，只传 `MOVEFILE_REPLACE_EXISTING`，没有 `MOVEFILE_WRITE_THROUGH`。这只是 Go 1.27.2 实现事实，不是 NTFS 所有版本的规范保证。[Go `os.Rename`](https://github.com/golang/go/blob/go1.27.2/src/os/file.go#L431-L438)；[Windows 路径实现](https://github.com/golang/go/blob/go1.27.2/src/os/file_windows.go#L263-L268)；[MoveFileEx 包装](https://github.com/golang/go/blob/go1.27.2/src/internal/syscall/windows/syscall_windows.go#L356-L370)

Microsoft 的 [MoveFileExW](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw) 文档说明：`MOVEFILE_REPLACE_EXISTING` 允许替换目标；跨卷要设置 `MOVEFILE_COPY_ALLOWED`，这种情况下模拟为 `CopyFile` 再 `DeleteFile`，源删除失败时函数仍可能成功并留下原源文件；`MOVEFILE_WRITE_THROUGH` 表示等到文件实际移到磁盘，但文档明确将“复制后删除”的刷新保证限定于该路径。Go `os.Rename` 不启用这些额外 flags。MoveFileExW 文档没有说明同卷同目录 rename 的原子可见性或一般失败时旧目标一定保留；“单次 API 调用成功”不等于文档承诺的原子事务。

### `ReplaceFileW` 的特殊失败不是“失败则原文件不变”

Microsoft 说明 `ReplaceFileW` 把“写新文件、把原件改名、让新文件使用原名、删除原件”等步骤组合在一个函数，并可保留原件的创建时间、短文件名、对象 ID、DACL、安全资源属性、加密/压缩及替换文件没有的命名流。它要求被替换文件、替换文件与可选备份在同一卷。重要的是，文档明列了特殊失败时的结果：

| 错误 | Microsoft 文档所述失败后状态 |
| --- | --- |
| `ERROR_UNABLE_TO_REMOVE_REPLACED` (1175) | 被替换文件和替换文件保持各自原文件名。 |
| `ERROR_UNABLE_TO_MOVE_REPLACEMENT` (1176) | 若指定了备份，两文件保持原名；若未指定备份，原被替换文件不再存在，替换文件仍在其原名下。 |
| `ERROR_UNABLE_TO_MOVE_REPLACEMENT_2` (1177) | 替换文件仍以原名存在，但已继承被替换文件的数据流/属性；被替换文件仍存在但改了名，指定备份时备份名就是旧文件名。 |
| 其他错误 | 两文件保留原文件名，但不保证没有备份文件，也不保证替换文件已继承所有原属性/数据流。 |

所以 `ReplaceFileW` 的错误结果不能统一解释为“提交没有发生、原文件完好、临时文件未变化”。仅按 0/非 0 返回值也不足以恢复用户期望状态；如果选择该 API，必须把这些 Win32 错误状态纳入恢复/错误合同，且不要把其多步功能描述直接改写成“文档保证原子”。`REPLACEFILE_WRITE_THROUGH` 当前文档明确称“不支持”。[ReplaceFileW 返回码与备注](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)

### 句柄重命名 / POSIX flags

Win32 `FILE_RENAME_INFO` 有一个 `Flags` 字段，Microsoft 说明仅当 `SetFileInformationByHandle` 的类为 `FileRenameInfoEx` 时使用；同一页列出传统 `FileRenameInfo` 的 `ReplaceIfExists` 行为。`SetFileInformationByHandle` 文档的合法信息类表没有列出 `FileRenameInfoEx`，这使通用 Win32 API 页对新类的支持/最低 Windows 版本与驱动支持信息不完整，不能据此承诺所有常规本地文件系统均支持。

Microsoft [MS-FSCC `FileRenameInformationEx`](https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-fscc/4217551b-d2c0-42cb-9dc1-69a716cf6d0c) 定义 `FILE_RENAME_REPLACE_IF_EXISTS` (`0x1`) 和 `FILE_RENAME_POSIX_SEMANTICS` (`0x2`)。其 POSIX 语义是：目标存在时以 POSIX 式语义删除旧目标，已经打开的旧目标句柄仍有效，后续以目标名打开会得到被改名进来的文件。该协议说明对替换使用“SHOULD”，适用范围是文件系统控制/SMB 协议规范，不能替代对特定本地 NTFS/Windows 版本的 Win32 契约或实测。

Go 1.27.2 内部 `Renameat` 是另一个路径：打开源文件句柄，再以 `NtSetInformationFile(FileRenameInformationEx)` 请求 `REPLACE_IF_EXISTS | POSIX_SEMANTICS`；失败后注释称文件系统可能不支持 POSIX 语义（例如 FAT）或未实现扩展类，随后回退 `FileRenameInformation` 传统替换。它并非 `os.Rename` 的 Windows 实现；不能拿该路径或其成功示例消除 `os.Rename` 明文免责声明，也不能把回退后仍成功当作 POSIX 语义兑现。[Go `Renameat`](https://github.com/golang/go/blob/go1.27.2/src/internal/syscall/windows/at_windows.go#L439-L510)

**区别总结：** 名称可见原子性（并发打开路径的读者看见完整旧或完整新）没有在所引 Win32 文档中得到足以覆盖应用承诺的本地 NTFS 通用保证；操作失败后状态方面 `ReplaceFileW` 已明确存在部分改名/元数据变化状态，MoveFileExW 的一般失败后目标状态在所引页未作同等细化；崩溃一致性（进程/系统崩溃时元数据恢复）没有被上述 API 的成功返回合同说明；物理断电后落盘顺序/设备易失缓存更没有得到保障。四者不能合并称作“原子替换”。

## 普通读句柄、同卷与名称可见性

Microsoft `CreateFileW` 的 `FILE_SHARE_DELETE` 是允许后来打开者请求 delete access 的共享标志；delete access 同时允许删除与重命名。既有句柄的共享选项在该句柄关闭前持续生效。若已有打开句柄没有允许 delete share，之后请求删除/rename 所需访问可能因共享冲突失败。[CreateFileW share mode](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)

Go 1.27.2 Windows `os.Open`/`os.OpenFile` 经 `syscall.Open`；后者传给 `CreateFileW` 的共享模式为 `FILE_SHARE_READ | FILE_SHARE_WRITE`，**不包含 `FILE_SHARE_DELETE`**。因此，普通 Go 只读句柄可阻碍 Go `os.Rename` / `ReplaceFileW` 替换目标（典型结果是 sharing violation），即使读者只读、没有写入也一样。此结论是 Go 源码共享标志与 Microsoft共享访问规则的直接推论，**本次未在 Windows 实测错误码**。用显式 `FILE_SHARE_DELETE` 打开的读者句柄则是另一种合同；POSIX rename flags 定义旧已开句柄继续访问被替换对象，但不能假定 Go 标准 `os.Open` 默认就是这种句柄。[Go 打开路径](https://github.com/golang/go/blob/go1.27.2/src/os/file_windows.go#L156-L165)；[Go `syscall.Open` 分享模式](https://github.com/golang/go/blob/go1.27.2/src/syscall/syscall_windows.go#L364-L396)；[Microsoft share-delete 规则](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)

`MoveFileExW` 的 `MOVEFILE_COPY_ALLOWED` 会把跨卷移动降为复制再删除；因此如要排除此类非替换路径，同目录临时文件是合理的**前置条件**，但并不能靠目录字符串本身证明最终源/目标位于同卷、路径解析不会经过 reparse point，或承诺原子性。MoveFileExW 的同卷限制/行为没有给本项目提供额外可见原子合同；`ReplaceFileW` 明确要求三文件同卷。Go API 自身也保留“同目录非 Unix 不原子”免责声明。

## 锁：LockFileEx 与稳定旁路锁文件

Microsoft `LockFileEx` 锁定文件内明确的字节范围，可请求共享/独占锁，也可用 `LOCKFILE_FAIL_IMMEDIATELY` 立即失败；否则不可用时等待。独占锁阻止其他进程对该范围读写，共享锁阻止该范围写；映射文件视图不受区域锁阻止。进程终止/关闭有锁句柄时，系统会释放锁，但微软提醒释放耗时取决于系统资源，建议进程显式解锁。锁不是对 vault 内容本身、未遵循锁协议的程序或映射 I/O 的全局互斥保证。[LockFileEx](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex)

Go 1.27.2 Windows 有 `LockFileEx` syscall bindings，但标准 `os.Rename` 并不自动使用它。作为具体第一方锁库参考，gofrs/flock v0.13.0 Windows 实现对打开的锁文件的第 0 字节取 1 字节锁；`TryLock` 使用 `LOCKFILE_FAIL_IMMEDIATELY`，普通 `Lock` 阻塞；`Unlock` 调 `UnlockFileEx` 后关闭句柄。其通用 `New` 默认以 `O_CREATE|O_RDONLY` 打开并用默认 `0o600` 权限；Windows `Unlock` 明确不删除锁文件。源码注释还称 shared flag 是依 Microsoft 文档推断。它展示的是一种实现参考，不代表项目已选库或其跨平台行为保证。[flock Windows](https://github.com/gofrs/flock/blob/v0.13.0/flock_windows.go)；[flock 通用路径](https://github.com/gofrs/flock/blob/v0.13.0/flock.go)

旁路锁文件的关键是**身份稳定**：锁应锁一个不会随 vault 文件替换而改变的另一个文件，覆盖完整读—改—写—替换过程。删除后再创建会形成新的文件对象；如果仍有进程握着旧对象锁，另一进程可能在新对象上取得互斥锁，从而分裂互斥域。Windows Go 默认 open handle 不分享删除，通常会让在句柄存活时删除/改名锁文件失败，但这只是默认共享模式带来的阻碍，不应把“删除一定被拒绝”当作协议或其他 API/FS 的普遍保证；删除-重建还会在无锁窗口改变锁对象。gofrs/flock 明确不自动删除锁文件。按用户“立即 busy”决定，应使用非阻塞获取并将冲突映射为 busy；不要依赖删除锁文件来清理“陈旧锁”，因为进程终止释放的是 OS 文件区域锁，留在目录中的 sidecar 是正常稳定对象，不是锁还被持有的证据。

## Windows 路径别名、重解析点和硬链接

- Windows 普通路径默认大小写不敏感；不同文件系统命名规则不完全相同。Microsoft 说明 NTFS 可支持 POSIX 大小写语义，但不是默认；8.3 短名可能随长名一起创建，也可按系统/卷禁用，不能假设一定存在。文档提供 `GetLongPathName`/`GetShortPathName` 映射 API。[Naming Files](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)
- `GetFinalPathNameByHandleW` 可按固定选项返回 normalized path，并选择 DOS 驱动器名、卷 GUID 或 NT 设备对象路径；卷 GUID 形式是 `\\?\Volume{GUID}\...`，NT 路径是 `\Device\HarddiskVolume...` 且文档说不能直接用于 Windows 程序。它说明符号链接示例中最终路径是目标路径，但 API 页**没有承诺**对所有短名/长名、硬链接名或任意文件系统路径返回唯一稳定的 basename。不同 `VOLUME_NAME_*` 选项会产生不同文本身份；使用“canonical 父目录 + 输入 basename + `.lock`”时，若别名未统一，路径字符串可以不同而指向同一目标。尤其短名别名可令 `name.lock` 与 `LONGNA~1.lock` 成为不同 sidecar；仅 canonicalize parent 不自动 canonicalize leaf。建议把这作为本票的待实现定义与验证项，而不是假设父目录规范化已经封闭全部别名。[GetFinalPathNameByHandleW](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfinalpathnamebyhandlew)；[8.3 alias](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)
- 本地目录身份若依赖路径文字，应统一同一种 volume naming（例如统一卷 GUID 或统一 DOS 形式），且不得让调用者混用 `C:\...`、卷 GUID 和设备路径产生多把 sidecar 锁。微软说明 `\\.\` 是设备 namespace 而非普通文件 namespace，`\\?\` 会关闭常规路径字符串解析，`GLOBALROOT` 可进入系统真实对象管理器根；各 API 对扩展路径支持并不一致。对本项目，允许的路径 namespace、8.3 输入、大小写和 volume alias 需在实现/验收时明示限制；本研究没有用实际卷测试规范化结果。[Naming Files, Paths, and Namespaces](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file)
- reparse point 数据由文件系统 filter 解释，可能实现符号链接、junction、挂载目录或其他行为；Microsoft 建议需要把 reparse point 本身当对象操作时用 `FILE_FLAG_OPEN_REPARSE_POINT`。常规 Go `os.Open` 不请求该标志（除 `O_CREATE|O_EXCL` 的创建场景会额外请求），所以不能把普通路径打开视作不跟随链接。用户已确认拒绝最终 vault 符号链接、受控目录父路径解析到真实目录；目录受信任避免了多数路径替换竞态，但其检查与最终替换是否对同一已打开目录对象锚定，Go `os.Rename` 的路径 API 本身没有提供该句柄相对约束。其他非 symlink reparse tag / 挂载点具体策略也未被“符号链接”一词自动覆盖。[Reparse Points](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points)；[Reparse Points and File Operations](https://learn.microsoft.com/en-us/windows/win32/fileio/reparse-points-and-file-operations)；[Go `os.OpenFile`](https://github.com/golang/go/blob/go1.27.2/src/os/file_windows.go#L156-L165)
- NTFS hard link 是同卷多个路径指向同一文件对象，任一链路内容/属性改变会反映到所有链接。`ReplaceFileW` 文档又明确结果文件 ID 与 replacement file 相同。故拒绝最终 vault 有多个 hard links 是合理的既定约束：若替换只更新给定路径，其他硬链接名不应被误认为也获得新文件内容。是否通过文件 ID/链接计数检查、检查时点如何避免竞态，需实现验证；仅比较路径文字不能发现同对象别名。[Hard Links](https://learn.microsoft.com/en-us/windows/win32/fileio/hard-links-and-junctions)；[ReplaceFileW 文件 ID](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)

## 刷新、崩溃与断电

Microsoft `FlushFileBuffers` 要求句柄有 `GENERIC_WRITE`；它将该文件的系统缓冲数据写往设备。`CreateFileW` 的 `FILE_FLAG_WRITE_THROUGH` 是绕过中间系统缓存、直接写盘的选项；`FILE_FLAG_NO_BUFFERING` 还要求 sector 对齐，带来额外实现约束，不是普通 Go 文件写入的默认语义。微软文件缓冲文档明确说其讨论不涵盖物理磁盘缓存，而硬件缓存不保证处于系统直接控制之下。[FlushFileBuffers](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers)；[CreateFileW flags](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew)；[File Buffering](https://learn.microsoft.com/en-us/windows/win32/fileio/file-buffering)

即使先刷新临时文件，再执行成功的命名替换，所引文档也没有定义“临时文件内容、目录项替换、相关元数据”在系统崩溃/突然断电下的持久顺序；`FlushFileBuffers` 的单文件合同不能自动等价于 parent directory rename 的 fsync，也不能承诺设备易失缓存已落到介质。Go 1.27.2 `os.Rename` 不设置 `MOVEFILE_WRITE_THROUGH`；而 `ReplaceFileW` 的同名 flag 标记为不支持。因此当前可确认的唯一结论是规格已正确排除“原子可见性即断电持久化”；具体写入刷新策略仍须用户决定并按目标存储验证，不得写成已保证物理断电可恢复。

## ACL、Go Chmod 与文件替换权限

Windows 文件权限由安全描述符中的 DACL/ACE 控制；新建对象的默认 security descriptor 通常继承父目录 ACL。Microsoft 的 File Security 文档明确区分 security descriptor/DACL 与文件属性。Go 1.27.2 `os.Chmod` API 注释说明 Windows 仅使用 `mode` 的 `0o200`（owner-writable）位设置/清除文件 read-only attribute，其余权限位不使用。因此 `Chmod(0600)` **不是**“仅当前账户可读写”的 DACL 设置，不能兑现账户隔离/私密 ACL 承诺。[Go `Chmod` 注释](https://github.com/golang/go/blob/go1.27.2/src/os/file.go#L628-L647)；[File Security and Access Rights](https://learn.microsoft.com/en-us/windows/win32/fileio/file-security-and-access-rights)

Microsoft `SetNamedSecurityInfo` 可设置对象 DACL（调用者需有 `WRITE_DAC` 或为对象 owner）；`CreateFileW` 在新建对象且 security descriptor 为 NULL 时使用默认 descriptor。按已确认的每台机器账户边界，若实际承诺仅指定账户访问，必须由实现设置/验证目标账户 SID 的 DACL，不能只设置 Go 文件 mode。产品明确不隔离本机管理员；DACL 约束不能误称为防本机管理员。

替换还会影响最终 DACL：`ReplaceFileW` 明确将原目标 DACL 等属性合并/保留到 replacement；即使同目录临时文件继承了目标账户的目录 ACL，替换后所依据的是旧目标 DACL 继承语义。若手动复制到新主机后须按目标账户设权限，应核验/设置**最终路径上的文件 DACL**，并把权限设置失败作为操作失败处理。MoveFileExW 文档只明确跨卷移动不会移动 security descriptor、目标赋予目标目录默认 descriptor；同卷 `MOVEFILE_REPLACE_EXISTING` 后究竟承载 source/target 的何种 DACL，文档没有在所引页明确到可承诺的程度。手动复制工具如何携带 DACL 也未在本任务限定，不能推断复制本身会按目标账户重映射。[ReplaceFileW 属性/DACL](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-replacefilew)；[MoveFileExW 跨卷安全描述符](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-movefileexw)；[SetNamedSecurityInfo](https://learn.microsoft.com/en-us/windows/win32/api/aclapi/nf-aclapi-setnamedsecurityinfoa)

## Go / Windows 支持底线

Go 官方最低系统要求页面列明 Go 1.21 及以后在 **Windows 10 及更高版本，或 Windows Server 2016 及更高版本**运行。Go 1.27.2 发布于 2026-10-08（官方发行历史）；Go 1.26 发布说明宣布 `windows/arm` 已移除。Go 官方环境支持矩阵列有 `windows/amd64`、`windows/arm64`（同时也曾列出历史架构）；因用户六组合仅要求 amd64/arm64，支持底线可据官方资料写成 **Go 1.27.2 + Windows 10+/Windows Server 2016+ + GOARCH amd64/arm64**。Windows/arm64 是 Go 正式端口（Go 1.26 发布说明亦描述其 linker 变化）。架构支持底线不代表系统/文件系统 API 提供原子 rename 或掉电持久性。[Go Minimum Requirements](https://go.dev/wiki/MinimumRequirements#windows)；[Go release history](https://go.dev/doc/devel/release)；[Go 1.26 Ports](https://go.dev/doc/go1.26#ports)；[Go install/source 环境列表](https://go.dev/doc/install/source#environment)

## 对本项目的约束、待决与需实测项

### 已确认事实转成的约束

1. 写入必须先准备完整密文临时文件再替换；临时文件与正式文件在同一受控目录是必要的同卷条件，但不能把它写成 Go/NTFS 原子性保证。不得直接 truncate 正式 vault。
2. `os.Rename` 现成 API 明确不承诺 Windows 原子操作。若票据需要“读者只看到完整旧或新”作为硬保证，需要实现 API 层的新研究/实测；ReplaceFileW 还需覆盖 1175/1176/1177 状态，错误不能简单宣称旧文件未动。
3. 使用稳定 sidecar 路径锁并覆盖完整读改写；锁竞争按用户决定立即 busy，不等待、不重试。锁文件不要随 vault 替换，不在普通成功/失败路径删除；进程崩溃后的 stale sidecar 文件应区别于 OS 仍持有 byte-range lock。
4. 检查普通 Go 读句柄共享删除冲突：运行中服务即便只读打开 vault，可能使替换报 sharing violation。错误契约应保留真实失败并且目标不被截断；但失败后完整状态仍须结合实际采用 API 和目标 FS 验证。
5. 拒绝最终 vault 符号链接/多硬链接；受信任、受 ACL 保护的目录并不能自动统一 8.3 basename、volume GUID/DOS/device path 字符串身份。实现须明示锁路径 canonicalization 规则，核验输入别名不会拿到另一把锁。
6. Go `Chmod` 不提供 DACL；按目标账户 SID 对目标目录、临时文件/最终文件、旁路锁文件设置并验证 DACL。替换/复制后的 DACL 语义需明确；目标机复制后权限设置须完成后再视为正式安装。管理员访问不在隔离承诺内。
7. 规格继续明确：名称可见性、操作失败后的状态、进程/系统崩溃状态、物理断电持久化是分开承诺的维度；FlushFileBuffers 不自动保证 rename 元数据或设备 cache 断电落盘。

### 仍需用户决定或实现阶段确定

- 在 Win32 与 Go 的明确限制下，是否把“完整旧/新文件可见”定为测试限定的经验承诺（指定 Windows build/NTFS/设备条件），还是调整产品接口/承诺以不声称 Windows 原子性；不能仅以选 ReplaceFileW/MoveFileExW 作决定。
- 锁 key 的规范路径：固定采用 DOS 还是卷 GUID 路径、是否接受 volume GUID/设备 namespace 输入、如何处理现存目标短名 alias、大小写变化；Canonical 父目录并不等于 basename 身份唯一。微软资料未证明 GetFinalPathNameByHandleW 在所有配置下会把 8.3 alias 归一成唯一长名，需选实现方式并验证。
- Windows 上普通 Go `os.Open` 没有 FILE_SHARE_DELETE；须决定是否接受读句柄令写入 busy/失败，或调用方需在提交时关闭句柄/改用另外一种 reader handle 合同。研究不替用户选。
- DACL 精确 ACE 内容/目标 SID、目录是否继承、临时文件及锁文件权限、复制后最终 ACL 设置失败时如何恢复，以及替换路径 API 对 ACL 的最终结果；账户授权不包括隔离管理员。
- 若要承诺崩溃/断电恢复，需确定具体 FlushFileBuffers/写穿透/硬件存储范围并在真实目标设备上做故障注入；Microsoft API 文档不提供该产品承诺的完整次序。

### 需要实测而本研究未做

Windows 10/11 与 Windows Server 2016+ 上 Go 1.27.2 windows/amd64、windows/arm64；实际 NTFS 卷（如其他常规文件系统则逐一命名）；并发打开读、正在读、共享删除/不共享删除句柄、权限/只读属性、文件系统过滤驱动或防病毒占用时的 rename 结果；ReplaceFileW 1175/1176/1177 故障注入后路径/内容/streams/ACL 状态；锁竞争立即失败、进程退出释放及 sidecar 被删除/重建的竞态；目录句柄规范路径、DOS/卷 GUID/NT device path 和短名 alias 对 lock key 的归一结果；系统崩溃/强制断电后的旧/新/缺失/损坏恢复与盘缓存边界。所有这些都是待观察项，不是本次运行过的验收。

## 研究状态

本研究只写入 `.scratch/service-config-vault/research/windows-file-semantics.md`。未修改票据、地图、规格或其他报告；未运行 build、lint、test、formatter、文件操作 smoke test 或断电实验；未创建/推送分支。报告只依据所列第一方公开文档与 Go 源码，不构成目标 Windows/NTFS 实测结论。
