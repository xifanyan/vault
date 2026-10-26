# Strict JSON model, pointers, and classification behavior

Type: task
Status: open
Labels: ready-for-agent
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 01, 02

## Goal

Implement the in-memory data model and configuration semantics in the design specification. Read [spec.md](../spec.md) and tickets [01](01-key-paths-json.md), [02](02-secret-metadata.md), and [07](07-cli-go-contract.md) before changing code.

## Scope

- Strict, precision-preserving JSON parsing: decoded duplicate-key rejection, UTF-8 validation, one JSON value, and depth cap.
- JSON Pointer parsing and object-only path operations, layer mutation, atomic failure, recursive defaults/environment merge, immutable snapshots, classifications, and redacted views.
- Root package model types and behavior covered by the agreed Go contract, without file IO, age encryption, or CLI code.

## Acceptance

- Exercise the agreed public model behavior for root vs `/`, escaped and Unicode keys, exact `9007199254740993`, duplicate decoded keys, null vs missing, array replacement/no element traversal, failed parent creation, environment inheritance, classification persistence, masking at obscured ancestors, and snapshot/byte ownership.
- Add behavior-focused tests before each implementation slice; all tests pass.
- No model mutation occurs for an invalid single operation; redaction does not mutate true config or metadata.

## Resolution

On completion, append an `## Answer` section describing implementation and exercised verification, set `Status: resolved`, and add a context pointer to the map's Decisions so far.
