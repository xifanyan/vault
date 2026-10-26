# CLI 与 Go 库接口契约

Type: grilling
Status: resolved
Labels: wayfinder:grilling
Mode: HITL
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 04, 05, 06

前置决策：[加密封装与文件格式契约](04-encrypted-file-format.md)、[Vault 与解锁凭据的便携使用契约](05-file-operations.md)、[JSON 查看与导出契约](06-json-export.md)。

## Question

怎样把已确定的数据、口令、文件与导出契约收敛为一套足够小且行为明确的 Go 接口，以及调用同一套规则的 CLI，让实现者和消费者不再猜测关键行为？

需要与用户确定：

- 初始化、默认配置与环境管理、键读写与删除、敏感标记、查看、导出和口令更换的命令与参数；不要求受控安装或注册操作。
- 配置值的类型输入、vault 与独立解锁凭据的路径、环境的显式选择；复制后可以更改文件位置，避免口令或敏感值出现在命令行参数中。
- 导出入口沿用已确认的整库层级视图、`service/data/redacted_paths` 结构、标准输出和默认不覆盖规则；只确定命令与方法接入，不重新增加导出模式。
- Go 库显式接收 vault 与解锁凭据文件的打开、按环境解析及快照读取入口，复用 CLI 的文件适配规则；确定写入能力的对外范围和调用者能否修改结果，不让业务调用方自行实现口令文件换行处理。
- 缺失键、合法空值、未知环境、认证失败、格式错误和普通文件 I/O 失败的 Go 错误与 CLI 退出码；库不能隐式提示终端或产生后台热更新，不引入复杂提交结果协议。
- 将错误、诊断及提示与可供脚本消费的输出分离，保持敏感内容不进入普通错误输出。
- 以人工管理、无人值守启动、复制 vault 与凭据后打开或显式重载三个流程核对契约；只指出阻碍这些核心流程的实现前缺口。

本票产出用户确认的接口与调用例子，不是 CLI、库实现或测试通过证明。

## Comments

- Q1：用户明确要求 Go 库也公开写入能力，不接受仅只读的公共 SDK。
- Q2：用户同意初始化可显式生成新的随机口令文件与 vault，同时保留已有凭据文件方式，不覆盖已有文件、不在读取失败后自动生成。
- Q3：用户确认 Go 先在内存修改，再显式 `Save`；CLI 每条修改命令成功后保存一次，不引入后台会话或事务协议。

## Answer

### 共同约束

CLI 名称及 Go 包名使用 `vault`。CLI 与公共 Go SDK 共用同一配置、分类、凭据适配和文件编码核心；SDK 同样能初始化、管理环境、修改值、分类、保存和更换解锁凭据，不把写入限定在 CLI 内部。

本契约只固定本地功能和包接口，不要求 HTTP 服务、远程模块发布、安装注册、锁文件或机器绑定。真实 Go 模块导入路径按实现仓库配置；下文示例使用包别名 `vault`。

### Go 对象与所有权

- `Vault` 是通过 `Open` 或 `Create` 得到的可编辑内存模型，绑定本次显式提供的 vault 路径；文件在操作结束后关闭，不要求调用方维持打开的句柄。
- `Layer` 是默认层或具名环境覆盖层的实时编辑视图。已取得环境层后若环境被删除，其访问返回 `ErrNotFound`；同名环境重建后，该名称视图指向新层，不保留孤立旧 map。
- `Config` 是 `Resolve` 返回的独立有效配置快照；后续编辑、保存或磁盘文件变化不会改变它。快照可并发只读；`Vault` 与 `Layer` 的修改和读取由调用方串行安排。
- `Get` 返回调用方拥有的 `json.RawMessage`；修改返回字节不能改变内部状态。`Set` 不保留调用方可变字节切片的别名。
- 公共读取返回真实 JSON 值，不做脱敏占位或隐式 `float64` 转换。业务方自行决定如何解码为 Go 类型；本库不添加泛型或自动结构体映射。
- 可编辑对象在本进程内保留本次解锁材料供保存使用；调用方控制对象寿命，不建立跨进程凭据托管，也不承诺 Go 内存中所有副本都能被擦除。

