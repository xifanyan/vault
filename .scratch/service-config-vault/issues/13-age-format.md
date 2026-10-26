# age v1 encryption and strict payload format

Type: task
Status: open
Labels: ready-for-agent
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 04, 09

## Goal

Implement the approved `filippo.io/age v1.3.2` binary scrypt envelope and bounded, fully authenticated payload handling. Read [spec.md](../spec.md), [ticket 04](04-encrypted-file-format.md), and [research](../research/existing-encryption-format.md).

## Scope

- Encrypt/decrypt opaque payload bytes with age's scrypt recipient/identity at exactly `logN=18`.
- Before KDF, accept only one binary age v1 scrypt stanza at exactly work factor 18; reject alternate stanzas, formats, and costs.
- Enforce actual ciphertext bytes <=20 MiB, plaintext <=16 MiB, and deliver no plaintext until age reader reaches authenticated EOF.
- Define the root package payload JSON encoding/decoding contract with ticket 12 (`marshalPayload` / `unmarshalPayload`); coordinate through shared context, not duplicate model semantics.

## Acceptance

- Behavior tests cover roundtrip, wrong password, modified and truncated authenticated payload, unsupported/multiple stanza and low/high cost rejection before derivation, and input/output limits.
- Tests demonstrate no partial payload is returned on authentication failure.
- No bespoke cryptographic primitives or alternate envelope; all tests pass.

## Resolution

On completion, append an `## Answer` section describing implementation and exercised verification, set `Status: resolved`, and add a context pointer to the map's Decisions so far.
