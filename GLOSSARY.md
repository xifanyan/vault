# Service Configuration Vault

This project defines a portable vault for managing service configuration.

## Language

**Vault**:
A collection of hierarchical service configuration values, including sensitive values.

**Service Defaults**:
Base configuration shared across all environments for a service. A distinct configuration scope, not an environment named `default`.

**Environment**:
A named configuration context for a service deployment, such as development or production. An environment does not provide access-control isolation.

**Environment Override**:
A value that replaces or supplements a service default in one environment. Values not overridden are inherited from service defaults.

**Effective Configuration**:
The result of combining service defaults with an environment's overrides. It is not a separately maintained configuration layer.

**Sensitive Classification**:
A protection classification on a logical configuration key that requires its content to be hidden. Deleting or replacing the current value does not remove the classification, and a sensitive container protects its descendants.

**Redacted View**:
A configuration view that hides sensitive content and identifies where content was hidden. It is not the true effective configuration used by applications or an access-control mechanism.

**Non-sensitive Configuration**:
A logical configuration key explicitly allowed to appear in plaintext. Its content remains hidden while protected by a sensitive ancestor.

**Unlock Passphrase**:
Secret text used to unlock a vault, distinct from business passwords or API keys stored inside it.

**Passphrase Change**:
Changing the passphrase protecting the current vault. It neither changes stored business credentials nor revokes copies still protected by the old passphrase.

**Vault Payload**:
The service identifier, service defaults, environment overrides, and classification data stored and protected together in a vault. It is not a redacted view.

**Unlock Credential File**:
A separate, portable file containing the vault's unlock passphrase. It is not part of the vault payload or bound to the source machine.

**Layered Export**:
A view that preserves the structure of service defaults and environment overrides without resolving inheritance. It is neither the effective configuration nor a restorable backup.

**Configuration Snapshot**:
The configuration view resolved at a particular read; later edits, saves, or file changes do not update it.