### Go 接口

`Classification` 是字符串类型，只允许常量 `Sensitive = "sensitive"` 与 `NonSensitive = "non-sensitive"`。下表是接口签名和语义，不是已经存在的实现。

| 签名 | 行为 |
| --- | --- |
| `GenerateCredentials(path string) error` | 只创建新的随机口令文件，不覆盖已有路径，不返回或打印口令 |
| `Create(vaultPath, credentialsPath, service string) (*Vault, error)` | 使用已有凭据文件创建空 vault 并持久化一次，返回可编辑对象；目标已存在则失败 |
| `Open(vaultPath, credentialsPath string) (*Vault, error)` | 读取明确给定的两份文件，共用凭据文件适配规则；完整验证后返回模型 |
| `(*Vault).Service() string` | 返回服务标识 |
| `(*Vault).Environments() []string` | 返回排序后的环境名称副本 |
| `(*Vault).Defaults() *Layer` | 取得默认层实时视图 |
| `(*Vault).Environment(name string) (*Layer, error)` | 取得已存在的环境覆盖层，不做默认值合并 |
| `(*Vault).CreateEnvironment(name string) error` | 在内存新增空环境；名称非法或已存在时失败 |
| `(*Vault).DeleteEnvironment(name string) error` | 显式删除该环境及覆盖值，包括非空环境；保留逻辑分类记录 |
| `(*Layer).Get(pointer string) (json.RawMessage, error)` | 从该存储层读取原值；空指针读取根，不补入继承值 |
| `(*Layer).Set(pointer string, value json.RawMessage) error` | 按既定路径规则修改内存值，维护默认敏感分类，不写盘 |
| `(*Layer).Delete(pointer string) error` | 删除本层显式值，不删除分类；该层没有键时返回 `ErrNotFound` |
| `(*Vault).Classify(pointer string, classification Classification) error` | 显式改变全局逻辑路径分类，不递归取消子标记；路径须存在于某层或已有保留分类 |
| `(*Vault).Resolve(environment string) (*Config, error)` | 对已存在环境合并默认值和覆盖，返回不变快照 |
| `(*Config).Get(pointer string) (json.RawMessage, error)` | 读取快照中的真实 JSON 原值，空指针取得整个有效配置 |
| `(*Vault).Save() error` | 校验并保存当前模型，使用本对象已提供的解锁材料，不重新提示或查找其他来源 |
| `(*Vault).SaveWithCredentials(credentialsPath string) error` | 显式读取另一凭据文件，将当前模型保存为新口令保护的文件；成功后后续 `Save` 使用新材料 |
| `(*Vault).ExportJSON(includeSensitive bool) ([]byte, error)` | 返回既定整库层级 JSON，反映当前内存模型，不执行文件输出或保存 |

新建环境不隐式从其他环境复制配置。环境删除或值删除后，保留的分类路径仍按既定规则生效；读取不存在的键与读取真实 `null` 必须区分，后者返回 JSON `null` 且无错误。

`Layer.Set`、`Layer.Delete` 不能对空指针写入或删除配置根。显式 `Classify` 只改变分类元数据，不替换根值；它适用既定的敏感祖先与非递归取消规则，包括显式对配置根设置分类的情况。数组仍只作为整体读取、替换和分类。

### 修改、保存与错误后的状态

单次修改的指针、环境、JSON 值或分类无效时，内存模型保持原样；此前其他成功修改不自动回滚。完整载荷大小及整体编码约束在保存前统一验证，不能为每个键重复加密。

除 `Create` 明确创建文件外，普通修改在 `Save` 前不改磁盘。`Save` 验证失败不触碰目标，失败的内存修改集合可供调用方检查或调整；实际 I/O 失败沿用基本文件边界，不自动重试、合并或回滚，也不声称目标绝对未动。

从打开到保存视为一个由调用方独占安排的编辑流程；外部文件更新后应重新 `Open`，不能用旧模型自动覆盖新内容。无需 `Reload` 方法或后台监听；旧 `Config` 快照一直保持不变。

