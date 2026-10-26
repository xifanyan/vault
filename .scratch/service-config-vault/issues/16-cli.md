# Vault CLI commands and output contract

Type: task
Status: claimed
Labels: ready-for-agent
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 15

## Goal

Implement the documented `vault` CLI using the public Go package as the single source of model, credential, encryption, and redaction behavior. Read [spec.md](../spec.md) and [ticket 07](07-cli-go-contract.md), especially CLI syntax and exit behavior.

## Scope

Implement global option parsing, `init`, `env list/create/delete`, `get`, `set`, `delete`, `classify`, `export`, and `passwd`; hidden terminal prompt where requested; explicit file/stdin passphrase/value sources; pure JSON output and stderr diagnostics; exit codes 0/1/2.

## Acceptance

- Smoke-test real command sequences for initialization, env creation, set/get inheritance, classification/redaction, export, password rotation, and reopen after copying vault plus credentials to different paths.
- Source conflicts and malformed invocation fail before consuming stdin or modifying files; failures do not print secrets.
- Export default no-overwrite and same-input-file protections hold; tests pass.

## Resolution

On completion, append an `## Answer` section describing implementation and exercised verification, set `Status: resolved`, and add a context pointer to the map's Decisions so far.
