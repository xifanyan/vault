# Service Configuration Vault

`servicevault` is a local encrypted configuration vault. The Go library lives at the module root (`servicevault`); the CLI entry point is `cmd/vault`.

## Build

```sh
go build -o vault ./cmd/vault
```

On Windows, use `go build -o vault.exe ./cmd/vault`. Run `./vault --help` on macOS/Linux or `.\vault.exe --help` in PowerShell. Without `--vault FILE`, commands use `$HOME/.vault/default.vault` (the current user's home directory; `USERPROFILE` on Windows). `init` creates the default `.vault` directory when needed. Global options must precede the command; command options must precede positional arguments.

## Getting started

Requirements: Go 1.27 or later. Run these commands from the repository root. They use `./vault`; on Windows, replace it with `.\vault.exe`.

Create a passphrase file and initialize a vault. The generated passphrase is written only to `service.pass` and is not printed:

```sh
./vault --vault service.vault init --service example --generate-passphrase-file service.pass
```

Create a `prod` environment, set its inherited host with the concise `--key`/`--value` form, then read it:

```sh
./vault --vault service.vault --passphrase-file service.pass env create prod
./vault --vault service.vault --passphrase-file service.pass set --defaults --key database.host --value db.internal --classification non-sensitive
./vault --vault service.vault --passphrase-file service.pass get --env prod --key database.host
```

The `get` command returns the selected JSON value directly, without a response wrapper. Keep `service.pass` separate from the vault and restrict access to both files. Values are sensitive by default; mark a value non-sensitive only when exposing it in ordinary reads is intended. See [Commands](#commands) for environment overrides, secret reads, exports, and passphrase changes.

## Unlock passphrase input

Choose at most one current-passphrase source before the command:

```text
--passphrase-file FILE
--passphrase-stdin
--prompt-passphrase
```

If omitted, an interactive terminal prompts with echo disabled. Non-interactive use must select a source; it never falls back to another source. A passphrase is never accepted as a command-line value. Files and standard input follow the library's single-line rule: one final LF or CRLF is removed, other whitespace is preserved, and empty, multi-line, NUL-containing, BOM-prefixed, or invalid UTF-8 values are rejected.

Standard input can carry only one logical input for a command. For example, `set --value-stdin` cannot be combined with `--passphrase-stdin`, and `passwd --new-passphrase-stdin` cannot use `--passphrase-stdin` for the old passphrase in the same invocation. Conflicts are rejected before input is read.

When standard input carries JSON, an interactive passphrase prompt uses the controlling terminal when available, so the two inputs remain separate.

`init` can generate a separate, non-overwriting passphrase file:

```sh
./vault --vault service.vault init --service example --generate-passphrase-file service.pass
```

The generated passphrase is never printed. Otherwise, `init` uses the ordinary current-passphrase source. Default interactive input and `--prompt-passphrase` ask for a new passphrase twice and require matching entries before creating the vault. `--passphrase-file` and `--passphrase-stdin` read once without confirmation; `--generate-passphrase-file` creates a generated credential file instead of prompting. `passwd` accepts `--new-passphrase-file FILE`, `--new-passphrase-stdin`, or `--prompt-new-passphrase`; interactive new passphrases are entered twice for confirmation, while explicit file and standard-input modes are read once.

## Commands

```text
vault [--vault FILE] [passphrase source] init --service ID [--generate-passphrase-file FILE]
vault [--vault FILE] [passphrase source] env list
vault [--vault FILE] [passphrase source] env create NAME
vault [--vault FILE] [passphrase source] env delete NAME
vault [--vault FILE] [passphrase source] get (--defaults | --env NAME) [--include-secrets] [--key KEY | POINTER]
vault [--vault FILE] [passphrase source] set (--defaults | --env NAME) (--value TEXT | --value-file FILE | --value-stdin) [--classification CLASS] (--key KEY | POINTER)
vault [--vault FILE] [passphrase source] delete (--defaults | --env NAME) (--key KEY | POINTER)
vault [--vault FILE] [passphrase source] classify POINTER (sensitive | non-sensitive)
vault [--vault FILE] [passphrase source] export [--output FILE] [--overwrite] [--include-secrets]
vault [--vault FILE] [passphrase source] passwd [--new-passphrase-file FILE | --new-passphrase-stdin | --prompt-new-passphrase]
```

`--defaults` selects service defaults; `--env NAME` selects a named environment. Exactly one is required for `get`, `set`, and `delete`. Environment reads use the effective configuration (defaults recursively merged with the selected environment); writes and deletes affect only that layer. Deleting a missing environment override does not remove an inherited value. Environment names are validated by the library.

`set`, `get`, and `delete` accept `--key database.host` for the nested `host` property inside the `database` object; this never creates a flat property named `database.host`. `get --key database.host` reads the leaf value, while `get --key database` reads the whole object. Dot-separated key segments must be non-empty. `set` and `delete` require exactly one target; `get` accepts at most one and reads the root when omitted. Targets are mutually exclusive: `--key KEY` or a positional JSON Pointer. For property names containing literal dots or empty names, use a JSON Pointer (`/database/host`; `~1` encodes `/`, `~0` encodes `~`). Either `set` target form works with any value source.

`--value` saves only numbers or strings. Text matching a JSON number (including negative, fractional, and exponent forms) is saved as a number without floating-point conversion; other text is saved as a string. `true`, `null`, and object/array-looking text remain strings. Shell quotes do not force a numeric-looking value to be a string: `--value "8443"` still saves a number. Use a JSON string through `--value-file` or `--value-stdin` when a numeric-looking string is required.

With an existing `dev` environment, these PowerShell commands save a string host and a numeric port, then read the leaf value and parent object:

```powershell
.\vault.exe --passphrase-file .\pass set --env dev --key database.host --value "vm-rhauswirth2.otxlab.net"
.\vault.exe --passphrase-file .\pass set --env dev --key database.port --value 8443
.\vault.exe --passphrase-file .\pass get --env dev --key database.host --include-secrets
.\vault.exe --passphrase-file .\pass get --env dev --key database --include-secrets
```

To delete the `dev` override without changing service defaults or sibling properties:

```powershell
.\vault.exe --passphrase-file .\pass delete --env dev --key database.host
```

If service defaults define the same key, subsequent environment reads inherit that value. Deleting a key absent from the selected layer fails even when it exists in defaults. `delete --env dev --key database` removes the entire `database` override object from `dev`, not the defaults.

Value sources are mutually exclusive. `--value-file` and `--value-stdin` retain the existing JSON input behavior, including booleans, null, objects, and arrays. Command-line values may be visible in process arguments and shell history; use files or standard input for sensitive values. Unlock passphrases are still never accepted as command-line values.

New values default to sensitive. `--classification sensitive` or `--classification non-sensitive` explicitly changes the target's global classification; classifications are shared across environments.

`get` writes the selected JSON value itself to standard output, without a `value`/`redacted_paths` response wrapper. With no key or pointer, it writes the configuration object directly; selecting a leaf writes its JSON value, preserving types and numeric precision. Ordinary reads use the library redaction view: hidden values become `null`, and only when values are hidden, a JSON diagnostic containing `redacted_paths` is written to standard error. Those paths are relative to the selected configuration root. A genuine non-sensitive `null` does not produce a redaction diagnostic. Successful redacted reads still exit with status `0`.

`--include-secrets` explicitly requests actual values and produces no redaction diagnostic. Scripts should consume standard output directly instead of extracting `.value`; the previous query response wrapper has been removed. This change does not alter `export`'s layered format.

`export` writes the saved vault's layered JSON (`service`, `data.defaults`, `data.environments`, and `redacted_paths`) without expanding inherited values or exposing classification metadata. Sensitive values are hidden unless `--include-secrets` is present. With no `--output`, export JSON goes to standard output. File output does not overwrite by default; `--overwrite` enables replacement, but neither mode may target the input vault or the passphrase file used to open it. Shell redirection is outside these program-level checks.

Successful queries and exports write only JSON to standard output. Successful mutations write nothing there. Diagnostics go to standard error and do not include configuration values or passphrases. Exit status is `0` for success/help, `2` for malformed command shape or source conflicts, and `1` for operation failures such as invalid data, missing objects, authentication, or file I/O.