`SaveWithCredentials` 使用已经通过 `Open` 认证或由 `Create` 建立的旧解锁上下文，遵循同一独占编辑前提。它显式保存当前全部内存修改并更换口令，不修改旧凭据文件，也不撤销旧 vault 副本；新输入不合法时不更换对象的解锁材料。I/O 失败后先重新打开核对，再决定后续写入。

### 凭据生成与初始化

`GenerateCredentials` 用系统密码学随机源生成 32 字节，编码为无填充的 base64url 文本并以单个 LF 结束。它仍是既定口令模式的高熵口令文件，不是新私钥协议，不打印秘密。

CLI 的显式生成选项先生成新凭据，再调用 `Create`；已有输入文件读取失败不会进入此分支。新文件一律不覆盖已有路径，并拒绝明显的输出路径冲突。普通系统权限由操作者负责，不额外建立 ACL 管理器。

两份文件不承诺原子创建；发生 I/O 错误可能留下一份文件，应明确报错并由操作者核对，不自动删除已生成的凭据或假称全部未发生。`Create` 自身完成初始化的一次持久化，CLI 不能再多调用一次 `Save`。

### CLI 语法

公共选项位于子命令之前；命令选项位于该命令的位置参数之前。所有功能命令必须有 `--vault FILE`，不搜索隐含默认位置。`--help` 不需要文件或凭据，退出成功。

当前口令来源为互斥的 `--passphrase-file FILE`、`--passphrase-stdin`、`--prompt-passphrase`。未指定时仅交互终端可提示，否则参数错误；显式终端输入没有可用终端时失败，不读其他通道。`init --generate-passphrase-file FILE` 与当前口令输入来源互斥，是新输出的显式选择，不是读取回退。

| 子命令及命令参数 | 作用与成功输出 |
| --- | --- |
| `init --service ID [--generate-passphrase-file FILE]` | 新建 vault；显式生成时同时创建凭据。否则使用当前输入来源。成功标准输出为空 |
| `env list` | 输出排序后的环境名称 JSON 数组 |
| `env create NAME` | 新建空环境并保存；标准输出为空 |
| `env delete NAME` | 显式删除环境及其覆盖值并保存，不额外确认；标准输出为空 |
| `get (--defaults 或 --env NAME) [--include-secrets] [POINTER]` | 默认层读原值，环境读有效值；省略指针读根，输出下述查询 JSON |
| `set (--defaults 或 --env NAME) (--value-file FILE 或 --value-stdin) [--classification CLASS] POINTER` | 从文件或标准输入读取恰好一份 JSON 值；修改所选原始层，必要时显式分类，然后保存一次；标准输出为空 |
| `delete (--defaults 或 --env NAME) POINTER` | 只删除所选存储层的显式值并保存；标准输出为空 |
| `classify POINTER CLASS` | `CLASS` 仅为 `sensitive` 或 `non-sensitive`，作用于所有环境共享的逻辑路径；保存后标准输出为空 |
| `export [--output FILE] [--overwrite] [--include-secrets]` | 输出既定整库层级 JSON；指定文件时默认不覆盖，不增加环境导出模式 |
| `passwd [--new-passphrase-file FILE 或 --new-passphrase-stdin 或 --prompt-new-passphrase]` | 用当前来源认证旧口令，按新来源保存新口令保护的文件；未指定新来源时只允许交互确认，成功标准输出为空 |

读取/写入的 `--defaults` 与 `--env` 必须恰好选择一个；环境不存在时报错，不自动创建或退回默认层。`get --env` 读取有效配置不等于增加一种 `export` 模式。`set` 新键默认敏感；显式 `--classification` 只改目标本身的全局分类，不对整个子树取消保护。

`set` 不接受命令行中的明文值；配置 JSON 用文件或标准输入传递，口令也不出现在参数内容中。配置值输入按既定 16 MiB 载荷上限限长读取，保存时还计入其他配置与元数据。标准输入只分配给一个逻辑输入；来源与形状冲突在读文件、提示或消费输入前拒绝。

