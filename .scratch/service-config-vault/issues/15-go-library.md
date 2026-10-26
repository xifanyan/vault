# Public Go library lifecycle and persistence

Type: task
Status: claimed
Labels: ready-for-agent
Parent: [本地服务配置 Vault：实现就绪规格路线图](../map.md)
Blocked by: 12, 13, 14

## Goal

Implement the documented `vault` Go package API over the merged model, age format, and file/credential helpers. Read [spec.md](../spec.md) and [ticket 07](07-cli-go-contract.md).

## Scope

Implement `GenerateCredentials`, `Create`, `Open`, `Vault`/`Layer` environment management and edits, `Resolve`, classifications, explicit `Save`/`SaveWithCredentials`, and `ExportJSON` with specified error identities and snapshot ownership. Keep file handles closed before authenticated parsing. Add only the narrow public view/passphrase helpers needed by the CLI so both surfaces reuse core rules.

## Acceptance

- Public-API behavior tests cover create/open/save, edits remain in-memory until save, one explicit save, credential rotation and old-copy semantics, exact JSON ownership, and export of saved layers without inheritance or internal classifications.
- Error identities are checkable with `errors.Is`; invalid single operations do not mutate the model.
- Tests pass; no background reload, prompt, or implicit save.

## Resolution

On completion, append an `## Answer` section describing implementation and exercised verification, set `Status: resolved`, and add a context pointer to the map's Decisions so far.
