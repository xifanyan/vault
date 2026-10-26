# Service Configuration Vault

`servicevault` is a local encrypted configuration vault. The Go library lives at the module root (`servicevault`); the CLI entry point is `cmd/vault`.

## Build

```sh
go build -o vault ./cmd/vault
```

Run `vault --help` for the command summary. Every command except help requires an explicit vault path. Global options must precede the command; command options must precede positional arguments.

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
vault --vault service.vault init --service example --generate-passphrase-file service.pass
```

The generated passphrase is never printed. Otherwise, `init` uses the ordinary current-passphrase source. `passwd` accepts `--new-passphrase-file FILE`, `--new-passphrase-stdin`, or `--prompt-new-passphrase`; interactive new passphrases are entered twice for confirmation. File and standard-input modes do not add a confirmation prompt.

## Commands

```text
vault --vault FILE [passphrase source] init --service ID [--generate-passphrase-file FILE]
vault --vault FILE [passphrase source] env list
vault --vault FILE [passphrase source] env create NAME
vault --vault FILE [passphrase source] env delete NAME
vault --vault FILE [passphrase source] get (--defaults | --env NAME) [--include-secrets] [POINTER]
vault --vault FILE [passphrase source] set (--defaults | --env NAME) (--value-file FILE | --value-stdin) [--classification CLASS] POINTER
vault --vault FILE [passphrase source] delete (--defaults | --env NAME) POINTER
vault --vault FILE [passphrase source] classify POINTER (sensitive | non-sensitive)
vault --vault FILE [passphrase source] export [--output FILE] [--overwrite] [--include-secrets]
vault --vault FILE [passphrase source] passwd [--new-passphrase-file FILE | --new-passphrase-stdin | --prompt-new-passphrase]
```

`--defaults` selects service defaults; `--env NAME` selects a named environment. Exactly one is required for `get`, `set`, and `delete`. Environment reads use the effective configuration (defaults recursively merged with the selected environment); writes and deletes affect only that layer. Deleting a missing environment override does not remove an inherited value. Environment names are validated by the library.

Configuration values are JSON documents supplied by `--value-file` or `--value-stdin`, never command-line arguments. Pointers use JSON Pointer syntax (`/database/host`; `~1` encodes `/`, `~0` encodes `~`). New values default to sensitive. `--classification sensitive` or `--classification non-sensitive` explicitly changes the target's global classification; classifications are shared across environments.

`get` writes one JSON object with exactly `value` and `redacted_paths`. Ordinary reads use the library redaction view; redacted values are `null`, and paths are relative to the selected configuration root. `--include-secrets` is an explicit request for actual values and emits an empty `redacted_paths` list.

`export` writes the saved vault's layered JSON (`service`, `data.defaults`, `data.environments`, and `redacted_paths`) without expanding inherited values or exposing classification metadata. Sensitive values are hidden unless `--include-secrets` is present. With no `--output`, export JSON goes to standard output. File output does not overwrite by default; `--overwrite` enables replacement, but neither mode may target the input vault or the passphrase file used to open it. Shell redirection is outside these program-level checks.

Successful queries and exports write only JSON to standard output. Successful mutations write nothing there. Diagnostics go to standard error and do not include configuration values or passphrases. Exit status is `0` for success/help, `2` for malformed command shape or source conflicts, and `1` for operation failures such as invalid data, missing objects, authentication, or file I/O.

## Example

Place a JSON string such as `"db.internal"` in `host.json`:

```sh
vault --vault service.vault init --service example --generate-passphrase-file service.pass
vault --vault service.vault --passphrase-file service.pass env create prod
vault --vault service.vault --passphrase-file service.pass set --defaults --value-file host.json --classification non-sensitive /database/host
vault --vault service.vault --passphrase-file service.pass get --env prod /database/host
vault --vault service.vault --passphrase-file service.pass export --output view.json
```

The vault and passphrase file are independent portable files. Protect and copy both as appropriate; a JSON export is a viewing format, not a backup or an importable vault.
