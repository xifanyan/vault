# Service Configuration Vault

This project defines a portable vault for managing service configuration.

## Language

**Vault**:
A collection of hierarchical service configuration values, including sensitive values.

**服务默认值（Service Defaults）**:
同一服务各环境共享的基础配置。它是独立的配置作用域，不是名为 `default` 的环境。

**环境（Environment）**:
用于区分同一服务不同部署用途的具名配置上下文，例如开发与生产；它本身不是访问权限隔离。

**环境覆盖（Environment Override）**:
某个环境对服务默认值提供的替代或补充值；未覆盖的配置继承服务默认值。

**有效配置（Effective Configuration）**:
服务默认值与某个环境覆盖结合后得到的实际配置，不是独立维护的另一份配置层。

**敏感标记（Sensitive Classification）**:
逻辑配置键上要求隐藏其内容的保护分类；删除或替换当前值不会自动取消该分类。敏感容器的保护范围包含其后代。

**脱敏视图（Redacted View）**:
隐藏敏感内容并明确标识隐藏位置的配置展示结果，不是业务程序使用的真实有效配置，也不是访问权限隔离。

**普通配置（Non-sensitive Configuration）**:
被显式指定为允许明文展示的逻辑配置键；若仍受敏感祖先保护，其内容依然隐藏。

**解锁口令（Unlock Passphrase）**:
用于解锁一个 vault 的秘密文本，与其中保存的业务密码或 API key 不同。

**口令更换（Passphrase Change）**:
为当前 vault 更换解锁口令，不改变其中的业务凭据，也不撤销仍由旧口令保护的副本。

**Vault 载荷（Vault Payload）**:
同一个 vault 中共同保存和保护的服务标识、默认配置、环境覆盖与分类信息，不是供查看的脱敏视图。

**解锁凭据文件（Unlock Credential File）**:
保存本项目既定解锁口令的独立可迁移文件，不是 vault 载荷的一部分，也不绑定源主机。

**层级导出（Layered Export）**:
保留服务默认值与各环境覆盖原有结构的查看结果，不展开继承值；它不是有效配置或可恢复备份。

**配置快照（Configuration Snapshot）**:
在某次读取或环境解析时确定的配置视图，不会随之后的编辑、保存或文件更新自动改变。
