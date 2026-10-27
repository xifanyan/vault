package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	vaultlib "servicevault"
)

type countingReader struct {
	reads int
}

func (r *countingReader) Read([]byte) (int, error) {
	r.reads++
	return 0, io.EOF
}

func TestInputSourceConflictsFailBeforeReadingStdin(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "set value and passphrase",
			args: []string{"--vault", "vault.bin", "--passphrase-stdin", "set", "--defaults", "--value-stdin", "/token"},
		},
		{
			name: "old and new passphrase",
			args: []string{"--vault", "vault.bin", "--passphrase-stdin", "passwd", "--new-passphrase-stdin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdin := &countingReader{}
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, stdin, &stdout, &stderr); code != 2 {
				t.Fatalf("run() exit = %d, want 2; stderr=%q", code, stderr.String())
			}
			if stdin.reads != 0 {
				t.Fatalf("stdin was read %d times before rejecting conflicting sources", stdin.reads)
			}
			if stdout.Len() != 0 {
				t.Fatalf("shape error wrote to stdout: %q", stdout.String())
			}
		})
	}
}

func TestInvocationShapeErrorsUseExitCodeTwo(t *testing.T) {
	tests := [][]string{
		{"get", "--defaults"},
		{"--vault", "vault.bin", "get", "--defaults"},
		{"--vault", "vault.bin", "get", "--defaults", "/token", "--include-secrets"},
		{"--vault", "vault.bin", "env", "create"},
		{"--vault", "vault.bin", "--passphrase-stdin", "set", "--defaults", "--value-file", "value.json"},
		{"--vault", "vault.bin", "--passphrase-file", "--passphrase-stdin", "get", "--defaults"},
		{"--vault", "vault.bin", "--passphrase-stdin", "get", "--env", "--defaults", "/token"},
		{"--vault", "vault.bin", "--passphrase-stdin", "delete", "--defaults"},
	}
	for _, options := range [][]string{
		{"--key", "", "--value", "text"},
		{"--key", ".host", "--value", "text"},
		{"--key", "database.", "--value", "text"},
		{"--key", "database..host", "--value", "text"},
		{"--key", "database.host", "--value", "text", "/other"},
		{"--key", "database.host", "--key", "other", "--value", "text"},
		{"--key", "database.host", "--value", "first", "--value", "second"},
		{"--key", "database.host", "--value", "text", "--value-file", "value.json"},
		{"--key", "database.host", "--value-file", "value.json", "--value", "text"},
		{"--key", "database.host", "--value", "text", "--value-stdin"},
		{"--key", "database.host", "--value-stdin", "--value", "text"},
		{"--key", "database.host", "--value"},
		{"--key", "database.host"},
		{"--value", "text"},
		{"--value", "text", "--key"},
		{"--key", "--value", "text"},
		{"--value", "--key", "database.host"},
	} {
		args := []string{"--vault", "vault.bin", "--passphrase-stdin", "set", "--defaults"}
		tests = append(tests, append(args, options...))
	}
	for _, options := range [][]string{
		{"--key"},
		{"--key", ""},
		{"--key", ".host"},
		{"--key", "database."},
		{"--key", "database..host"},
		{"--key", "database.host", "/other"},
		{"--key", "database.host", "--key", "other"},
		{"--key", "--include-secrets"},
	} {
		for _, command := range []string{"get", "delete"} {
			args := []string{"--vault", "vault.bin", "--passphrase-stdin", command, "--defaults"}
			tests = append(tests, append(args, options...))
		}
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdin := &countingReader{}
			var stdout, stderr bytes.Buffer
			if code := run(args, stdin, &stdout, &stderr); code != 2 {
				t.Fatalf("run() exit = %d, want 2; stderr=%q", code, stderr.String())
			}
			if stdin.reads != 0 || stdout.Len() != 0 {
				t.Fatalf("shape error consumed stdin (%d reads) or wrote stdout %q", stdin.reads, stdout.String())
			}
		})
	}
}

