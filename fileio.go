package vault

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func validatePassphrase(passphrase string) error {
	switch {
	case passphrase == "":
		return fmt.Errorf("%w: passphrase is empty", ErrInvalidArgument)
	case !utf8.ValidString(passphrase):
		return fmt.Errorf("%w: passphrase is not valid UTF-8", ErrInvalidArgument)
	case strings.HasPrefix(passphrase, "\uFEFF"):
		return fmt.Errorf("%w: passphrase must not start with a BOM", ErrInvalidArgument)
	case strings.IndexByte(passphrase, 0) >= 0:
		return fmt.Errorf("%w: passphrase contains NUL", ErrInvalidArgument)
	case strings.ContainsAny(passphrase, "\r\n"):
		return fmt.Errorf("%w: passphrase must be a single line", ErrInvalidArgument)
	default:
		return nil
	}
}

func readPassphraseFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open passphrase file: %w", err)
	}
	passphrase, readErr := readPassphrase(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return "", errors.Join(readErr, wrapFileError("close passphrase file", closeErr))
	}
	return passphrase, nil
}

func readPassphrase(reader io.Reader) (string, error) {
	if reader == nil {
		return "", fmt.Errorf("%w: nil passphrase reader", ErrInvalidArgument)
	}
	input, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	if len(input) > 0 && input[len(input)-1] == '\n' {
		input = input[:len(input)-1]
		if len(input) > 0 && input[len(input)-1] == '\r' {
			input = input[:len(input)-1]
		}
	}
	passphrase := string(input)
	if err := validatePassphrase(passphrase); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidFormat, err)
	}
	return passphrase, nil
}

func generateCredentials(path string) error {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return fmt.Errorf("generate passphrase: %w", err)
	}

	var contents [44]byte
	encodedLen := base64.RawURLEncoding.EncodedLen(len(secret))
	base64.RawURLEncoding.Encode(contents[:encodedLen], secret[:])
	contents[encodedLen] = '\n'

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %w", ErrAlreadyExists, err)
		}
		return fmt.Errorf("create passphrase file: %w", err)
	}
	written, writeErr := file.Write(contents[:encodedLen+1])
	if writeErr == nil && written != encodedLen+1 {
		writeErr = io.ErrShortWrite
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(wrapFileError("write passphrase file", writeErr), wrapFileError("close passphrase file", closeErr))
	}
	return nil
}

func readVaultFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open vault file: %w", err)
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, int64(maxVaultFileBytes)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(wrapFileError("read vault file", readErr), wrapFileError("close vault file", closeErr))
	}
	if len(contents) > maxVaultFileBytes {
		limitErr := fmt.Errorf("%w: vault file exceeds %d bytes", ErrLimitExceeded, maxVaultFileBytes)
		return nil, limitErr
	}
	return contents, nil
}

func writeVaultFile(path string, ciphertext []byte) error {
	if err := writeReplacementFile(path, ciphertext); err != nil {
		return fmt.Errorf("write vault file: %w", err)
	}
	return nil
}

func writeExportFile(path string, data []byte, overwrite bool, forbiddenPaths ...string) error {
	if err := rejectInputAliases(path, forbiddenPaths); err != nil {
		return err
	}
	if overwrite {
		if err := writeReplacementFile(path, data); err != nil {
			return fmt.Errorf("write export file: %w", err)
		}
		return nil
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%w: %w", ErrAlreadyExists, err)
		}
		return fmt.Errorf("create export file: %w", err)
	}
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(
			wrapFileError("write export file", writeErr),
			wrapFileError("sync export file", syncErr),
			wrapFileError("close export file", closeErr),
		)
	}
	return nil
}

func rejectInputAliases(destination string, forbiddenPaths []string) error {
	destinationInfo, destinationErr := os.Stat(destination)
	if destinationErr != nil && !errors.Is(destinationErr, fs.ErrNotExist) {
		return fmt.Errorf("stat export destination: %w", destinationErr)
	}
	if destinationErr != nil {
		destinationInfo = nil
	}
	for _, forbiddenPath := range forbiddenPaths {
		inputInfo, err := os.Stat(forbiddenPath)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("stat export input: %w", err)
		}
		if destinationInfo != nil && os.SameFile(destinationInfo, inputInfo) {
			return fmt.Errorf("%w: export destination aliases an input file", ErrInvalidArgument)
		}
	}
	return nil
}

func writeReplacementFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".vault-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	temporaryPath := file.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.Join(
			wrapFileError("write temporary file", writeErr),
			wrapFileError("sync temporary file", syncErr),
			wrapFileError("close temporary file", closeErr),
		)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace destination: %w", err)
	}
	keepTemporary = false
	return nil
}

func wrapFileError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
