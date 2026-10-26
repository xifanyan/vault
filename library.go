package vault

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"unicode/utf8"
)

// GenerateCredentials creates a new randomly generated passphrase file.
func GenerateCredentials(path string) error {
	return generateCredentials(path)
}

// Create creates and saves an empty vault using a passphrase read from
// credentialsPath.
func Create(vaultPath, credentialsPath, service string) (*Vault, error) {
	passphrase, err := readPassphraseFile(credentialsPath)
	if err != nil {
		return nil, err
	}
	return CreateWithPassphrase(vaultPath, service, passphrase)
}

// CreateWithPassphrase creates and saves an empty vault using passphrase
// directly. It is intended for callers that have already adapted an explicit
// passphrase source.
func CreateWithPassphrase(vaultPath, service, passphrase string) (*Vault, error) {
	if vaultPath == "" || service == "" || !utf8.ValidString(service) {
		return nil, fmt.Errorf("%w: invalid vault path or service", ErrInvalidArgument)
	}
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	v := newVault(service)
	plaintext, err := marshalPayload(payloadFromVault(v))
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	ciphertext, err := encryptPayload(passphrase, plaintext)
	if err != nil {
		return nil, err
	}
	if err := createVaultFile(vaultPath, ciphertext); err != nil {
		return nil, err
	}
	v.vaultPath = vaultPath
	v.passphrase = passphrase
	return v, nil
}

// Open reads, authenticates, and decodes a vault using a passphrase read from
// credentialsPath. The vault file is closed before authentication begins.
func Open(vaultPath, credentialsPath string) (*Vault, error) {
	passphrase, err := readPassphraseFile(credentialsPath)
	if err != nil {
		return nil, err
	}
	return OpenWithPassphrase(vaultPath, passphrase)
}

// OpenWithPassphrase opens a vault using passphrase directly. It is intended
// for callers that have already adapted an explicit passphrase source.
func OpenWithPassphrase(vaultPath, passphrase string) (*Vault, error) {
	if vaultPath == "" {
		return nil, fmt.Errorf("%w: empty vault path", ErrInvalidArgument)
	}
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	ciphertext, err := readVaultFile(vaultPath)
	if err != nil {
		return nil, err
	}
	plaintext, err := decryptPayload(passphrase, ciphertext)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	p, err := unmarshalPayload(plaintext)
	if err != nil {
		return nil, err
	}
	v := vaultFromPayload(p)
	v.vaultPath = vaultPath
	v.passphrase = passphrase
	return v, nil
}

// Save validates and persists the current in-memory vault with its active
// passphrase.
func (v *Vault) Save() error {
	if v == nil {
		return fmt.Errorf("%w: nil vault", ErrInvalidArgument)
	}
	return v.saveWithPassphrase(v.passphrase)
}

// SaveWithCredentials saves the vault using the passphrase read from a new
// credentials file. The active passphrase changes only after the replacement
// file has been written successfully.
func (v *Vault) SaveWithCredentials(credentialsPath string) error {
	passphrase, err := readPassphraseFile(credentialsPath)
	if err != nil {
		return err
	}
	return v.SaveWithPassphrase(passphrase)
}

// SaveWithPassphrase saves the vault with passphrase directly. It is intended
// for callers that have already adapted an explicit passphrase source. The
// active passphrase changes only after the replacement succeeds.
func (v *Vault) SaveWithPassphrase(passphrase string) error {
	if v == nil {
		return fmt.Errorf("%w: nil vault", ErrInvalidArgument)
	}
	if err := v.saveWithPassphrase(passphrase); err != nil {
		return err
	}
	v.passphrase = passphrase
	return nil
}

func (v *Vault) saveWithPassphrase(passphrase string) error {
	if v.vaultPath == "" {
		return fmt.Errorf("%w: vault is not bound to a file", ErrInvalidArgument)
	}
	if err := validatePassphrase(passphrase); err != nil {
		return err
	}
	plaintext, err := marshalPayload(payloadFromVault(v))
	if err != nil {
		return err
	}
	defer clear(plaintext)
	ciphertext, err := encryptPayload(passphrase, plaintext)
	if err != nil {
		return err
	}
	return writeVaultFile(v.vaultPath, ciphertext)
}

// ExportJSON returns a layered JSON view of the current in-memory vault.
// Sensitive values are hidden unless includeSensitive is true.
func (v *Vault) ExportJSON(includeSensitive bool) ([]byte, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil vault", ErrInvalidArgument)
	}
	defaults, defaultRedactions, err := exportLayer(v.Defaults(), includeSensitive)
	if err != nil {
		return nil, err
	}
	environments := make(map[string]json.RawMessage, len(v.environments))
	redactedPaths := make([]string, 0, len(defaultRedactions))
	for _, path := range defaultRedactions {
		redactedPaths = append(redactedPaths, "/defaults"+path)
	}
	for name := range v.environments {
		layer := &Layer{vault: v, env: name}
		encoded, paths, err := exportLayer(layer, includeSensitive)
		if err != nil {
			return nil, err
		}
		environments[name] = encoded
		for _, path := range paths {
			redactedPaths = append(redactedPaths, "/environments/"+name+path)
		}
	}
	sort.Strings(redactedPaths)
	if redactedPaths == nil {
		redactedPaths = make([]string, 0)
	}
	view := struct {
		Service       string                     `json:"service"`
		Data          exportData                 `json:"data"`
		RedactedPaths []string                   `json:"redacted_paths"`
	}{
		Service: v.service,
		Data: exportData{Defaults: defaults, Environments: environments},
		RedactedPaths: redactedPaths,
	}
	encoded, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("%w: export cannot be encoded", ErrInvalidFormat)
	}
	return append(encoded, '\n'), nil
}

type exportData struct {
	Defaults     json.RawMessage            `json:"defaults"`
	Environments map[string]json.RawMessage `json:"environments"`
}

func exportLayer(layer *Layer, includeSensitive bool) (json.RawMessage, []string, error) {
	if includeSensitive {
		root, err := layer.Get("")
		return root, []string{}, err
	}
	return layer.GetRedacted("")
}

func createVaultFile(path string, ciphertext []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %w", ErrAlreadyExists, err)
		}
		return fmt.Errorf("create vault file: %w", err)
	}
	written, writeErr := file.Write(ciphertext)
	if writeErr == nil && written != len(ciphertext) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(
			wrapFileError("write vault file", writeErr),
			wrapFileError("sync vault file", syncErr),
			wrapFileError("close vault file", closeErr),
		)
	}
	return nil
}

// WriteExportFile writes an export without overwriting an existing file unless
// overwrite is true. It rejects destinations that alias any forbidden input
// path.
func WriteExportFile(path string, data []byte, overwrite bool, forbiddenPaths ...string) error {
	return writeExportFile(path, data, overwrite, forbiddenPaths...)
}
