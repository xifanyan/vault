package vault

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePassphrase(t *testing.T) {
	for _, passphrase := range []string{"plain", " with spaces ", "雪だるま🔐"} {
		if err := validatePassphrase(passphrase); err != nil {
			t.Errorf("validatePassphrase(%q) error = %v", passphrase, err)
		}
	}

	for _, passphrase := range []string{"", "line\nbreak", "carriage\rreturn", "both\r\n", "nul\x00byte", "\xef\xbb\xbfsecret", string([]byte{0xff})} {
		t.Run("invalid", func(t *testing.T) {
			if err := validatePassphrase(passphrase); !errors.Is(err, ErrInvalidArgument) {
				t.Errorf("validatePassphrase(%q) error = %v, want ErrInvalidArgument", passphrase, err)
			}
		})
	}
}

func TestReadPassphrase(t *testing.T) {
	for input, want := range map[string]string{
		"secret":        "secret",
		"secret\n":      "secret",
		"secret\r\n":    "secret",
		" secret \r\n":  " secret ",
		"雪だるま\n":     "雪だるま",
	} {
		got, err := readPassphrase(strings.NewReader(input))
		if err != nil || got != want {
			t.Errorf("readPassphrase(%q) = %q, %v; want %q, nil", input, got, err, want)
		}
	}

	for _, input := range []string{"", "\n", "\r\n", "secret\n\n", "secret\r\n\r\n", "first\nsecond", "secret\n\r\n", "\xef\xbb\xbfsecret", "nul\x00byte", string([]byte{0xff})} {
		t.Run("invalid", func(t *testing.T) {
			if got, err := readPassphrase(strings.NewReader(input)); !errors.Is(err, ErrInvalidFormat) || got != "" {
				t.Errorf("readPassphrase(%q) = %q, %v; want empty result and ErrInvalidFormat", input, got, err)
			}
		})
	}
}

func TestReadPassphraseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte(" secret \r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPassphraseFile(path)
	if err != nil || got != " secret " {
		t.Fatalf("readPassphraseFile() = %q, %v; want %q, nil", got, err, " secret ")
	}

	if err := os.WriteFile(path, []byte("secret\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readPassphraseFile(path); !errors.Is(err, ErrInvalidFormat) || got != "" {
		t.Errorf("readPassphraseFile(multiple lines) = %q, %v; want empty result and ErrInvalidFormat", got, err)
	}
}

func TestGenerateCredentialsCreatesRandomBase64URLFileOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := generateCredentials(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "\n") || strings.ContainsRune(string(got[:len(got)-1]), '\n') {
		t.Fatalf("generated credentials are not one LF-terminated line: %q", got)
	}
	encoded := string(got[:len(got)-1])
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("generated credentials decode to %d bytes with error %v; want 32 bytes", len(decoded), err)
	}

	if err := generateCredentials(path); !errors.Is(err, ErrAlreadyExists) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second generateCredentials() error = %v; want ErrAlreadyExists and fs.ErrExist", err)
	}
	gotAgain, err := os.ReadFile(path)
	if err != nil || string(gotAgain) != string(got) {
		t.Fatalf("existing credentials changed: read error %v", err)
	}
}

func TestReadVaultFileEnforcesSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault")
	within := make([]byte, maxVaultFileBytes)
	if err := os.WriteFile(path, within, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readVaultFile(path)
	if err != nil || len(got) != maxVaultFileBytes {
		t.Fatalf("readVaultFile(limit) returned %d bytes, %v; want %d bytes, nil", len(got), err, maxVaultFileBytes)
	}

	if err := os.WriteFile(path, append(within, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readVaultFile(path); !errors.Is(err, ErrLimitExceeded) || got != nil {
		t.Errorf("readVaultFile(over limit) returned %d bytes, %v; want nil and ErrLimitExceeded", len(got), err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("vault file remained open after readVaultFile: %v", err)
	}
}


func TestWriteVaultFileReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault")
	if err := os.WriteFile(path, []byte("old ciphertext"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeVaultFile(path, []byte("new ciphertext")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new ciphertext" {
		t.Fatalf("replaced vault = %q, %v; want new ciphertext, nil", got, err)
	}
}

func TestWriteExportFileOverwritePolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if err := writeExportFile(path, []byte("first"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeExportFile(path, []byte("refused"), false); !errors.Is(err, ErrAlreadyExists) || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second write without overwrite error = %v; want ErrAlreadyExists and fs.ErrExist", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "first" {
		t.Fatalf("refused overwrite changed export to %q, %v", got, err)
	}
	if err := writeExportFile(path, []byte("replacement"), true); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil || string(got) != "replacement" {
		t.Fatalf("overwrite result = %q, %v; want replacement, nil", got, err)
	}
}

func TestWriteExportFileRejectsInputAliases(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "vault")
	if err := os.WriteFile(input, []byte("encrypted input"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeExportFile(input, []byte("export"), true, input); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("same-path export error = %v; want ErrInvalidArgument", err)
	}

	alias := filepath.Join(dir, "vault-alias")
	if err := os.Link(input, alias); err != nil {
		t.Skipf("hard links are unavailable: %v", err)
	}
	if err := writeExportFile(alias, []byte("export"), true, input); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("hard-link alias export error = %v; want ErrInvalidArgument", err)
	}
	got, err := os.ReadFile(input)
	if err != nil || string(got) != "encrypted input" {
		t.Fatalf("alias rejection changed input to %q, %v", got, err)
	}
}
