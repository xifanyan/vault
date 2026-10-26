package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateOpenSaveAndInMemoryEdits(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "service.vault")
	credentialsPath := filepath.Join(dir, "service.pass")
	if err := os.WriteFile(credentialsPath, []byte("correct horse\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	v, err := Create(vaultPath, credentialsPath, "orders")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Service(); got != "orders" {
		t.Fatalf("Service() = %q, want orders", got)
	}

	if err := v.Defaults().Set("/count", json.RawMessage(`9007199254740993`)); err != nil {
		t.Fatal(err)
	}
	beforeSave, err := Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := beforeSave.Defaults().Get("/count"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unsaved edit reached disk: Get(/count) error = %v, want ErrNotFound", err)
	}

	if err := v.SaveWithPassphrase("correct horse"); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(vaultPath, credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := opened.Defaults().Get("/count")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `9007199254740993` {
		t.Fatalf("saved value = %s, want exact integer", got)
	}
}

func TestEnvironmentViewsAreSnapshotsAndLayersAreLive(t *testing.T) {
	v, err := CreateWithPassphrase(filepath.Join(t.TempDir(), "vault"), "catalog", "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	defaults := v.Defaults()
	if err := defaults.Set("/database", json.RawMessage(`{"host":"default","port":5432}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	prod, err := v.Environment("prod")
	if err != nil {
		t.Fatal(err)
	}
	host := json.RawMessage(`"production"`)
	if err := prod.Set("/database/host", host); err != nil {
		t.Fatal(err)
	}
	host[2] = 'X'
	layerValue, err := prod.Get("/database/host")
	if err != nil || string(layerValue) != `"production"` {
		t.Fatalf("mutating Set input changed layer value: %s, %v", layerValue, err)
	}
	layerValue[2] = 'X'
	layerValue, err = prod.Get("/database/host")
	if err != nil || string(layerValue) != `"production"` {
		t.Fatalf("mutating Get output changed layer value: %s, %v", layerValue, err)
	}
	if _, err := prod.Get("/database/port"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("environment layer inherited default: error = %v, want ErrNotFound", err)
	}
	config, err := v.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	got, err := config.Get("/database/host")
	if err != nil || string(got) != `"production"` {
		t.Fatalf("resolved host = %s, %v", got, err)
	}
	got[0] = 'x'
	got, err = config.Get("/database/host")
	if err != nil || string(got) != `"production"` {
		t.Fatalf("mutating returned JSON changed snapshot: %s, %v", got, err)
	}
	if err := defaults.Set("/database/port", json.RawMessage(`6432`)); err != nil {
		t.Fatal(err)
	}
	got, err = config.Get("/database/port")
	if err != nil || string(got) != `5432` {
		t.Fatalf("editing vault changed prior snapshot: %s, %v", got, err)
	}
	if err := v.DeleteEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := prod.Get(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted environment view error = %v, want ErrNotFound", err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	got, err = prod.Get("")
	if err != nil || string(got) != `{}` {
		t.Fatalf("recreated environment view = %s, %v; want empty new layer", got, err)
	}
	names := v.Environments()
	if len(names) != 1 || names[0] != "prod" {
		t.Fatalf("Environments() = %v, want [prod]", names)
	}
	names[0] = "mutated"
	if names := v.Environments(); len(names) != 1 || names[0] != "prod" {
		t.Fatalf("mutating returned environment names changed vault: %v", names)
	}
}

func TestExportJSONPreservesRawLayersAndOmitsClassifications(t *testing.T) {
	v, err := CreateWithPassphrase(filepath.Join(t.TempDir(), "vault"), "catalog", "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	defaults := v.Defaults()
	for pointer, value := range map[string]string{
		"/database/address":  `"default.db"`,
		"/database/password": `"top-secret"`,
		"/database/port":     `5432`,
		"/precision":         `9007199254740993`,
	} {
		if err := defaults.Set(pointer, json.RawMessage(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := v.Classify("/database", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/database/address", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/database/port", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/precision", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	prod, err := v.Environment("prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := prod.Set("/database/address", json.RawMessage(`"prod.db"`)); err != nil {
		t.Fatal(err)
	}
	if err := prod.Set("/database/password", json.RawMessage(`"prod-secret"`)); err != nil {
		t.Fatal(err)
	}

	encoded, err := v.ExportJSON(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "9007199254740993") {
		t.Fatalf("export lost exact integer: %s", encoded)
	}
	var view struct {
		Service string `json:"service"`
		Data struct {
			Defaults map[string]any `json:"defaults"`
			Environments map[string]map[string]any `json:"environments"`
		} `json:"data"`
		RedactedPaths []string `json:"redacted_paths"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	if view.Service != "catalog" {
		t.Fatalf("export service = %q", view.Service)
	}
	database := view.Data.Defaults["database"].(map[string]any)
	if database["address"] != "default.db" || database["password"] != nil || database["port"] != float64(5432) || len(database) != 3 {
		t.Fatalf("default raw layer = %#v", database)
	}
	prodDatabase := view.Data.Environments["prod"]["database"].(map[string]any)
	if prodDatabase["address"] != "prod.db" || prodDatabase["password"] != nil || len(prodDatabase) != 2 {
		t.Fatalf("environment export expanded or changed overrides: %#v", prodDatabase)
	}
	if len(view.RedactedPaths) != 2 || view.RedactedPaths[0] != "/defaults/database/password" || view.RedactedPaths[1] != "/environments/prod/database/password" {
		t.Fatalf("redacted_paths = %v", view.RedactedPaths)
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &outer); err != nil {
		t.Fatal(err)
	}
	if _, exists := outer["classifications"]; exists {
		t.Fatal("export included internal classifications")
	}

	full, err := v.ExportJSON(true)
	if err != nil {
		t.Fatal(err)
	}
	var fullView map[string]any
	if err := json.Unmarshal(full, &fullView); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(full), "top-secret") || !strings.Contains(string(full), "prod-secret") {
		t.Fatalf("include-sensitive export omitted real values: %s", full)
	}
	if paths := fullView["redacted_paths"].([]any); paths == nil || len(paths) != 0 {
		t.Fatalf("include-sensitive paths = %#v, want empty array", fullView["redacted_paths"])
	}
	again, err := v.ExportJSON(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(again), "top-secret") || strings.Contains(string(again), "prod-secret") {
		t.Fatalf("explicit secret export changed classifications: %s", again)
	}
}

func TestCredentialRotationPreservesOldCopies(t *testing.T) {
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault")
	oldCopyPath := filepath.Join(dir, "old-copy")
	v, err := CreateWithPassphrase(vaultPath, "catalog", "old passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/token", json.RawMessage(`"value"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatal(err)
	}
	oldCopy, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldCopyPath, oldCopy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveWithPassphrase(""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid rotation error = %v, want ErrInvalidArgument", err)
	}
	if err := v.Save(); err != nil {
		t.Fatalf("Save after failed rotation: %v", err)
	}
	if _, err := OpenWithPassphrase(vaultPath, "old passphrase"); err != nil {
		t.Fatalf("failed rotation changed the active passphrase: %v", err)
	}
	newCredentials := filepath.Join(dir, "new.pass")
	if err := os.WriteFile(newCredentials, []byte("new passphrase\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := v.SaveWithCredentials(newCredentials); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(vaultPath, newCredentials); err != nil {
		t.Fatalf("new credentials did not open updated file: %v", err)
	}
	if err := v.Save(); err != nil {
		t.Fatalf("Save did not use new credentials: %v", err)
	}
	if _, err := Open(vaultPath, newCredentials); err != nil {
		t.Fatalf("subsequent save did not retain new credentials: %v", err)
	}
	if err := v.SaveWithPassphrase("newer passphrase"); err != nil {
		t.Fatalf("SaveWithPassphrase rotation: %v", err)
	}
	if err := v.Defaults().Set("/after-rotation", json.RawMessage(`true`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(); err != nil {
		t.Fatalf("Save did not use new active passphrase: %v", err)
	}
	if _, err := OpenWithPassphrase(vaultPath, "newer passphrase"); err != nil {
		t.Fatalf("updated object passphrase did not protect subsequent save: %v", err)
	}
	if _, err := OpenWithPassphrase(oldCopyPath, "old passphrase"); err != nil {
		t.Fatalf("old copy no longer opens with old passphrase: %v", err)
	}
	if _, err := OpenWithPassphrase(vaultPath, "old passphrase"); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("old passphrase opened rotated file, error = %v", err)
	}
}

func TestGenerateCredentialsAndCreateRejectExistingFiles(t *testing.T) {
	dir := t.TempDir()
	credentials := filepath.Join(dir, "credentials")
	if err := GenerateCredentials(credentials); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 44 || data[43] != '\n' {
		t.Fatalf("generated credentials length/ending = %d / %q", len(data), data[len(data)-1:])
	}
	for _, char := range data[:43] {
		if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			t.Fatalf("generated credential contains non-base64url character %q", char)
		}
	}
	secret, err := base64.RawURLEncoding.Strict().DecodeString(string(data[:43]))
	if err != nil || len(secret) != 32 {
		t.Fatalf("generated credential payload decoded to %d bytes: %v", len(secret), err)
	}
	if err := GenerateCredentials(credentials); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second GenerateCredentials error = %v, want ErrAlreadyExists", err)
	}

	vaultPath := filepath.Join(dir, "vault")
	if _, err := CreateWithPassphrase(vaultPath, "catalog", "passphrase"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateWithPassphrase(vaultPath, "other", "passphrase"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second Create error = %v, want ErrAlreadyExists", err)
	}
}

func TestInvalidLayerEditDoesNotChangeValues(t *testing.T) {
	v, err := CreateWithPassphrase(filepath.Join(t.TempDir(), "vault"), "catalog", "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	layer := v.Defaults()
	if err := layer.Set("/root", json.RawMessage(`1`)); err != nil {
		t.Fatal(err)
	}
	before, err := layer.Get("")
	if err != nil {
		t.Fatal(err)
	}
	if err := layer.Set("/root/child", json.RawMessage(`true`)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("conflicting Set error = %v, want ErrInvalidArgument", err)
	}
	if err := layer.Set("/new/child", json.RawMessage(`{"broken":}`)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid JSON Set error = %v, want ErrInvalidArgument", err)
	}
	after, err := layer.Get("")
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("failed edit mutated layer: before %s, after %s", before, after)
	}
	if err := layer.Delete("/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing error = %v, want ErrNotFound", err)
	}
}