func TestGetEmitsOnlyJSONAndUsesLibraryRedaction(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	args := []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "get", "--defaults", "--key", "token"}
	var stdout, stderr bytes.Buffer
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("redacted get exit = %d; stderr=%q", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("get stdout is not pure JSON: %q", stdout.String())
	}
	if strings.TrimSpace(stdout.String()) != "null" {
		t.Fatalf("redacted stdout = %q, want JSON null", stdout.String())
	}
	var diagnostic struct {
		RedactedPaths []string `json:"redacted_paths"`
	}
	if err := json.Unmarshal(stderr.Bytes(), &diagnostic); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(diagnostic.RedactedPaths, []string{"/token"}) {
		t.Fatalf("redacted paths = %v, want [/token]", diagnostic.RedactedPaths)
	}
	if strings.Contains(stdout.String(), "never-print-this") || strings.Contains(stderr.String(), "never-print-this") {
		t.Fatalf("redacted get leaked secret: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	args = []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "get", "--defaults", "--include-secrets", "/token"}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("plain get exit = %d; stderr=%q", code, stderr.String())
	}
	var value string
	if err := json.Unmarshal(stdout.Bytes(), &value); err != nil {
		t.Fatalf("explicit get did not emit a JSON string: %q; error=%v", stdout.String(), err)
	}
	if value != "never-print-this" || stderr.Len() != 0 {
		t.Fatalf("explicit get output = %q, stderr=%q", value, stderr.String())
	}
}

