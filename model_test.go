package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLayerSetAndGetPreserveExactJSONAndOwnership(t *testing.T) {
	v := newVault("svc")
	layer := v.Defaults()
	input := json.RawMessage(`9007199254740993`)
	if err := layer.Set("/n", input); err != nil {
		t.Fatal(err)
	}
	input[0] = '0'

	got, err := layer.Get("/n")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `9007199254740993` {
		t.Fatalf("got %s, want exact integer", got)
	}
	got[0] = '0'
	got, err = layer.Get("/n")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `9007199254740993` {
		t.Fatalf("mutating returned bytes changed layer value: %s", got)
	}
}

func TestLayerSetRejectsMalformedAndDuplicateJSON(t *testing.T) {
	v := newVault("svc")
	layer := v.Defaults()
	for _, input := range []json.RawMessage{
		json.RawMessage(`{"a":1,"a":2}`),
        json.RawMessage(`{"a":1,"\u0061":2}`),
		json.RawMessage(`null true`),
		json.RawMessage{0xff},
	} {
		before, err := layer.Get("")
		if err != nil {
			t.Fatal(err)
		}
		err = layer.Set("/candidate", input)
		if !errors.Is(err, ErrInvalidArgument) && !errors.Is(err, ErrLimitExceeded) {
			t.Fatalf("Set(%q) error = %v, want invalid argument or limit exceeded", input, err)
		}
		after, getErr := layer.Get("")
		if getErr != nil {
			t.Fatal(getErr)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("invalid JSON changed layer: before %s, after %s", before, after)
		}
	}
}

func TestJSONPointerObjectOnlyMutationAndAtomicFailure(t *testing.T) {
	v := newVault("svc")
	layer := v.Defaults()
	for _, item := range []struct{ pointer, value string }{
		{"/", `"empty"`},
		{"/a~1b", `"slash"`},
		{"/a~0b", `"tilde"`},
		{"/服务/地址", `"unicode"`},
		{"/nullable", `null`},
		{"/array", `["first"]`},
	} {
		if err := layer.Set(item.pointer, json.RawMessage(item.value)); err != nil {
			t.Fatalf("Set(%q): %v", item.pointer, err)
		}
		got, err := layer.Get(item.pointer)
		if err != nil || string(got) != item.value {
			t.Fatalf("Get(%q) = %s, %v; want %s", item.pointer, got, err, item.value)
		}
	}
	for _, pointer := range []string{"relative", "/bad~2escape", "/array/0"} {
		if _, err := layer.Get(pointer); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("Get(%q) error = %v, want ErrInvalidArgument", pointer, err)
		}
	}
	for _, pointer := range []string{"", "/bad~"} {
		if err := layer.Set(pointer, json.RawMessage(`1`)); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("Set(%q) error = %v, want ErrInvalidArgument", pointer, err)
		}
	}
	if err := layer.Delete(""); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Delete root error = %v, want ErrInvalidArgument", err)
	}
	if err := layer.Delete("/"); err != nil {
		t.Fatalf("Delete empty-string key: %v", err)
	}
	if _, err := layer.Get("/"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty-string key after deletion error = %v, want ErrNotFound", err)
	}
	if _, err := layer.Get("/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing lookup error = %v, want ErrNotFound", err)
	}
	if err := layer.Set("/blocked", json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
	before, err := layer.Get("")
	if err != nil {
		t.Fatal(err)
	}
	if err := layer.Set("/blocked/child/deep", json.RawMessage(`1`)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Set through null error = %v, want ErrInvalidArgument", err)
	}
	after, err := layer.Get("")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed parent creation changed root: before %s after %s (%v)", before, after, err)
	}
	if err := layer.Set("/array/0", json.RawMessage(`"replacement"`)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("Set through array error = %v, want ErrInvalidArgument", err)
	}
}