CLI 的普通修改命令执行打开、修改、一次保存；`passwd` 只执行一次新口令保存，读取和导出命令不保存。交互新口令仍需确认两次，文件与标准输入方式不额外确认；所有命令均不自动等待锁或重试。

### 查询 JSON 与错误约定

`get` 固定输出 `value` 和 `redacted_paths`。路径相对于所选配置根，而不是相对于返回的 `value`；空数组表示未隐藏。默认遵守敏感容器不透明规则，直接返回保护边界的 `null`，不先泄露后代存在性。`--include-secrets` 才走真实读取，恢复正常路径错误。

根与值读取均保持 JSON 类型和精度；合法 `null` 不等于不存在。删除环境层中不存在的覆盖键返回 `ErrNotFound`，即使有效配置能从默认层继承该值；不将不存在当成成功。

公共 Go 错误可用 `errors.Is` 检查：`ErrInvalidArgument`（领域输入无效）、`ErrNotFound`（环境/键/分类路径不存在）、`ErrAlreadyExists`（已存在）、`ErrAuthentication`、`ErrInvalidFormat`、`ErrLimitExceeded`。普通文件错误保留对 `fs.ErrNotExist`、`fs.ErrPermission`、`fs.ErrExist` 等的可检查关系，不吞掉原因或伪造空配置。

CLI 退出码为：`0` 成功或帮助；`2` 命令/选项/参数数量或来源互斥等调用形状错误；`1` 调用后的操作失败，包括领域值校验、找不到对象、已存在、凭据、认证、格式、资源与 I/O 错误。无论类别，诊断都不回显口令、配置秘密或隐藏结构；具体文案不是兼容契约。

查询与导出标准输出为纯 JSON；修改命令不输出配置或凭据，诊断仅到标准错误。输出 I/O 失败不能伪报成功，不增加事务化输出状态。

### 三条主流程走查

以下命令是未来 CLI 契约示例，不是本次已经运行的命令。先准备 `host.json`，内容为 JSON 字符串 `"db.internal"`。

```sh
vault --vault service.vault init --service example-service --generate-passphrase-file service.pass
vault --vault service.vault --passphrase-file service.pass env create prod
vault --vault service.vault --passphrase-file service.pass set --defaults --value-file host.json --classification non-sensitive /database/host
vault --vault service.vault --passphrase-file service.pass get --env prod /database/host
vault --vault service.vault --passphrase-file service.pass export --output view.json
```

人工管理流程因此能创建两份便携材料、增加环境、设置普通默认值、从环境继承读取并导出保存层级；不需要后台服务、注册或额外安装命令。

Go 读写示例假定已有 `prod` 环境。它使用包别名 `vault` 和标准包 `encoding/json`，只展示未来方法契约，不是已经实现的可执行程序：

```go
func updateDatabase(vaultPath, credentialsPath string) error {
	v, err := vault.Open(vaultPath, credentialsPath)
	if err != nil {
		return err
	}
	layer, err := v.Environment("prod")
	if err != nil {
		return err
	}
	if err := layer.Set("/database/host", json.RawMessage(`"db.internal"`)); err != nil {
		return err
	}
	if err := v.Classify("/database/host", vault.NonSensitive); err != nil {
		return err
	}
	return v.Save()
}
```

业务方通过 `Resolve("prod")` 取得快照并调用 `Config.Get` 读取真实 JSON。保存前可以从模型读取或导出当前修改；磁盘只在显式保存后改变，已有快照不变。

迁移流程只复制完整的 vault 与凭据文件到新路径，再用相同 CLI 参数或 `Open` 打开。JSON 导出仍是查看格式，不能替代这两份便携材料；源主机路径、账户及内存对象不参与新主机解锁。

这三条流程已按契约逐项走查，没有暴露需要新立票据的实现前决策。路线图可以交接工程实现；产品代码、命令执行、包链接及实际跨平台验收仍未完成，不能用文档检查冒充。
