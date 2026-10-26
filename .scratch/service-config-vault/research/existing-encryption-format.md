# age 加密容器与 Go 口令模式核查

## 范围与结论摘要

本笔记核查 age v1 文件协议及官方 Go 库，不替本项目选择封装方案。协议来源为 [age v1 规格](https://age-encryption.org/v1)（稳定引用：[C2SP age 规格](https://c2sp.org/age)）；Go 源码事实固定到 [`filippo.io/age` v1.3.2](https://github.com/FiloSottile/age/tree/v1.3.2)，其版本化 [Go API 文档](https://pkg.go.dev/filippo.io/age@v1.3.2)列出 BSD-3-Clause。age 把口令模式实现为 scrypt 包封随机文件密钥，再以该文件密钥认证加密流式载荷；它是文件加密格式/库，不是配置序列化、配置元数据模型或原子文件写入层。

主要适配点：现成 v1 格式、Go 直接 API、头部 MAC 与分块 AEAD、口令模式的 scrypt 上限 API、固定头解析上限。age v1.3.2 源码同时明确不推荐在自动化系统中使用 ScryptRecipient；这与本项目已确认的无人值守显式口令场景存在上游建议上的张力，但不改变已批准的口令模式。主要集成约束：流式解密须暂存并读至 EOF 后才可返回配置；项目已确认 16 MiB 解密载荷上限、约 256 MiB 单次 KDF 内存预算及参考主机约 1 秒派生目标（未测），这些限制须由调用方落实，age 默认解密上限远高于预算。

## 协议及加密算法

- **文件格式版本：**文件以明文 `age-encryption.org/v1` 版本行开始；规格仅定义 v1，版本行后的头部格式可在未来版本变化。Go v1.3.2 的解析器要求精确匹配此 v1 行，因此未知版本不能按 v1 解读。Go 包文档称稳定版 v1 文件可由后续 v1 API 解密；该兼容性承诺不等于支持 v2 或未稳定版本。[规格：Header / Version line](https://age-encryption.org/v1)；[v1.3.2 format parser](https://github.com/FiloSottile/age/blob/v1.3.2/internal/format/format.go)；[v1.3.2 API 兼容性注释](https://github.com/FiloSottile/age/blob/v1.3.2/age.go)
- **口令包封：**每个文件使用 16 字节随机文件密钥。scrypt stanza 暴露 16 字节 CSPRNG 盐及十进制 `logN`；派生参数固定 `r=8, p=1, dkLen=32`，盐输入为 `"age-encryption.org/v1/scrypt" || salt`。派生密钥用 ChaCha20-Poly1305 包封 16 字节文件密钥，nonce 固定为 12 个零字节，密文连同 16 字节 AEAD 标签共 32 字节。scrypt stanza 必须是唯一 stanza，不能和其他收件人混用，便于保留口令认证语义。[规格：scrypt recipient stanza](https://age-encryption.org/v1)；[v1.3.2 `scrypt.go`](https://github.com/FiloSottile/age/blob/v1.3.2/scrypt.go)
- **载荷加密：**载荷开头为每文件新生成的 16 字节随机 nonce；HKDF-SHA-256 从文件密钥和该 nonce 派生 32 字节载荷密钥。明文按至多 64 KiB 分块，以 ChaCha20-Poly1305 认证加密；块 nonce 由 88 位递增计数器与末块标志构成，每块含 16 字节认证标签。未出现有效末块即截断错误。[规格：File key / Payload](https://age-encryption.org/v1)；[v1.3.2 `internal/stream/stream.go`](https://github.com/FiloSottile/age/blob/v1.3.2/internal/stream/stream.go)
- **头部认证：**末尾头部 MAC 是 HMAC-SHA-256；密钥由文件密钥经 HKDF-SHA-256（info=`header`）派生。MAC 覆盖从版本行开始、直到 `---` 标记（含标记、不含其后的空格）的完整头部，故认证版本及收件人 stanza（包括 scrypt 盐、工作因子和包封密文）。解密端先解析头并解包文件密钥，再常数时间比较头部 MAC；MAC 通过前 `Decrypt` 不返回载荷 Reader。[规格：Header MAC](https://age-encryption.org/v1)；[v1.3.2 `age.go` 的 `decryptHdr` / `Decrypt`](https://github.com/FiloSottile/age/blob/v1.3.2/age.go)

## scrypt 默认成本与不可信头部

| 项目 | 官方源码事实及含义 |
| --- | --- |
| 加密默认值 | `NewScryptRecipient` 默认 `logN=18`，即 `N=2^18`，并固定 `r=8,p=1`。源码注释估算“现代机器约 1 秒”；这是作者注释，不是本项目或各平台实测保证。项目确认的约 1 秒目标针对参考主机，尚未实测。调用方可在 `Wrap` 前用 `SetWorkFactor(logN)` 设置，允许值 1–30，越界会 panic。 |
| 解密默认上限 | `NewScryptIdentity` 默认 `maxWorkFactor=22`（源码注释“现代机器约 15 秒”同样不是性能保证）。`SetMaxWorkFactor(logN)` 可在 `Unwrap` 前降低上限，允许值 1–30，越界会 panic；头部声明值大于配置上限会在调用 scrypt 前拒绝。默认值应视作可被不可信文件诱发的最高派生成本，而非建议所有服务无条件接受。 |
| 自动化使用警示 | `ScryptRecipient` 源码注释明确称“不推荐用于自动化系统”，建议改用 `HybridRecipient` 或 `X25519Recipient`。这是上游维护者建议；本项目已批准无人值守服务显式口令输入，本笔记只记录该张力，不将其视为变更口令模式的决定。源码未量化该警示的原因或提供例外条件。[age v1.3.2 `scrypt.go`](https://github.com/FiloSottile/age/blob/v1.3.2/scrypt.go) |
| 内存量级 | age 解密使用 `N=1<<logN, r=8`；与其锁定的 [golang.org/x/crypto v0.55.0 scrypt 实现](https://github.com/golang/crypto/blob/v0.55.0/scrypt/scrypt.go)相符，该实现分配 `V := make([]uint32, 32*N*r)`。仅该主工作区即 `128*N*r` 字节：默认加密因子 `logN=18` 为 256 MiB；默认解密上限 `logN=22` 为 4 GiB。其他工作区、堆与运行时开销未计入；这是从源码分配尺寸计算出的理论量级，不是 RSS 实测。 |
| 认证前解析上限 | v1.3.2 解析器对**整个头部**设 `maxHeaderBytes = 2 << 20`（2 MiB），且最多 1,024 个 recipient stanza。解析完成后，才调用 identity 解包，因此头部大小受界；但它并不把 scrypt 默认上限降到适合本项目进程的值。 |

上表由 [age `scrypt.go`](https://github.com/FiloSottile/age/blob/v1.3.2/scrypt.go)、[age `internal/format/format.go`](https://github.com/FiloSottile/age/blob/v1.3.2/internal/format/format.go) 与其 [go.mod 锁定依赖](https://github.com/FiloSottile/age/blob/v1.3.2/go.mod)支持。项目已确认的资源边界为：最大解密载荷 16 MiB、约 256 MiB 单次 KDF 内存预算、参考主机约 1 秒派生目标（未测）；这些是项目约束，不是 age 的限制或已验证性能。调用方必须执行 16 MiB 上限并配置 `SetMaxWorkFactor`，不能沿用 age 默认 `maxWorkFactor=22`。`logN=18` 时仅主工作区已约 256 MiB，另有辅助内存；需结合“约”预算解释余量，不能把该算式当成实测峰值。

## 认证何时完整、如何避免部分配置泄漏

`age.Decrypt(src, identities...)` 会先解析头、尝试身份解包、校验头部 MAC、读取载荷 nonce，然后返回一个**惰性** `io.Reader`。载荷块在后续 `Read` 时逐块认证；因此一次 `Read` 成功、读取到最后一段明文、甚至头部 MAC 成功，都不足以证明全文件完整。流 Reader 在识别末块后还会检查其后确为 EOF，尾随数据会报错；缺失末块、损坏块、截断或尾随数据最终都会以读取错误暴露。[v1.3.2 `age.go`](https://github.com/FiloSottile/age/blob/v1.3.2/age.go)；[v1.3.2 `internal/stream/stream.go`](https://github.com/FiloSottile/age/blob/v1.3.2/internal/stream/stream.go)；[规格：Payload](https://age-encryption.org/v1)

要满足“认证失败不向调用方返回任何配置”，应用边界应当：

1. 把解密 Reader 的明文写入调用方不可见的暂存区（内存或受控临时存储），而不是边读边返回配置字段/调用业务代码。
2. 持续读取直到 Reader 报 EOF；只有读取完整成功且无非 EOF 错误，才解析整个暂存载荷、连同敏感性元数据构成内部结果并一次性交付。任何错误都丢弃暂存数据，不以部分明文、空配置或部分解析结果代替成功。
3. 在返回前完成应用层格式解析与语义校验；age 只认证 bytes，不验证其是否为合法 JSON、配置对象或与敏感元数据一致。

这是一种基于 age 流 Reader 行为的集成约束，不是 age API 自带的事务化配置返回保证。项目已确认解密载荷最多 16 MiB，但 age 不执行此上限，必须由调用方限长并在超限时失败；同时需要限制加密输入总字节，2 MiB 只限制头部，不限制载荷。实现限长读取时仍须检测超限而非把人为截断误当成正常 EOF。若暂存至磁盘，还需项目自己的权限、清理与失败原子性契约。

## 可见信息与项目适配

- 未加密可见的部分包括 v1 标识、scrypt 类型、公开盐、公开 `logN`、头部 stanza/包封密文长度、16 字节载荷 nonce、载荷总长度及分块结构。载荷没有填充；因为每块固定 16 字节标签、分块大小固定，减去可见头和 nonce 后可从密文长度精确计算明文长度（源码提供 `PlaintextSize`）。因此 age 不隐藏配置文件大小；头部/载荷区分也不会隐藏其加密参数。[规格：Header / Payload / scrypt](https://age-encryption.org/v1)；[v1.3.2 `internal/stream/stream.go`](https://github.com/FiloSottile/age/blob/v1.3.2/internal/stream/stream.go)
- 配置值与敏感性元数据都可放在同一加密载荷中，age 本身不提供此数据模型；必须由应用将二者一并序列化，不能把元数据放入明文头。格式不提供隐藏长度的填充策略。[age v1 规格](https://age-encryption.org/v1)；[项目已确认规格](../spec.md#加密与泄露边界)
- 项目口令契约要求有效 UTF-8、拒绝 BOM/空值/换行/NUL、保留空格且不做 Unicode 归一化；见[口令输入票据](../issues/03-passphrase-input.md#口令文本与跨平台一致性)。age API 的 `NewScryptRecipient(string)` / `NewScryptIdentity(string)` 只拒绝空字符串，并直接对 Go 字符串的字节切片做 scrypt；它不验证 UTF-8、拒绝 BOM/NUL/换行、裁剪空格或执行 Unicode 归一化。因此这些规则必须在应用 API 边界独立落实；age 库会将调用方提供的有效 UTF-8 字符串字节原样用于派生。[age v1.3.2 `scrypt.go`](https://github.com/FiloSottile/age/blob/v1.3.2/scrypt.go)
- 该格式将配置 bytes 作为单一 payload；认证完整性来自头部 MAC 和每块 AEAD，而不是只对解锁口令做校验。口令 stanza 本身不得混合其他 stanza。[age v1 规格](https://age-encryption.org/v1)

## Go 集成、平台与许可边界

Go 库公开 `NewScryptRecipient`、`NewScryptIdentity`、`Encrypt(io.Writer, recipients...)` 和 `Decrypt(io.Reader, identities...)`，可在进程内直接集成，不需调用 CLI 子进程；写端需关闭返回的 `io.WriteCloser` 以生成末块。v1.3.2 的 `go.mod` 声明 `go 1.25.0`、`toolchain go1.27.0`，并依赖 `golang.org/x/crypto v0.55.0` 等模块。源码模块许可为 BSD-3-Clause（需保留许可和免责声明，并遵守禁止未经许可背书的条款）。[API 源码](https://github.com/FiloSottile/age/blob/v1.3.2/age.go)；[go.mod](https://github.com/FiloSottile/age/blob/v1.3.2/go.mod)；[LICENSE](https://github.com/FiloSottile/age/blob/v1.3.2/LICENSE)；[pkg.go.dev 版本页](https://pkg.go.dev/filippo.io/age@v1.3.2)

age v1.3.2 的官方 CI 工作流配置了 Windows、macOS、Linux runner，并针对 stable/oldstable Go 运行测试；这只是仓库工作流矩阵的证据，不是本次实际测试结果、硬件架构覆盖清单或本项目三个平台验收证明。官方 README 也列出这三类系统的预构建发行物，但不能据此推导本项目的构建、文件替换、CSPRNG、资源预算或互操作性已验证。[CI workflow](https://github.com/FiloSottile/age/blob/v1.3.2/.github/workflows/test.yml)；[README](https://github.com/FiloSottile/age/blob/v1.3.2/README.md)

核查到的 age 规格、包文档、依赖清单与源码没有声明 FIPS 验证模块或认证编号。故本研究不能证明 age 在 FIPS 模式下可用或已认证，也不把使用 Go/x-crypto 源码推论成所有操作系统上的平台保证；若 FIPS 是约束，需另行取得适用模块、版本与运行环境的正式证据。

## 需由后续契约决定或验证

- 是否接受 age v1 作为容器，及是否接受其精确泄露载荷长度、公开 scrypt 参数的边界；本笔记不作方案选择。
- **已确认的资源边界（非待决值）：**解密载荷上限 16 MiB；单次 KDF 内存预算约 256 MiB；参考主机的派生目标约 1 秒，尚未实测。age 不实施载荷限额，且默认解密 `maxWorkFactor=22` 对应约 4 GiB 主工作区，调用方必须执行 16 MiB 限额并配置合适的 scrypt 上限。`logN=18` 主工作区约 256 MiB，辅助开销另计；不能据此声称 RSS 或耗时已经达标。
- 明文暂存方式与失败清除/访问权限；age 只负责解密流，不提供“完全解密并认证后才提交配置”的事务 API。
- Go 版本和依赖许可审查；v1.3.2 的 Go 版本要求可能不适用于目标部署基线，需按实际工具链确认。
- Windows、Linux、macOS 上本项目真实 Go 构建、口令互操作、认证失败无配置返回以及内存上限行为均未在此执行或验收；没有 FIPS 认证证据。

研究按用户确认的范围仅保留本地 Markdown；不要求创建 Git 研究分支或发布。当前工作区没有 Git 仓库或远程，因此文档留在本地，这符合本次交付范围；不得将其表述为上游发布或产品验收记录。

## 集成者补充：Windows 库级烟测

在上面的引用研究完成后，集成者用系统临时目录中的独立 Go 模块执行了一次 `go run -mod=mod .`。实际环境为 Windows/amd64、Go 1.27.2、`filippo.io/age v1.3.2`，使用合成口令及 131195 字节合成载荷，写入 `logN=18`、解密上限显式设为 18。

| 实际运行场景 | 观察结果 |
| --- | --- |
| 正常加密 | 生成 131409 字节密文，单次操作耗时 1.1857563 秒 |
| 完整解密并读取至 EOF | 明文字节逐一相同，单次操作耗时 1.7458453 秒 |
| 错误口令 | `Decrypt` 返回错误，不返回明文 Reader |
| 只篡改最后一个载荷字节 | 头部解密成功，先读取到 65536 字节正确明文，后续读取报告认证错误 |
| 截断最后一个密文字节 | 头部可解密，完整读取报告载荷认证错误 |
| 头部工作因子改为 19，而 identity 上限为 18 | 在解包阶段返回 `scrypt work factor too large: 19`，不返回 Reader |

该观测直接支持“age 的 Reader 不能作为已完整验证的配置快照交付”的集成约束。计时仅是当前主机单次库操作观察，不是稳定基准、约 1 秒目标已达标的证明或跨平台性能保证。没有测量内存峰值，没有测试应用层 16 MiB 限额、口令文本处理、载荷 schema、文件替换或非 Windows 运行时；本项目产品实现仍未开始。一次性研究程序不作为产品代码保留。
