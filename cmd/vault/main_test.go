package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
	args := []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "get", "--defaults", "/token"}
	var stdout, stderr bytes.Buffer
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("redacted get exit = %d; stderr=%q", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) {
		t.Fatalf("get stdout is not pure JSON: %q", stdout.String())
	}
	var response struct {
		Value         json.RawMessage `json:"value"`
		RedactedPaths []string        `json:"redacted_paths"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if string(response.Value) != "null" || !reflect.DeepEqual(response.RedactedPaths, []string{"/token"}) {
		t.Fatalf("redacted result = (%s, %v), want (null, [/token])", response.Value, response.RedactedPaths)
	}
	if stderr.Len() != 0 || strings.Contains(stdout.String(), "never-print-this") {
		t.Fatalf("redacted get leaked diagnostics or secret: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	args = []string{"--vault", vaultPath, "--passphrase-file", credentialsPath, "get", "--defaults", "--include-secrets", "/token"}
	stdout.Reset()
	stderr.Reset()
	if code := run(args, strings.NewReader("unused"), &stdout, &stderr); code != 0 {
		t.Fatalf("plain get exit = %d; stderr=%q", code, stderr.String())
	}
	if !json.Valid(bytes.TrimSpace(stdout.Bytes())) || !strings.Contains(stdout.String(), `"never-print-this"`) {
		t.Fatalf("explicit get did not emit valid plaintext JSON value: %q", stdout.String())
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
			Defaults map[string]json.RawMessage `json:"defaults"`
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