func TestExportProtectsExistingAndInputFiles(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	originalVault, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	originalCredentials, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}

	existing := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		path string
	}{
		{name: "existing output", path: existing},
		{name: "vault alias", path: vaultPath},
		{name: "credential alias", path: credentialsPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "export", "--output", tt.path, "--overwrite"}
			if tt.name == "existing output" {
				args = args[:len(args)-1]
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 1 {
				t.Fatalf("export exit = %d, want 1; stderr=%q", code, stderr.String())
			}
			if stdout.Len() != 0 || strings.Contains(stderr.String(), "never-print-this") {
				t.Fatalf("failed export leaked output: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
	if after, err := os.ReadFile(vaultPath); err != nil || !bytes.Equal(after, originalVault) {
		t.Fatalf("vault input changed after rejected export: err=%v", err)
	}
	if after, err := os.ReadFile(credentialsPath); err != nil || !bytes.Equal(after, originalCredentials) {
		t.Fatalf("credential input changed after rejected export: err=%v", err)
	}
	if after, err := os.ReadFile(existing); err != nil || string(after) != "keep" {
		t.Fatalf("existing export target changed: contents=%q err=%v", after, err)
	}
}

func createTestVault(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "service.vault")
	credentialsPath := filepath.Join(dir, "service.pass")
	v, err := vaultlib.CreateWithPassphrase(vaultPath, "example-service", "test-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/token", json.RawMessage(`"never-print-this"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentialsPath, []byte("test-passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return vaultPath, credentialsPath
}

func TestInitSourceConflictDoesNotCreateFiles(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "service.vault")
	credentialsPath := filepath.Join(dir, "service.pass")
	stdin := &countingReader{}
	var stdout, stderr bytes.Buffer
	args := []string{"--vault", vaultPath, "--passphrase-stdin", "init", "--service", "example", "--generate-passphrase-file", credentialsPath}
	if code := run(args, stdin, &stdout, &stderr); code != 2 {
		t.Fatalf("conflicting init exit = %d, want 2; stderr=%q", code, stderr.String())
	}
	if stdin.reads != 0 || stdout.Len() != 0 {
		t.Fatalf("conflicting init consumed stdin or wrote stdout: reads=%d stdout=%q", stdin.reads, stdout.String())
	}
	for _, path := range []string{vaultPath, credentialsPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("conflicting init created %q: stat error=%v", path, err)
		}
	}
}

func TestInitConfirmsInteractivePassphraseBeforeCreatingVault(t *testing.T) {
	tests := []struct {
		name       string
		responses  []string
		wantErr    bool
		wantCreate bool
	}{
		{name: "matching entries", responses: []string{"secret", "secret"}, wantCreate: true},
		{name: "mismatch", responses: []string{"first", "second"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			vaultPath := filepath.Join(dir, "service.vault")
			prompts := []string{}
			responseIndex := 0
			prompt := func(_ io.Reader, _ io.Writer, text string, _ bool) (string, error) {
				prompts = append(prompts, text)
				response := tt.responses[responseIndex]
				responseIndex++
				return response, nil
			}

			inv := invocation{
				command:    "init",
				vaultPath:  vaultPath,
				passphrase: inputSource{kind: sourcePrompt},
				service:    "example",
			}
			err := executeInitWithPrompt(inv, strings.NewReader("unused"), io.Discard, false, prompt)
			if (err != nil) != tt.wantErr {
				t.Fatalf("executeInitWithPrompt() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(prompts, []string{"New passphrase: ", "Confirm new passphrase: "}) {
				t.Fatalf("prompts = %q, want two matching-entry prompts", prompts)
			}
			if tt.wantCreate {
				if _, err := vaultlib.OpenWithPassphrase(vaultPath, "secret"); err != nil {
					t.Fatalf("matching init created an unusable vault: %v", err)
				}
			} else if _, err := os.Stat(vaultPath); !os.IsNotExist(err) {
				t.Fatalf("mismatched init created vault: stat error = %v", err)
			}
		})
	}
}

func TestInitAndCommandsUseDefaultVaultPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	credentialsPath := filepath.Join(home, "service.pass")
	if err := os.WriteFile(credentialsPath, []byte("test-passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	args := []string{"--passphrase-file", credentialsPath, "init", "--service", "example"}
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("default init exit = %d; stderr=%q", code, stderr.String())
	}
	defaultVaultPath := filepath.Join(home, ".vault", "default.vault")
	if _, err := vaultlib.Open(defaultVaultPath, credentialsPath); err != nil {
		t.Fatalf("default init did not create an openable vault at %q: %v", defaultVaultPath, err)
	}

	stdout.Reset()
	stderr.Reset()
	args = []string{"--passphrase-file", credentialsPath, "env", "list"}
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("default env list exit = %d; stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" || stderr.Len() != 0 {
		t.Fatalf("default env list output = %q, stderr=%q; want empty JSON array", stdout.String(), stderr.String())
	}
}

func TestHelpDoesNotRequireVault(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"env", "list", "--help"}} {
		stdin := &countingReader{}
		var stdout, stderr bytes.Buffer
		if code := run(args, stdin, &stdout, &stderr); code != 0 {
			t.Fatalf("help exit = %d, want 0; args=%v stderr=%q", code, args, stderr.String())
		}
		if stdin.reads != 0 || !strings.Contains(stdout.String(), "Commands:") || stderr.Len() != 0 {
			t.Fatalf("help output/input contract violated: args=%v reads=%d stdout=%q stderr=%q", args, stdin.reads, stdout.String(), stderr.String())
		}
	}
}

func TestExportStdoutIsLayeredRedactedJSON(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	args := []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "export"}
	var stdout, stderr bytes.Buffer
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("export exit = %d; stderr=%q", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("export stdout is not pure JSON: %q", stdout.String())
	}
	var result struct {
		Service string `json:"service"`
		Data    struct {
			Defaults     map[string]json.RawMessage `json:"defaults"`
			Environments map[string]json.RawMessage `json:"environments"`
		} `json:"data"`
		RedactedPaths []string `json:"redacted_paths"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Service != "example-service" || string(result.Data.Defaults["token"]) != "null" || !reflect.DeepEqual(result.RedactedPaths, []string{"/defaults/token"}) {
		t.Fatalf("export did not preserve the layered redacted view: service=%q defaults=%v paths=%v", result.Service, result.Data.Defaults, result.RedactedPaths)
	}
	if result.Data.Environments == nil || stderr.Len() != 0 || strings.Contains(stdout.String(), "never-print-this") {
		t.Fatalf("export output contract violated: environments=%v stdout=%q stderr=%q", result.Data.Environments, stdout.String(), stderr.String())
	}
}

func TestSetAndGetDottedKeysPreserveHierarchyTypesAndSensitivity(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	runCommand := func(options ...string) (string, string) {
		t.Helper()
		args := append([]string{"--vault", vaultPath, "--passphrase-file", credentialsPath}, options...)
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
			t.Fatalf("command %v exit = %d; stderr=%q", options, code, stderr.String())
		}
		return stdout.String(), stderr.String()
	}
	if output, diagnostic := runCommand("env", "create", "dev"); output != "" || diagnostic != "" {
		t.Fatalf("env create output: stdout=%q stderr=%q", output, diagnostic)
	}
	for _, options := range [][]string{
		{"--key", "database.host", "--value", "vm-rhauswirth2.otxlab.net"},
		{"--value", "8443", "--key", "database.port"},
		{"--key", "database.zero", "--value", "0"},
		{"--key", "database.offset", "--value", "-1.25e-3"},
		{"--key", "database.large", "--value", "9007199254740993"},
		{"--key", "database.code", "--value", "00123"},
		{"--key", "database.enabled", "--value", "true"},
		{"--key", "database.options", "--value", `{"nested":null}`},
		{"--key", "database.empty", "--value", ""},
		{"--key", "database.null", "--value", "null"},
		{"--key", "names.a/b.~name", "--value", "quoted \"text\" \\path\n中文"},
		{"--value", "literal dot", "/database.host"},
	} {
		if output, diagnostic := runCommand(append([]string{"set", "--env", "dev"}, options...)...); output != "" || diagnostic != "" {
			t.Fatalf("set output: stdout=%q stderr=%q", output, diagnostic)
		}
	}

	v, err := vaultlib.Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	layer, err := v.Environment("dev")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := layer.Get("")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"database": map[string]any{
			"host": "vm-rhauswirth2.otxlab.net", "port": json.Number("8443"), "enabled": "true",
			"zero": json.Number("0"), "offset": json.Number("-1.25e-3"),
			"large": json.Number("9007199254740993"), "code": "00123",
			"options": `{"nested":null}`, "empty": "", "null": "null",
		},
		"names":         map[string]any{"a/b": map[string]any{"~name": "quoted \"text\" \\path\n中文"}},
		"database.host": "literal dot",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved dev configuration = %#v, want %#v", got, want)
	}
	if _, err := v.Defaults().Get("/database/host"); err != vaultlib.ErrNotFound {
		t.Fatalf("setting dev modified defaults: error=%v", err)
	}
	output, diagnostic := runCommand("get", "--env", "dev", "--key", "database.host")
	var redacted struct {
		RedactedPaths []string `json:"redacted_paths"`
	}
	if err := json.Unmarshal([]byte(diagnostic), &redacted); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != "null" || !reflect.DeepEqual(redacted.RedactedPaths, []string{"/database/host"}) {
		t.Fatalf("literal string was not sensitive by default: stdout=%s stderr=%s", output, diagnostic)
	}
	want["token"] = "never-print-this"
	for _, tt := range []struct {
		key  string
		want any
	}{
		{key: "database.host", want: "vm-rhauswirth2.otxlab.net"},
		{key: "database", want: want["database"]},
		{key: "database.large", want: json.Number("9007199254740993")},
		{key: "names.a/b.~name", want: "quoted \"text\" \\path\n中文"},
		{key: "", want: want},
	} {
		options := []string{"get", "--env", "dev", "--include-secrets"}
		if tt.key != "" {
			options = append(options, "--key", tt.key)
		}
		output, diagnostic := runCommand(options...)
		var value any
		decoder := json.NewDecoder(strings.NewReader(output))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(value, tt.want) || diagnostic != "" {
			t.Fatalf("get --key %q = %s, stderr=%s; want value %#v without redactions", tt.key, output, diagnostic, tt.want)
		}
	}
}

func TestDeleteDottedKeyPreservesDefaultsAndSiblingValues(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	v, err := vaultlib.Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/database", json.RawMessage(`{"host":"default.internal","port":8443}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("dev"); err != nil {
		t.Fatal(err)
	}
	dev, err := v.Environment("dev")
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Set("/database", json.RawMessage(`{"host":"dev.internal","user":"dev-user"}`)); err != nil {
		t.Fatal(err)
	}
	if err := dev.Set("/database.host", json.RawMessage(`"literal-dot"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	deleteKey := func(wantCode int, options ...string) {
		t.Helper()
		args := append([]string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "delete"}, options...)
		var stdout, stderr bytes.Buffer
		if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != wantCode {
			t.Fatalf("delete %v exit = %d, want %d; stderr=%q", options, code, wantCode, stderr.String())
		}
		if stdout.Len() != 0 || wantCode == 0 && stderr.Len() != 0 {
			t.Fatalf("delete output contract violated: stdout=%q stderr=%q", stdout.String(), stderr.String())
		}
	}
	deleteKey(0, "--env", "dev", "--key", "database.host")
	v, err = vaultlib.Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	dev, err = v.Environment("dev")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := dev.Get("")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"database":      map[string]any{"user": "dev-user"},
		"database.host": "literal-dot",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dev after deleting nested host = %#v, want %#v", got, want)
	}
	config, err := v.Resolve("dev")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = config.Get("/database/host")
	if err != nil || string(raw) != `"default.internal"` {
		t.Fatalf("effective host after deleting override = %s, error=%v; want inherited default", raw, err)
	}
	before, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	deleteKey(1, "--key", "database.host", "--env", "dev")
	after, err := os.ReadFile(vaultPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("deleting an inherited-only value changed the vault: error=%v", err)
	}
}

func TestGetPublicNullDoesNotReportRedaction(t *testing.T) {
	vaultPath, credentialsPath := createTestVault(t)
	v, err := vaultlib.Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/nullable", json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/nullable", vaultlib.NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	args := []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "get", "--defaults", "--key", "nullable"}
	var stdout, stderr bytes.Buffer
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("public null get exit = %d; stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "null" || stderr.Len() != 0 {
		t.Fatalf("public null output = %q, stderr=%q; want raw JSON null without redaction diagnostics", stdout.String(), stderr.String())
	}
}
