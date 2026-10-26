package vault

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"filippo.io/age"
)

const ageScryptWorkFactor = 18

var ageBase64 = base64.RawStdEncoding.Strict()

func encryptPayload(passphrase string, plaintext []byte) ([]byte, error) {
	if len(plaintext) > maxPayloadBytes {
		return nil, ErrLimitExceeded
	}
	if passphrase == "" {
		return nil, ErrInvalidArgument
	}

	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, ErrInvalidArgument
	}
	recipient.SetWorkFactor(ageScryptWorkFactor)

	var ciphertext bytes.Buffer
	writer, err := age.Encrypt(&ciphertext, recipient)
	if err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}
	if _, err := writer.Write(plaintext); err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}
	if ciphertext.Len() > maxVaultFileBytes {
		return nil, ErrLimitExceeded
	}
	return ciphertext.Bytes(), nil
}

func decryptPayload(passphrase string, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) > maxVaultFileBytes {
		return nil, ErrLimitExceeded
	}
	if passphrase == "" {
		return nil, ErrInvalidArgument
	}
	if err := inspectAgeHeader(ciphertext); err != nil {
		return nil, ErrInvalidFormat
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, ErrInvalidArgument
	}
	identity.SetMaxWorkFactor(ageScryptWorkFactor)

	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, ErrAuthentication
	}
	plaintext, err := io.ReadAll(io.LimitReader(reader, maxPayloadBytes+1))
	if err != nil {
		clear(plaintext)
		return nil, ErrAuthentication
	}
	if len(plaintext) > maxPayloadBytes {
		clear(plaintext)
		return nil, ErrLimitExceeded
	}
	return plaintext, nil
}

func inspectAgeHeader(ciphertext []byte) error {
	const intro = "age-encryption.org/v1\n"
	if !bytes.HasPrefix(ciphertext, []byte(intro)) {
		return errors.New("unsupported age format")
	}

	footerStart := bytes.Index(ciphertext, []byte("\n--- "))
	if footerStart < 0 || footerStart > maxAgeHeaderBytes {
		return errors.New("invalid age header")
	}
	footerLineStart := footerStart + 1
	footerLineEndRelative := bytes.IndexByte(ciphertext[footerLineStart:], '\n')
	if footerLineEndRelative < 0 {
		return errors.New("unterminated age header")
	}
	footerLineEnd := footerLineStart + footerLineEndRelative
	if footerLineEnd+1 > maxAgeHeaderBytes {
		return errors.New("age header exceeds limit")
	}

	headerLines := bytes.Split(ciphertext[:footerStart], []byte{'\n'})
	if len(headerLines) != 3 || string(headerLines[0])+"\n" != intro {
		return errors.New("unsupported age stanza layout")
	}
	stanzaFields := strings.Split(string(headerLines[1]), " ")
	if len(stanzaFields) != 4 || stanzaFields[0] != "->" || stanzaFields[1] != "scrypt" {
		return errors.New("unsupported age stanza")
	}
	if stanzaFields[3] != "18" {
		return errors.New("unsupported scrypt work factor")
	}
	salt, err := ageBase64.DecodeString(stanzaFields[2])
	if err != nil || len(salt) != 16 {
		return errors.New("invalid scrypt salt")
	}
	wrappedKey, err := ageBase64.DecodeString(string(headerLines[2]))
	if err != nil || len(wrappedKey) != 32 {
		return errors.New("invalid wrapped file key")
	}

	footerFields := strings.Split(string(ciphertext[footerLineStart:footerLineEnd]), " ")
	if len(footerFields) != 2 || footerFields[0] != "---" {
		return errors.New("invalid age header footer")
	}
	mac, err := ageBase64.DecodeString(footerFields[1])
	if err != nil || len(mac) != 32 {
		return errors.New("invalid age header MAC")
	}
	return nil
}
