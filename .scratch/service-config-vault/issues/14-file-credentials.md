# Credential adapters and safe file operations

Type: task
Status: open
Labels: ready-for-agent
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 03, 05

## Goal

Implement shared passphrase/file adapters and the basic file-writing boundary. Read [spec.md](../spec.md), [ticket 03](03-passphrase-input.md), and [ticket 05](05-file-operations.md).

## Scope

- Validate direct passphrases; read files/stdin with shared UTF-8, BOM, NUL, empty, single-line and exactly-one-terminal-LF/CRLF rules.
- Generate 32 cryptographic random bytes as unpadded base64url plus LF, create-only.
- Bounded vault file reads, same-directory encrypted temporary output, write/sync/close checks, and platform replacement without in-place truncation; ordinary errors remain inspectable.
- Export destination checks: refuse overwrite unless explicit and reject aliases of input vault/credential files.

## Acceptance

- Behavior tests cover credential edge cases, generation format/no overwrite, bounded reads, replacement of existing vaults on supported host semantics, output overwrite policy, and identical-file/hard-link alias rejection.
- Do not introduce locks, install workflows, ACL repair, retries, or transaction claims.
- Tests pass.

## Resolution

On completion, append an `## Answer` section describing implementation and exercised verification, set `Status: resolved`, and add a context pointer to the map's Decisions so far.
