package vault

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestPayloadEncryptionAndDecryption(t *testing.T) {
	plaintext := bytes.Repeat([]byte("payload"), 10_000)
	ciphertext, err := encryptPayload("correct horse", plaintext)
	if err != nil {
		t.Fatalf("encryptPayload() error = %v", err)
	}

	got, err := decryptPayload("correct horse", ciphertext)
	if err != nil {
		t.Fatalf("decryptPayload() error = %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decryptPayload() returned %d bytes, want %d", len(got), len(plaintext))
	}

	t.Run("wrong passphrase", func(t *testing.T) {
		got, err := decryptPayload("incorrect horse", ciphertext)
		if !errors.Is(err, ErrAuthentication) {
			t.Fatalf("decryptPayload() error = %v, want ErrAuthentication", err)
		}
		if got != nil {
			t.Fatalf("decryptPayload() returned partial plaintext %q", got)
		}
	})

	t.Run("modified final block", func(t *testing.T) {
		modified := bytes.Clone(ciphertext)
		modified[len(modified)-1] ^= 0x01
		got, err := decryptPayload("correct horse", modified)
		if !errors.Is(err, ErrAuthentication) {
			t.Fatalf("decryptPayload() error = %v, want ErrAuthentication", err)
		}
		if got != nil {
			t.Fatalf("decryptPayload() returned partial plaintext %q", got)
		}
	})

	t.Run("truncated final block", func(t *testing.T) {
		truncated := ciphertext[:len(ciphertext)-1]
		got, err := decryptPayload("correct horse", truncated)
		if !errors.Is(err, ErrAuthentication) {
			t.Fatalf("decryptPayload() error = %v, want ErrAuthentication", err)
		}
		if got != nil {
			t.Fatalf("decryptPayload() returned partial plaintext %q", got)
		}
	})
}

func TestPayloadDecryptionRejectsUnsupportedHeaders(t *testing.T) {
	ciphertext, err := encryptPayload("correct horse", []byte("payload"))
	if err != nil {
		t.Fatalf("encryptPayload() error = %v", err)
	}

	tests := []struct {
		name       string
		ciphertext []byte
	}{
		{name: "non-age format", ciphertext: bytes.Replace(bytes.Clone(ciphertext), []byte("age-encryption.org/v1"), []byte("age-encryption.org/v2"), 1)},
		{name: "armored format", ciphertext: append([]byte("-----BEGIN AGE ENCRYPTED FILE-----\n"), ciphertext...)},
		{name: "low scrypt work factor", ciphertext: replaceScryptWorkFactor(t, ciphertext, "17")},
		{name: "high scrypt work factor", ciphertext: replaceScryptWorkFactor(t, ciphertext, "19")},
		{name: "multiple stanzas", ciphertext: duplicateScryptStanza(t, ciphertext)},
		{name: "unsupported stanza", ciphertext: bytes.Replace(bytes.Clone(ciphertext), []byte("-> scrypt "), []byte("-> x25519 "), 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decryptPayload("correct horse", tt.ciphertext)
			if !errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("decryptPayload() error = %v, want ErrInvalidFormat", err)
			}
			if got != nil {
				t.Fatalf("decryptPayload() returned plaintext %q", got)
			}
		})
	}
}

func TestPayloadEncryptionAndDecryptionLimits(t *testing.T) {
	t.Run("plaintext too large to encrypt", func(t *testing.T) {
		_, err := encryptPayload("correct horse", make([]byte, maxPayloadBytes+1))
		if !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("encryptPayload() error = %v, want ErrLimitExceeded", err)
		}
	})

	t.Run("ciphertext too large to decrypt", func(t *testing.T) {
		_, err := decryptPayload("correct horse", make([]byte, maxVaultFileBytes+1))
		if !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("decryptPayload() error = %v, want ErrLimitExceeded", err)
		}
	})

	t.Run("ciphertext too large to encrypt", func(t *testing.T) {
		plaintext := make([]byte, maxPayloadBytes)
		ciphertext, err := encryptPayload("correct horse", plaintext)
		if err != nil {
			t.Fatalf("encryptPayload() error = %v", err)
		}
		if len(ciphertext) > maxVaultFileBytes {
			t.Fatalf("encryptPayload() returned %d bytes, over limit %d", len(ciphertext), maxVaultFileBytes)
		}
	})
}

func replaceScryptWorkFactor(t *testing.T, ciphertext []byte, factor string) []byte {
	t.Helper()
	const marker = "-> scrypt "
	start := bytes.Index(ciphertext, []byte(marker))
	if start < 0 {
		t.Fatal("encrypted fixture has no scrypt stanza")
	}
	end := bytes.IndexByte(ciphertext[start:], '\n')
	if end < 0 {
		t.Fatal("encrypted fixture has unterminated scrypt stanza")
	}
	line := string(ciphertext[start : start+end])
	fields := strings.Split(line, " ")
	if len(fields) != 4 {
		t.Fatalf("unexpected scrypt stanza: %q", line)
	}
	fields[3] = factor
	changed := []byte(strings.Join(fields, " "))
	result := make([]byte, 0, len(ciphertext)+len(changed)-len(line))
	result = append(result, ciphertext[:start]...)
	result = append(result, changed...)
	result = append(result, ciphertext[start+len(line):]...)
	return result
}

func duplicateScryptStanza(t *testing.T, ciphertext []byte) []byte {
	t.Helper()
	start := bytes.Index(ciphertext, []byte("-> scrypt "))
	if start < 0 {
		t.Fatal("encrypted fixture has no scrypt stanza")
	}
	footer := bytes.Index(ciphertext[start:], []byte("\n--- "))
	if footer < 0 {
		t.Fatal("encrypted fixture has no age footer")
	}
	stanza := ciphertext[start : start+footer]
	result := make([]byte, 0, len(ciphertext)+len(stanza)+1)
	result = append(result, ciphertext[:start+footer]...)
	result = append(result, '\n')
	result = append(result, stanza...)
	result = append(result, '\n')
	result = append(result, ciphertext[start+footer+1:]...)
	return result
}
