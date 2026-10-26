# Go 密码原语与认证前资源上限核查

## 范围、版本与结论边界

本文件依据 Argon2 与 scrypt 规范、Go 标准库文档/源码，以及 `golang.org/x/crypto` 第一方文档/源码，记录可核对的 API 与安全条件；不选算法、不定本项目资源预算，也不把规范示例当成实测。本次核查时所见版本为 Go 源码标签 **go1.26.5**、`golang.org/x/crypto` **v0.58.0**（该模块 `go.mod` 要求 Go 1.26.0）。这些是引用的上游版本，不代表本项目已选择或固定了它们。RFC 9106 与 RFC 7914 均为 Informational RFC，不是 Internet Standards Track 规范。[Go 源码标签](https://github.com/golang/go/tree/go1.26.5)；[x/crypto v0.58.0 模块元数据](https://github.com/golang/crypto/blob/v0.58.0/go.mod)；[RFC 9106 状态](https://www.rfc-editor.org/rfc/rfc9106#section-Status-of-This-Memo)；[RFC 7914 状态](https://www.rfc-editor.org/rfc/rfc7914#section-Status-of-This-Memo)

下文将**规范事实**、**库/API 事实**与**设计推论（未实测）**分开。未在 Windows、Linux、macOS 上构建或运行这些接口；源码中存在可移植 Go 实现，只能支持“实现没有显式绑定到某个操作系统”的有限观察，不能证明目标版本在三平台均成功构建、随机源/硬件路径相同，或得出耗时、峰值 RSS 等运行时结论。参数指导不是本项目性能基准；本地没有运行基准、测试或实现。

## 一手来源与版本

- [RFC 9106：Argon2，2021-09](https://www.rfc-editor.org/rfc/rfc9106)；具体输入、内存分配及参数建议见[§3.1](https://www.rfc-editor.org/rfc/rfc9106#section-3.1)、[§3.2](https://www.rfc-editor.org/rfc/rfc9106#section-3.2)、[§4](https://www.rfc-editor.org/rfc/rfc9106#section-4)、[§7.3](https://www.rfc-editor.org/rfc/rfc9106#section-7.3)。
- [x/crypto v0.58.0 Argon2 源码](https://github.com/golang/crypto/blob/v0.58.0/argon2/argon2.go)；[Argon2 包 API](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/argon2)。
- [RFC 7914：scrypt，2016-08](https://www.rfc-editor.org/rfc/rfc7914)；具体参数与 ROMix 见[§2](https://www.rfc-editor.org/rfc/rfc7914#section-2)、[§5](https://www.rfc-editor.org/rfc/rfc7914#section-5)、[§6](https://www.rfc-editor.org/rfc/rfc7914#section-6)。
- [x/crypto v0.58.0 scrypt 源码](https://github.com/golang/crypto/blob/v0.58.0/scrypt/scrypt.go)；[scrypt 包 API](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/scrypt)。
- [Go 1.26.5 AES 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/aes/aes.go)、[cipher AEAD/GCM 文档](https://pkg.go.dev/crypto/cipher@go1.26.5)、[GCM 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/cipher/gcm.go)；`NewGCMWithRandomNonce` 于 Go 1.24.0 加入（[Go 1.24 发布说明](https://go.dev/doc/go1.24#crypto_cipher)）。
- [x/crypto v0.58.0 ChaCha20-Poly1305 源码](https://github.com/golang/crypto/blob/v0.58.0/chacha20poly1305/chacha20poly1305.go)、[XChaCha20-Poly1305 源码](https://github.com/golang/crypto/blob/v0.58.0/chacha20poly1305/xchacha20poly1305.go)；[包 API](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/chacha20poly1305)。源码声明标准 ChaCha20-Poly1305 依 RFC 8439、XChaCha 依 draft-irtf-cfrg-xchacha-01，而非声称 XChaCha 有一份 RFC 标准。
- [Go 1.26.5 crypto/rand 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/rand/rand.go)；[Go FIPS 140-3 官方说明](https://go.dev/doc/security/fips140)。

## 口令、盐与 KDF

### Argon2id

**规范事实。** RFC 9106 定义 Argon2 版本 1.3（版本字节 `0x13`），并要求实现支持 Argon2id。输入为密码字节 `P`、盐 `S`、并行度/lanes `p`、输出/tag 长度 `T`、内存参数 `m`、pass 数 `t`；`p` 为正整数，`m` 以 KiB 计且至少为 `8p`，`t` 至少为 1。盐和密码长度上限在规范中各为 `2^32−1` 字节；密码哈希用途推荐 16 字节盐，并建议每个密码使用唯一盐。`t` 表示内存遍数；`p` 表示可并行计算链数，不等价于保证利用指定数量的 CPU。规范中 `T` 是派生输出长度，作为 KDF 使用时应按所需 AEAD 密钥长度固定，而非从文件头任意读取。[RFC 9106 §3.1](https://www.rfc-editor.org/rfc/rfc9106#section-3.1)；[RFC 9106 §3.2](https://www.rfc-editor.org/rfc/rfc9106#section-3.2)

RFC 9106 的名义 `m` 会向下取整至 `m′ = 4p × floor(m/(4p))` 个 KiB；算法按每块 1024 字节分配，因此主工作区为 `1024 × m′` 字节（约等于名义 `m` KiB，未计其他对象及运行时开销）。源码也先将 `memory` 向下取整为 `4 × threads` 的倍数、再保证至少 `8 × threads` KiB；每个 `block` 是 128 个 `uint64`，即 1024 字节，并以 `make([]block, memory)` 配置主工作区。源码遍历 `time` 次 pass。故 `m` 是主要内存成本参数、`t` 是遍数/计算成本参数、`p` 同时影响并行度及合法最小内存/取整；实际墙钟时间与 RSS 还取决于实现、调度器、操作系统、硬件与并发量。[RFC 9106 §3.2](https://www.rfc-editor.org/rfc/rfc9106#section-3.2)；[Argon2 源码](https://github.com/golang/crypto/blob/v0.58.0/argon2/argon2.go)

**Go 库事实。** `argon2.IDKey(password, salt, time, memory, threads, keyLen)` 派生 Argon2id 密钥；`memory` 单位为 KiB，`time` 是 pass 数，`threads` 对应并行 lanes，`keyLen` 是输出字节数。`IDKey` 返回 `[]byte` 而没有错误返回；源码对 `time < 1`、`threads < 1` 直接 panic，并分配由 `memory` 驱动的大型切片，API 没有为调用方设置文件级内存上限。函数不会替调用方生成盐；库注释要求使用随机盐。它的 `uint32`/`uint8` 参数类型和实现校验不是应用层资源预算。源码暴露的 `IDKey` 也未提供 RFC 输入中的 optional secret 与 associated-data 参数。[Argon2 API 与源码](https://github.com/golang/crypto/blob/v0.58.0/argon2/argon2.go)；[Argon2 API 文档](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/argon2#IDKey)

**规范参考配置，不是本项目建议或性能结果。** RFC 9106 的第一组选项为 Argon2id `t=1, p=4, m=2^21 KiB`（2 GiB）、16 字节盐、32 字节输出；内存较少时第二组选项为 `t=3, p=4, m=2^16 KiB`（64 MiB）、相同盐/输出长度。Go `IDKey` 文档重复了这两组数值。与本项目已确认的单次 KDF 专用内存预算 256 MiB 对照，第一组的 Argon2 名义主工作区已超过该预算；第二组名义主工作区低于预算，但这不等同于批准采用该组或实测确认其实际 KDF 总分配符合预算。约 1 秒仅为参考主机调参目标，尚未实测；RFC 的参考值、背景设备/CPU时间及 Go 文档示例均非本项目跨平台基准、实际内存峰值或启动延迟保证。[RFC 9106 §4](https://www.rfc-editor.org/rfc/rfc9106#section-4)；[Go Argon2 API 注释](https://github.com/golang/crypto/blob/v0.58.0/argon2/argon2.go)；[本项目已确认的资源约束](#本项目已确认的资源约束)

### scrypt

**规范事实。** RFC 7914 中 `N`（CPU/内存成本）必须大于 1 且是 2 的幂；`r` 是 block size 参数；`p` 是独立 ROMix 的并行化参数；盐通常应唯一且随机。规范未把固定盐字节数设为唯一必选长度，不能把 Argon2 的 16 字节推荐自动称作 scrypt 的规范要求。ROMix 为每个 `p` 分支保存 `N` 个、每个 `128r` 字节的状态块，故主要内存数组 `V` 的负载为 `128 × N × r` 字节；初始 B 数据另为 `128 × r × p` 字节。按算法对每分支执行 N 次填表与 N 次混合，工作量随 `N`、`r`、`p` 增加；这描述算法结构，不是时间测量。[RFC 7914 §2](https://www.rfc-editor.org/rfc/rfc7914#section-2)；[§5 ROMix](https://www.rfc-editor.org/rfc/rfc7914#section-5)；[§6 scrypt](https://www.rfc-editor.org/rfc/rfc7914#section-6)

**Go 库事实。** `scrypt.Key(password, salt, N, r, p, keyLen)` 参数为 `int`，失败时返回 error；校验 `N` 为大于 1 的 2 的幂、`r,p > 0`，并针对乘积/整数溢出拒绝超大值。源码对 `V` 分配 `32×N×r` 个 `uint32`（正好 `128Nr` 字节），对 `xy` 分配 `64r` 个 `uint32`（`256r` 字节），对 B 分配 `128rp` 字节，另有派生和临时开销。通过整数合法性校验不等于适合当前服务的成本上限；合法但极大的参数仍会请求大量分配、可能导致进程内存压力或分配失败。Go 包注释里的 `N=32768, r=8, p=1` 是其标注为“as of 2017”的交互登录示例/建议，不是当前设备或本项目性能基准，也不是 RFC 的普适默认值。[scrypt 源码](https://github.com/golang/crypto/blob/v0.58.0/scrypt/scrypt.go)；[scrypt API](https://pkg.go.dev/golang.org/x/crypto@v0.58.0/scrypt#Key)

本项目已确认口令文本为有效 UTF-8 且按约定转换为 UTF-8 字节；KDF 应接收这些确定的字节，不得再做裁剪或 Unicode 归一化。具体输入规约见[口令票据](../issues/03-passphrase-input.md#口令文本与跨平台一致性)。KDF 盐是公开随机输入、用于区分派生结果，不是 AEAD nonce，也不需要保密；每个新建/重加密文件都应生成新盐。Go 官方 `crypto/rand` 提供 CSPRNG 字节；Go 1.26.5 文档列明 Windows 使用 ProcessPrng、macOS 使用 `arc4random_buf`，Linux 通常使用 `getrandom(2)`，并指出 `rand.Read` 在随机读取失败时会终止进程。这里是 Go 源码对平台路径的描述，不是本任务对系统配置的实测。[crypto/rand 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/rand/rand.go)

## AEAD 参数、nonce 与 AAD

### AES-GCM（标准显式 nonce）

**Go API 事实。** `crypto/aes.NewCipher` 只接受 16、24 或 32 字节 AES 密钥；若协议统一派生 32 字节则对应 AES-256，但该事实本身不代表应选 AES-256。Go `cipher.NewGCM` 面向 AES 128-bit block cipher，标准 nonce 长度为 12 字节、默认 tag 16 字节，`AEAD.Overhead()` 为 tag 长度；`AEAD.Seal` 要求同一密钥下 nonce 对所有消息唯一。普通 `NewGCM` 不负责生成/追踪调用方传入的 nonce，重用约束由封装协议实现。`NewGCMWithNonceSize` 与 `NewGCMWithTagSize` 在 Go 源码中被限定为兼容既有非标准格式的用途，新格式通常不应自行改变标准尺寸。[AES `NewCipher`](https://github.com/golang/go/blob/go1.26.5/src/crypto/aes/aes.go)；[AEAD 与 GCM 文档](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)；[Go GCM 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/cipher/gcm.go)

**随机 nonce 变体。** Go 1.24 起的 `cipher.NewGCMWithRandomNonce` 仅接受由 `crypto/aes.NewCipher` 建立的 AES block；`Seal` 内部生成随机 96-bit nonce 并前置到密文，`Open` 从密文提取它；接口 `NonceSize()` 返回 0（调用时传空 nonce），`Overhead()` 为 28 字节（12 nonce + 16 tag）。标准库明确规定同一密钥不可加密超过 `2^32` 条消息，以将随机 nonce 碰撞风险保持在可忽略水平。它减少了调用方误传重复 nonce 的机会，但**没有取消密钥级消息数上限**，也不代表存储格式可省略整个密文长度限制。[Go 1.24 发布说明](https://go.dev/doc/go1.24#crypto_cipher)；[Go GCM API/源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/cipher/gcm.go#L79-L99)

Go 1.26.5 的 GCM 源码还拒绝超过 `(2^32−2)×16` 字节的单条明文（约 64 GiB）并约束认证 tag 至少 12 字节。此为实现/API 极限，不是建议尺寸；本项目已确认解密载荷（配置及分类元数据）最多 16 MiB，实际加密文件还包含有界头部、nonce 和 AEAD tag，格式实现须在认证前由长度/文件读取上限确保不能解出超过该载荷限制。[Go GCM 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/cipher/gcm.go)；[项目资源约束确认](#本项目已确认的资源约束)

### XChaCha20-Poly1305

`golang.org/x/crypto/chacha20poly1305.NewX` 接受 32 字节密钥；`NonceSize()` 为 24 字节，tag/overhead 为 16 字节。`NewX` 不生成 nonce：调用方仍需通过 CSPRNG 等方式准备 24 字节 nonce，按 `cipher.AEAD` 合同保证同一密钥下唯一。库源码明确说扩展 nonce 适合随机生成、在无法轻易确保唯一时优先考虑；这表示随机碰撞概率适合降低风险，并非数学上永不碰撞。XChaCha 源码复用标准 AEAD 的整条消息 slice API，明确指出 API 要求整条消息在内存中，底层实现最大明文为 `(2^38−64)` 字节；这同样不是项目文件大小建议。XChaCha 依据该 Go 包注明的 CFRG draft（不是 RFC 9106/8439 中标准 ChaCha 96-bit nonce 的同一参数接口）。[x/crypto v0.58.0 `NewX`/nonce 与尺寸实现](https://github.com/golang/crypto/blob/v0.58.0/chacha20poly1305/xchacha20poly1305.go)；[标准 ChaCha 参数与 key 常量](https://github.com/golang/crypto/blob/v0.58.0/chacha20poly1305/chacha20poly1305.go)；[AEAD nonce 唯一性合同](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)

### AAD、认证失败与头部

Go `cipher.AEAD` 的 `Seal` 会同时认证密文和 `additionalData`；`Open` 必须使用与加密端相同的 nonce 和附加数据，并在认证成功时才把明文附加到结果。Go GCM 源码在认证失败时返回 `nil, err`，且清空输出缓冲区；但 AEAD 通用接口提醒失败时 `dst` 的容量范围内容可能已被覆盖，调用方必须丢弃错误路径的输出，不得解析或返回它。[Go 1.26.5 `AEAD` 文档](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)；[GCM `Open` 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/internal/fips140/aes/gcm/gcm.go)

**格式设计推论（未实测）。** 可将精确的、规范化编码的未加密头部作为 AAD，使 magic/版本、算法标识、KDF 标识与参数、盐、明文/密文长度等影响解释的字段在认证后不能被静默修改。AEAD nonce 本身由 `Open` 参数作为认证运算输入，不应误以为普通附加数据能替代 nonce。选择只用固定格式/参数 profile 时，可认证相应的编码值；无论如何，所有决定解析或解密方式的明文字段必须在成功认证前视为不可信。认证只保护完整性，不隐藏明文头部；敏感标记、配置数据等按项目已确认要求应与内容一起置于加密载荷，而不是放在可见头部。[Go AEAD 语义](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)；[项目规格 §加密与泄露边界/文件格式](../spec.md#加密与泄露边界)

## 认证前的资源耗尽面与有限解析

**可证事实。** 解密前必须根据口令和盐/成本参数得到 AEAD 密钥；随后才能调用 `Open` 验证密文及 AAD。因此，普通 AEAD 本身不能在 KDF 前证明文件头可信。Argon2 `IDKey` 把内存参数直接用于工作区分配，scrypt 把 `N,r,p` 用于数组分配与循环；任何可由输入文件控制的成本参数都可能在认证之前触发昂贵计算或分配。scrypt/Argon2 API 的数值/溢出校验不构成针对单个本地服务的策略上限。[Argon2 源码](https://github.com/golang/crypto/blob/v0.58.0/argon2/argon2.go)；[scrypt 源码](https://github.com/golang/crypto/blob/v0.58.0/scrypt/scrypt.go)；[AEAD.Open 文档](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)

**未实测的攻击面判断。** 能修改或替换本地 vault 文件的人，可以构造未认证头部并诱使解锁路径按允许的最高成本运行；用户提供口令不能阻止这类 KDF 资源消耗，因为认证恰好发生在派生密钥之后。严格上限能把单次最坏成本限制在协议/服务明确定义的范围内，但不让该成本变为零；并发解锁还会叠加，因此仅靠参数上限不能承诺整体并发内存或服务可用性。这里是从 API 顺序和分配路径推出的威胁分析，不是本项目运行时压测结论。

### 本项目已确认的资源约束

**产品约束，不是算法规范或实测结论。** 解密载荷（配置值与分类元数据）上限为 **16 MiB**；这是明文载荷上限，不等同于整个文件字节数，密文还含头部、nonce/tag 等格式开销。每次 KDF 的专用内存预算为 **约 256 MiB**，它不是整个进程的峰值 RSS 或进程整体内存上限；不同 KDF 的主工作区及辅助数组应按其源码公式纳入预算，Go 运行时/调用方其他对象和多次并发 KDF 不由该单次预算自动覆盖。参考主机上的 KDF 调参目标约 **1 秒**，但没有实测，不能写成耗时保证；也没有自动降低成本以适配较慢主机的约定。算法事实与参数映射仍须按这些边界评估，而不据此选择算法或直接定档。[产品约束来自用户确认；Argon2 内存模型](https://www.rfc-editor.org/rfc/rfc9106#section-3.2)；[scrypt 内存模型](https://www.rfc-editor.org/rfc/rfc7914#section-5)

**派生前至少应做的有限检查（设计约束，不是批准参数）。**

1. 在读取可变长度数据之前，读取固定长度前缀并检查 magic、已支持的精确版本和算法/KDF 标识；未知版本/算法拒绝，不从未认证字段加载插件或任意算法。头长度、盐长度、nonce 长度、长度字段宽度均应固定或有很小且明确的上限；拒绝整数溢出、截断、超长、重复/歧义编码。
2. KDF 成本不要接受任意合法范围。优先把文件头字段解释为有限的 profile 标识，或逐项校验 `t/m/p`、`N/r/p` 并对每一项施加服务本地硬上限；确认乘积、字节数、转换为 `int` 的操作不溢出。派生长度固定为所选 AEAD 所需长度（AES 可 16/24/32；XChaCha 固定 32），不信任文件头给出的任意 `keyLen`。Argon2 盐可采用固定长度，RFC 9106 推荐 16 字节；scrypt 的具体盐宽度需由文件格式明确规定，RFC 7914 没有要求与 Argon2 相同的固定宽度。
3. 在总文件读取有界的前提下，先核验声明与实际长度，并确认 AEAD 明文长度不超过已确认的 **16 MiB** 载荷上限；头部和 AEAD 开销需由格式的有限长度规则另行约束，不能把 16 MiB 误作整个密文文件大小。不能对不可信文件无界 `ReadAll`，也不能仅信任文件头自报长度。GCM/XChaCha 所示 `AEAD` 接口接收完整切片，x/crypto XChaCha 源码也明确要求整条消息在内存，因此库最大消息尺寸远高于项目上限；受控读取应同时对实际字节数限流，避免头部/载荷触发无界分配。
4. 仅在所有上限通过后派生密钥；将用于格式解释的头部字节与密文一并送入 AEAD 认证（nonce 作为 nonce 参数）；`Open` 成功之后才解析 JSON、敏感元数据或构造对外配置对象。失败时完全丢弃解密缓冲区，保持“认证失败不向调用方返回任何配置”的既定契约。
5. 额外限制并发解锁/排队可降低多个合法上限请求叠加的风险；上限、排队行为、取消和调度方案目前均未测量，也不在本研究中设值。

以上建议只陈述文件解析与 API 顺序所需的安全条件；不替用户选择算法、盐/nonce 编码、KDF 参数档位或并发调度。已确认的 16 MiB 解密载荷上限、单次 KDF 专用内存预算 256 MiB、参考主机约 1 秒调参目标各有不同语义；这些约束不意味着已得到运行耗时/进程峰值内存的测量，也不应在认证成功前信任文件头。

## 自行组合原语与成熟文件容器的协议责任

直接把 Argon2/scrypt 和 `cipher.AEAD` 拼起来，只获得 KDF 与单条消息 AEAD 接口；它不会替应用定义文件 magic/version、规范化编码、参数迁移和拒绝策略、nonce/盐存放规则、AAD 中究竟绑定哪些头部字段、密文长度边界、完整文件截断/尾随数据检测、成功认证前不释出明文的读取流程、跨版本互操作和兼容测试。这些均成为自定义格式的维护与审计责任；标准 API 只契约其函数参数和认证行为，并不自动给应用层文件格式背书。[Go AEAD 接口合同](https://pkg.go.dev/crypto/cipher@go1.26.5#AEAD)；[RFC 9106](https://www.rfc-editor.org/rfc/rfc9106)；[RFC 7914](https://www.rfc-editor.org/rfc/rfc7914)

作为“成熟容器承担一部分协议工作”的第一方规范例子，age v1 规范定义了版本行、recipient stanzas、header MAC、文件密钥、payload 分块/认证及 final-chunk 语义；其口令 recipient 规定 scrypt 参数和盐布局。age Go 实现还提供 `SetMaxWorkFactor` 限制解密时接受的成本，并说明默认上限可能不适合处理不可信文件。采用既有容器可复用其明确的格式和实现，而不是再设计这些协议元素；但仍须核对它是否满足本项目对 UTF-8 口令字节、完整载荷认证时点、文件大小、元数据保密、资源预算和兼容性的确切要求。这里仅说明协议责任差异，不结论说 age 或任一容器适配/不适配，也不替用户选容器。[age v1 第一方格式规范](https://c2sp.org/age)；[age v1.3.2 scrypt 实现/API](https://github.com/FiloSottile/age/blob/v1.3.2/scrypt.go)；[age v1.3.2 API 文档](https://pkg.go.dev/filippo.io/age@v1.3.2)

## FIPS 与跨平台保证的边界

Go 官方说明，Go Cryptographic Module 是标准库 `crypto/internal/fips140/...` 中实现获批算法的模块；FIPS 140-3 依赖具体验证模块与目标运行环境，启用 Go 的合规模式本身并不自动满足个别监管要求。官方页面在 2026-04-28 说明 v1.26.0 模块处于 CMVP Pending Review，而 Go Cryptographic Module v1.0.0 覆盖 CMVP 证书 #5247；算法验证证书与模块 CMVP 认证不是同一项结论。Go 1.26.5 AES/GCM 源码使用内部 FIPS 模块，`NewGCMWithRandomNonce` 限于 AES；x/crypto 的 `New`/`NewX` 在 FIPS 140-only 模式下返回错误。Argon2 与 scrypt 位于外部 `golang.org/x/crypto` 模块，不应因 Go 标准库 AES/GCM 的 FIPS 事实而宣称它们也在同一验证范围内；本文件也未对任何应用二进制作认证判断。[Go FIPS 140-3 说明](https://go.dev/doc/security/fips140)；[Go AES/GCM 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/aes/aes.go)；[Go GCM 随机 nonce API](https://github.com/golang/go/blob/go1.26.5/src/crypto/cipher/gcm.go)；[x/crypto XChaCha FIPS 检查](https://github.com/golang/crypto/blob/v0.58.0/chacha20poly1305/xchacha20poly1305.go)

Go AES 文档说明，AES 操作通常不是常量时间；有 AES 硬件支持时例外，且对应 AES GCM 的 GHASH 才有文档所述常量时间属性。是否启用硬件路径取决于运行平台，源码说明不能替代目标环境实测。`crypto/rand` 在 Go 1.26.5 源码中列有 Windows/macOS/Linux 各自随机源路径，但本研究没有验证目标主机环境或跨平台二进制行为。Argon2/scrypt 源码中纯 Go 的切片/循环实现、或 GCM 源码引用内部 FIPS 实现，只能描述源码事实，不能推出任何运行时速度、RSS、常量时间、跨平台构建成功或 FIPS 认证保证。[Go AES 源码说明](https://github.com/golang/go/blob/go1.26.5/src/crypto/aes/aes.go)；[crypto/rand 源码](https://github.com/golang/go/blob/go1.26.5/src/crypto/rand/rand.go)；[Go FIPS 官方说明](https://go.dev/doc/security/fips140)

## 本地状态

本研究仅写入 `.scratch/service-config-vault/research/go-crypto-resource-limits.md`。按当前任务提供的仓库环境信息，该目录不是 Git 仓库、没有远程；本地研究未提交、未发布。