func TestResolveMergesOverridesAndKeepsSnapshots(t *testing.T) {
	v := newVault("svc")
	defaults := v.Defaults()
	if err := defaults.Set("/database", json.RawMessage(`{"host":"default","port":5432,"nullable":null,"servers":["a","b"]}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	prod, err := v.Environment("prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ pointer, value string }{
		{"/database/host", `"prod"`},
		{"/database/servers", `["only"]`},
		{"/database/nullable", `null`},
	} {
		if err := prod.Set(item.pointer, json.RawMessage(item.value)); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := v.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ pointer, want string }{
		{"/database/host", `"prod"`},
		{"/database/port", `5432`},
		{"/database/nullable", `null`},
		{"/database/servers", `["only"]`},
	} {
		got, err := snapshot.Get(item.pointer)
		if err != nil || string(got) != item.want {
			t.Errorf("snapshot Get(%q) = %s, %v; want %s", item.pointer, got, err, item.want)
		}
	}
	owned, err := snapshot.Get("/database/host")
	if err != nil {
		t.Fatal(err)
	}
	owned[0] = 'x'
	owned, err = snapshot.Get("/database/host")
	if err != nil || string(owned) != `"prod"` {
		t.Fatalf("mutating returned snapshot bytes changed snapshot: %s, %v", owned, err)
	}
	if err := prod.Set("/database/host", json.RawMessage(`"later"`)); err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Get("/database/host")
	if err != nil || string(got) != `"prod"` {
		t.Fatalf("old snapshot changed after edit: %s, %v", got, err)
	}
	if err := prod.Delete("/database/host"); err != nil {
		t.Fatal(err)
	}
	resolved, err := v.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	got, err = resolved.Get("/database/host")
	if err != nil || string(got) != `"default"` {
		t.Fatalf("deleting override did not restore inheritance: %s, %v", got, err)
	}
	if _, err := v.Resolve("unknown"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown environment error = %v, want ErrNotFound", err)
	}
}

func TestClassificationsPersistAcrossDeleteAndSet(t *testing.T) {
	v := newVault("svc")
	layer := v.Defaults()
	if err := layer.Set("/token", json.RawMessage(`"first"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/token", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := layer.Delete("/token"); err != nil {
		t.Fatal(err)
	}
	if err := layer.Set("/token", json.RawMessage(`"restored"`)); err != nil {
		t.Fatal(err)
	}
    got, paths, err := layer.GetRedacted("/token")
    if err != nil || string(got) != `"restored"` || len(paths) != 0 {
        t.Fatalf("restored non-sensitive classification = %s, %v, %v", got, paths, err)
    }
	if err := v.Classify("/token", Sensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/not-present", NonSensitive); !errors.Is(err, ErrNotFound) {
		t.Fatalf("classify absent path error = %v, want ErrNotFound", err)
	}
}

func TestRedactionMasksSensitiveContainersAndObscuredAncestors(t *testing.T) {
	v := newVault("svc")
	defaults := v.Defaults()
	if err := defaults.Set("/database", json.RawMessage(`{"host":"db","password":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/database", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := defaults.Set("/credentials", json.RawMessage(`{"private":"hidden","nested":{"name":"hidden"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	prod, err := v.Environment("prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := prod.Set("/database", json.RawMessage(`"opaque"`)); err != nil {
		t.Fatal(err)
	}
	before := make(map[string]Classification, len(v.classifications))
	for pointer, classification := range v.classifications {
		before[pointer] = classification
	}
	config, err := v.Resolve("prod")
	if err != nil {
		t.Fatal(err)
	}
	whole, paths, err := config.GetRedacted("")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(whole, []byte(`"database":null`)) || !bytes.Contains(whole, []byte(`"credentials":null`)) {
		t.Fatalf("redacted view did not mask containers: %s", whole)
	}
	if !containsPath(paths, "/database") || !containsPath(paths, "/credentials") || containsPath(paths, "/credentials/private") {
		t.Fatalf("redacted paths = %v, want only container boundaries", paths)
	}
	query, queryPaths, err := config.GetRedacted("/database/does-not-exist")
	if err != nil || string(query) != "null" || len(queryPaths) != 1 || queryPaths[0] != "/database" {
		t.Fatalf("opaque query = %s, %v, %v; want null at /database", query, queryPaths, err)
	}
	query, queryPaths, err = config.GetRedacted("/credentials/missing/deeper")
	if err != nil || string(query) != "null" || len(queryPaths) != 1 || queryPaths[0] != "/credentials" {
		t.Fatalf("sensitive-container query = %s, %v, %v", query, queryPaths, err)
	}
	if _, _, err := config.GetRedacted("/database/bad~2escape"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("invalid pointer under redacted container error = %v, want ErrInvalidArgument", err)
	}
	trueValue, err := config.Get("/database")
	if err != nil || string(trueValue) != `"opaque"` {
		t.Fatalf("redaction mutated true config: %s, %v", trueValue, err)
	}
	trueSecret, err := config.Get("/credentials/private")
	if err != nil || string(trueSecret) != `"hidden"` {
		t.Fatalf("business read did not return real value: %s, %v", trueSecret, err)
	}
	if !reflect.DeepEqual(before, v.classifications) {
		t.Fatalf("redaction mutated classifications: before %v after %v", before, v.classifications)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func TestPayloadRoundTripPreservesNumbersAndStrictSchema(t *testing.T) {
	input := payload{
		SchemaVersion: 1,
		Service:       "svc",
		Defaults: map[string]any{
			"large": json.Number("9007199254740993"),
			"database": map[string]any{"password": "secret"},
		},
		Environments: map[string]map[string]any{"prod": {"database": map[string]any{"host": "db"}}},
		Classifications: map[string]Classification{
			"/large":             Sensitive,
			"/database":          NonSensitive,
			"/database/password": Sensitive,
			"/database/host":     Sensitive,
		},
	}
	encoded, err := marshalPayload(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := unmarshalPayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Defaults["large"].(json.Number).String(); got != "9007199254740993" {
		t.Fatalf("large number changed: %s", got)
	}
	if decoded.Classifications["/database/password"] != Sensitive || decoded.Service != "svc" {
		t.Fatalf("payload fields were not preserved: %#v", decoded)
	}

	valid := `{"schema_version":1,"service":"svc","defaults":{"x":1},"environments":{},"classifications":{"/x":"sensitive"}}`
	for _, invalid := range []string{
		`{"schema_version":1,"schema_version":1,"service":"svc","defaults":{},"environments":{},"classifications":{}}`,
		`{"schema_version":1,"service":"svc","\u0073ervice":"other","defaults":{},"environments":{},"classifications":{}}`,
		`{"schema_version":1,"service":"svc","defaults":{"x":1},"environments":{},"classifications":{"/x":"unknown"}}`,
		`{"schema_version":1,"service":"svc","defaults":{"x":1},"environments":{},"classifications":{}}`,
		`{"schema_version":1.0,"service":"svc","defaults":{},"environments":{},"classifications":{}}`,
		`{"schema_version":1,"service":"svc","defaults":{},"environments":{},"classifications":{},"extra":true}`,
		valid + ` {}`,
	} {
		if _, err := unmarshalPayload([]byte(invalid)); !errors.Is(err, ErrInvalidFormat) {
			t.Errorf("unmarshal invalid payload %s: error = %v, want ErrInvalidFormat", invalid, err)
		}
	}
	if _, err := unmarshalPayload([]byte(`{"schema_version":1,"service":"svc","defaults":{},"environments":{"Bad":{}},"classifications":{}}`)); !errors.Is(err, ErrInvalidFormat) {
		t.Errorf("invalid environment error = %v, want ErrInvalidFormat", err)
	}
	tooDeep := `{"schema_version":1,"service":"svc","defaults":` + strings.Repeat(`{"x":`, maxJSONDepth) + `null` + strings.Repeat(`}`, maxJSONDepth) + `,"environments":{},"classifications":{}}`
	if _, err := unmarshalPayload([]byte(tooDeep)); !errors.Is(err, ErrLimitExceeded) {
		t.Errorf("deep payload error = %v, want ErrLimitExceeded", err)
	}
}

func TestPayloadParserRejectsInvalidUTF8(t *testing.T) {
	input := []byte(`{"schema_version":1,"service":"svc","defaults":{},"environments":{},"classifications":{}}`)
	input = append(input[:len(input)-2], 0xff, '}', '}')
	if _, err := unmarshalPayload(input); !errors.Is(err, ErrInvalidFormat) {
		t.Fatalf("invalid UTF-8 payload error = %v, want ErrInvalidFormat", err)
	}
}

func TestEnvironmentLayersAreIndependentAndLiveViewsFollowRecreation(t *testing.T) {
	v := newVault("svc")
	if err := v.Defaults().Set("/database", json.RawMessage(`"default-is-scalar"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	prod, err := v.Environment("prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := prod.Set("/database/host", json.RawMessage(`"override"`)); err != nil {
		t.Fatalf("environment path incorrectly conflicted with defaults: %v", err)
	}
	if err := v.DeleteEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := prod.Get(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale environment layer error = %v, want ErrNotFound", err)
	}
	if err := v.CreateEnvironment("prod"); err != nil {
		t.Fatal(err)
	}
	if err := prod.Set("/fresh", json.RawMessage(`true`)); err != nil {
		t.Fatalf("environment view did not follow recreated name: %v", err)
	}
	got, err := prod.Get("/fresh")
	if err != nil || string(got) != `true` {
		t.Fatalf("recreated layer value = %s, %v", got, err)
	}
	if got := v.Environments(); len(got) != 1 || got[0] != "prod" {
		t.Fatalf("environment listing = %v", got)
	}
}

func TestAutomaticStructureAndClassificationDoNotLeak(t *testing.T) {
	v := newVault("svc")
	layer := v.Defaults()
	if err := layer.Set("/new/host", json.RawMessage(`"db"`)); err != nil {
		t.Fatal(err)
	}
	whole, paths, err := layer.GetRedacted("")
	if err != nil || !bytes.Contains(whole, []byte(`"new":{"host":null}`)) || !containsPath(paths, "/new/host") || containsPath(paths, "/new") {
		t.Fatalf("automatic structure redaction = %s, %v, %v", whole, paths, err)
	}
	if err := v.Classify("/new", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/new/host", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := layer.Set("/new/other", json.RawMessage(`"hidden"`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/new", Sensitive); err != nil {
		t.Fatal(err)
	}
	whole, paths, err = layer.GetRedacted("")
	if err != nil || !bytes.Contains(whole, []byte(`"new":null`)) || len(paths) != 1 || paths[0] != "/new" {
		t.Fatalf("sensitive ancestor did not mask descendants: %s, %v, %v", whole, paths, err)
	}
	if err := v.Classify("/new", NonSensitive); err != nil {
		t.Fatal(err)
	}
	whole, paths, err = layer.GetRedacted("")
	if err != nil || containsPath(paths, "/new") || !containsPath(paths, "/new/other") {
		t.Fatalf("cancelling parent classification changed descendants: %s, %v, %v", whole, paths, err)
	}
	if err := v.Classify("", Sensitive); err != nil {
		t.Fatal(err)
	}
	whole, paths, err = layer.GetRedacted("")
	if err != nil || string(whole) != "null" || len(paths) != 1 || paths[0] != "" {
		t.Fatalf("root classification redaction = %s, %v, %v", whole, paths, err)
	}
}

func TestSetRejectsClassificationsBeyondPointerDepthAtomically(t *testing.T) {
	layer := newVault("svc").Defaults()
	deepObject := strings.Repeat(`{"x":`, maxJSONDepth) + `"leaf"` + strings.Repeat(`}`, maxJSONDepth)
	before, err := layer.Get("")
	if err != nil {
		t.Fatal(err)
	}
	if err := layer.Set("/deep", json.RawMessage(deepObject)); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("Set deep object error = %v, want ErrLimitExceeded", err)
	}
	after, err := layer.Get("")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("depth-rejected value changed layer: before %s after %s (%v)", before, after, err)
	}
}

func TestRedactionMasksNullAndArrayTypeObstructions(t *testing.T) {
	v := newVault("svc")
	if err := v.Defaults().Set("/resource", json.RawMessage(`{"password":"secret"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/resource", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/ordinary-null", json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Defaults().Set("/sensitive-null", json.RawMessage(`null`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Classify("/ordinary-null", NonSensitive); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("nullcase"); err != nil {
		t.Fatal(err)
	}
	if err := v.CreateEnvironment("arraycase"); err != nil {
		t.Fatal(err)
	}
	for environment, replacement := range map[string]string{
		"nullcase":  `null`,
		"arraycase": `[]`,
	} {
		layer, err := v.Environment(environment)
		if err != nil {
			t.Fatal(err)
		}
		if err := layer.Set("/resource", json.RawMessage(replacement)); err != nil {
			t.Fatal(err)
		}
		config, err := v.Resolve(environment)
		if err != nil {
			t.Fatal(err)
		}
		whole, paths, err := config.GetRedacted("")
		if err != nil || !bytes.Contains(whole, []byte(`"resource":null`)) || !containsPath(paths, "/resource") || !containsPath(paths, "/sensitive-null") {
			t.Errorf("%s obstruction view = %s, %v, %v", environment, whole, paths, err)
		}
		if containsPath(paths, "/ordinary-null") {
			t.Errorf("ordinary null was reported as redacted: %v", paths)
		}
	}
}
