package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

func parseJSON(data []byte) (any, error) {
	if !utf8.Valid(data) || bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return nil, fmt.Errorf("%w: JSON must be valid UTF-8 without a BOM", ErrInvalidFormat)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	value, err := parseJSONValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: multiple JSON values", ErrInvalidFormat)
		}
		return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidFormat)
	}
	return value, nil
}

func parseJSONValue(dec *json.Decoder, depth int) (any, error) {
	token, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidFormat)
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	if delim != '{' && delim != '[' {
		return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidFormat)
	}
	depth++
	if depth > maxJSONDepth {
		return nil, ErrLimitExceeded
	}
	if delim == '{' {
		object := make(map[string]any)
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("%w: malformed JSON", ErrInvalidFormat)
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("%w: malformed JSON object key", ErrInvalidFormat)
			}
			if _, exists := object[key]; exists {
				return nil, fmt.Errorf("%w: duplicate JSON object key", ErrInvalidFormat)
			}
			value, err := parseJSONValue(dec, depth)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("%w: malformed JSON object", ErrInvalidFormat)
		}
		return object, nil
	}
	array := make([]any, 0)
	for dec.More() {
		value, err := parseJSONValue(dec, depth)
		if err != nil {
			return nil, err
		}
		array = append(array, value)
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%w: malformed JSON array", ErrInvalidFormat)
	}
	return array, nil
}

func parsePointer(pointer string) ([]string, error) {
	if !utf8.ValidString(pointer) {
		return nil, fmt.Errorf("%w: invalid JSON Pointer encoding", ErrInvalidArgument)
	}
	if pointer == "" {
		return nil, nil
	}
	if pointer[0] != '/' {
		return nil, fmt.Errorf("%w: JSON Pointer must be empty or begin with '/'", ErrInvalidArgument)
	}
	parts := strings.Split(pointer[1:], "/")
	if len(parts) > maxPointerDepth {
		return nil, ErrLimitExceeded
	}
	for i, part := range parts {
		var decoded strings.Builder
		decoded.Grow(len(part))
		for j := 0; j < len(part); j++ {
			if part[j] != '~' {
				decoded.WriteByte(part[j])
				continue
			}
			if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
				return nil, fmt.Errorf("%w: invalid JSON Pointer escape", ErrInvalidArgument)
			}
			j++
			if part[j] == '0' {
				decoded.WriteByte('~')
			} else {
				decoded.WriteByte('/')
			}
		}
		parts[i] = decoded.String()
	}
	return parts, nil
}

func pointerFor(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, part := range parts {
		b.WriteByte('/')
		part = strings.ReplaceAll(part, "~", "~0")
		part = strings.ReplaceAll(part, "/", "~1")
		b.WriteString(part)
	}
	return b.String()
}

func jsonBytes(value any) (json.RawMessage, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: JSON value cannot be encoded", ErrInvalidFormat)
	}
	return json.RawMessage(data), nil
}

func lookup(root any, parts []string) (any, error) {
	current := root
	for _, part := range parts {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: JSON Pointer traverses a non-object", ErrInvalidArgument)
		}
		var exists bool
		current, exists = object[part]
		if !exists {
			return nil, ErrNotFound
		}
	}
	return current, nil
}

func setValue(root map[string]any, parts []string, value any) error {
	if len(parts) == 0 {
		return fmt.Errorf("%w: cannot replace configuration root", ErrInvalidArgument)
	}
	current := root
	for i, part := range parts[:len(parts)-1] {
		child, exists := current[part]
		if !exists {
			branch := make(map[string]any)
			current[part] = branch
			current = branch
			for _, remaining := range parts[i+1 : len(parts)-1] {
				next := make(map[string]any)
				current[remaining] = next
				current = next
			}
			current[parts[len(parts)-1]] = value
			return nil
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: JSON Pointer traverses a non-object", ErrInvalidArgument)
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func invalidJSONArgument(err error) error {
	if errors.Is(err, ErrInvalidFormat) {
		return fmt.Errorf("%w: invalid JSON value", ErrInvalidArgument)
	}
	return err
}

type Classification string

const (
	Sensitive    Classification = "sensitive"
	NonSensitive Classification = "non-sensitive"
)

type payload struct {
	SchemaVersion   int                         `json:"schema_version"`
	Service         string                      `json:"service"`
	Defaults        map[string]any              `json:"defaults"`
	Environments    map[string]map[string]any   `json:"environments"`
	Classifications map[string]Classification `json:"classifications"`
}

type Vault struct {
	service            string
	defaults           map[string]any
	environments       map[string]map[string]any
	classifications    map[string]Classification
	classificationIndex *classificationNode
	vaultPath          string
	passphrase         string
}

type Layer struct {
	vault *Vault
	env   string
}

type Config struct {
	root            map[string]any
	classifications map[string]Classification
	classificationIndex *classificationNode
}

func newVault(service string) *Vault {
	return &Vault{
		service:         service,
		defaults:        make(map[string]any),
		environments:    make(map[string]map[string]any),
		classifications: make(map[string]Classification),
	}
}

func (v *Vault) Service() string {
	if v == nil {
		return ""
	}
	return v.service
}

func (v *Vault) Environments() []string {
	if v == nil {
		return nil
	}
	names := make([]string, 0, len(v.environments))
	for name := range v.environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (v *Vault) Defaults() *Layer {
	return &Layer{vault: v}
}

func (v *Vault) Environment(name string) (*Layer, error) {
	if !validEnvironment(name) {
		return nil, fmt.Errorf("%w: invalid environment name", ErrInvalidArgument)
	}
	if v == nil {
		return nil, ErrNotFound
	}
	if _, exists := v.environments[name]; !exists {
		return nil, ErrNotFound
	}
	return &Layer{vault: v, env: name}, nil
}

func (v *Vault) CreateEnvironment(name string) error {
	if v == nil || !validEnvironment(name) {
		return fmt.Errorf("%w: invalid environment name", ErrInvalidArgument)
	}
	if _, exists := v.environments[name]; exists {
		return ErrAlreadyExists
	}
	v.environments[name] = make(map[string]any)
	return nil
}

func (v *Vault) DeleteEnvironment(name string) error {
	if v == nil || !validEnvironment(name) {
		return fmt.Errorf("%w: invalid environment name", ErrInvalidArgument)
	}
	if _, exists := v.environments[name]; !exists {
		return ErrNotFound
	}
	delete(v.environments, name)
	return nil
}

func validEnvironment(name string) bool {
	if name == "" || !utf8.ValidString(name) || !((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= '0' && name[0] <= '9')) {
		return false
	}
	for i := range name[1:] {
		c := name[i+1]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func (l *Layer) root() (map[string]any, error) {
	if l == nil || l.vault == nil {
		return nil, fmt.Errorf("%w: nil layer", ErrInvalidArgument)
	}
	if l.env == "" {
		return l.vault.defaults, nil
	}
	root, exists := l.vault.environments[l.env]
	if !exists {
		return nil, ErrNotFound
	}
	return root, nil
}

func (l *Layer) Get(pointer string) (json.RawMessage, error) {
	parts, err := parsePointer(pointer)
	if err != nil {
		return nil, err
	}
	root, err := l.root()
	if err != nil {
		return nil, err
	}
	value, err := lookup(root, parts)
	if err != nil {
		return nil, err
	}
	return jsonBytes(value)
}

func (l *Layer) Set(pointer string, value json.RawMessage) error {
	parts, err := parsePointer(pointer)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("%w: cannot replace configuration root", ErrInvalidArgument)
	}
	root, err := l.root()
	if err != nil {
		return err
	}
	parsed, err := parseJSON(value)
	if err != nil {
		return invalidJSONArgument(err)
	}
	if err := validateClassificationDepth(parsed, len(parts)); err != nil {
		return err
	}
	var missingPrefixes []string
	current := root
	for i, part := range parts[:len(parts)-1] {
		child, exists := current[part]
		if !exists {
			missingPrefixes = append(missingPrefixes, pointerFor(parts[:i+1]))
			for offset := range parts[i+1 : len(parts)-1] {
				j := i + 1 + offset
				missingPrefixes = append(missingPrefixes, pointerFor(parts[:j+1]))
			}
			break
		}
		next, ok := child.(map[string]any)
		if !ok {
			return fmt.Errorf("%w: JSON Pointer traverses a non-object", ErrInvalidArgument)
		}
		current = next
	}
	if err := setValue(root, parts, parsed); err != nil {
		return err
	}
	classifyTree(parsed, parts, l.vault.classifications)
	for _, prefix := range missingPrefixes {
		if _, classified := l.vault.classifications[prefix]; !classified {
			l.vault.classifications[prefix] = NonSensitive
		}
	}
	l.vault.classificationIndex = nil
	return nil
}

func (l *Layer) Delete(pointer string) error {
	parts, err := parsePointer(pointer)
	if err != nil {
		return err
	}
	if len(parts) == 0 {
		return fmt.Errorf("%w: cannot delete configuration root", ErrInvalidArgument)
	}
	root, err := l.root()
	if err != nil {
		return err
	}
	if _, err := lookup(root, parts); err != nil {
		return err
	}
	parentValue, err := lookup(root, parts[:len(parts)-1])
	if err != nil {
		return err
	}
	parent := parentValue.(map[string]any)
	delete(parent, parts[len(parts)-1])
	return nil
}

func classifyTree(value any, parts []string, classifications map[string]Classification) {
	if len(parts) != 0 {
		pointer := pointerFor(parts)
		if _, exists := classifications[pointer]; !exists {
			classifications[pointer] = Sensitive
		}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return
	}
	for key, child := range object {
		classifyTree(child, appendPointer(parts, key), classifications)
	}
}

func validateClassificationDepth(value any, pathDepth int) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if pathDepth >= maxPointerDepth && len(object) != 0 {
		return ErrLimitExceeded
	}
	for _, child := range object {
		if err := validateClassificationDepth(child, pathDepth+1); err != nil {
			return err
		}
	}
	return nil
}

func appendPointer(parts []string, key string) []string {
	child := make([]string, len(parts)+1)
	copy(child, parts)
	child[len(parts)] = key
	return child
}

func (v *Vault) Classify(pointer string, classification Classification) error {
	parts, err := parsePointer(pointer)
	if err != nil {
		return err
	}
	if classification != Sensitive && classification != NonSensitive {
		return fmt.Errorf("%w: unknown classification", ErrInvalidArgument)
	}
	if v == nil {
		return fmt.Errorf("%w: nil vault", ErrInvalidArgument)
	}
	key := pointerFor(parts)
	if _, exists := v.classifications[key]; !exists {
		found := false
		if len(parts) == 0 {
			found = true
		} else {
			if _, err := lookup(v.defaults, parts); err == nil {
				found = true
			} else {
				for _, root := range v.environments {
					if _, err := lookup(root, parts); err == nil {
						found = true
						break
					}
				}
			}
		}
		if !found {
			return ErrNotFound
		}
	}
	v.classifications[key] = classification
	v.classificationIndex = nil
	return nil
}

func (v *Vault) Resolve(environment string) (*Config, error) {
	if v == nil {
		return nil, ErrNotFound
	}
	override, exists := v.environments[environment]
	if !exists {
		return nil, ErrNotFound
	}
	root := mergeObjects(v.defaults, override)
	return &Config{root: root, classifications: cloneClassifications(v.classifications), classificationIndex: v.classificationTree()}, nil
}

func (c *Config) Get(pointer string) (json.RawMessage, error) {
	parts, err := parsePointer(pointer)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("%w: nil config", ErrInvalidArgument)
	}
	value, err := lookup(c.root, parts)
	if err != nil {
		return nil, err
	}
	return jsonBytes(value)
}

func mergeObjects(defaults, override map[string]any) map[string]any {
	merged := cloneObject(defaults)
	for key, overrideValue := range override {
		if defaultObject, ok := merged[key].(map[string]any); ok {
			if overrideObject, ok := overrideValue.(map[string]any); ok {
				merged[key] = mergeObjects(defaultObject, overrideObject)
				continue
			}
		}
		merged[key] = cloneValue(overrideValue)
	}
	return merged
}

func cloneObject(object map[string]any) map[string]any {
	copy := make(map[string]any, len(object))
	for key, value := range object {
		copy[key] = cloneValue(value)
	}
	return copy
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneObject(value)
	case []any:
		copy := make([]any, len(value))
		for i, element := range value {
			copy[i] = cloneValue(element)
		}
		return copy
	default:
		return value
	}
}

func cloneClassifications(classifications map[string]Classification) map[string]Classification {
	copy := make(map[string]Classification, len(classifications))
	for pointer, classification := range classifications {
		copy[pointer] = classification
	}
	return copy
}

type classificationNode struct {
	classification Classification
	children       map[string]*classificationNode
	sensitive      bool
}

func buildClassificationTree(classifications map[string]Classification) *classificationNode {
	root := &classificationNode{children: make(map[string]*classificationNode)}
	for pointer, classification := range classifications {
		parts, err := parsePointer(pointer)
		if err != nil {
			continue
		}
		node := root
		for _, part := range parts {
			if node.children == nil {
				node.children = make(map[string]*classificationNode)
			}
			if node.children[part] == nil {
				node.children[part] = &classificationNode{}
			}
			node = node.children[part]
		}
		node.classification = classification
	}
	markSensitiveDescendants(root)
	return root
}

func markSensitiveDescendants(node *classificationNode) bool {
	node.sensitive = node.classification == Sensitive
	for _, child := range node.children {
		if markSensitiveDescendants(child) {
			node.sensitive = true
		}
	}
	return node.sensitive
}

func (v *Vault) classificationTree() *classificationNode {
	if v.classificationIndex == nil {
		v.classificationIndex = buildClassificationTree(v.classifications)
	}
	return v.classificationIndex
}

func (l *Layer) GetRedacted(pointer string) (json.RawMessage, []string, error) {
	parts, err := parsePointer(pointer)
	if err != nil {
		return nil, nil, err
	}
	root, err := l.root()
	if err != nil {
		return nil, nil, err
	}
	value, paths, err := redactedValueAt(root, l.vault.classificationTree(), parts)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := jsonBytes(value)
	return encoded, paths, err
}

func (c *Config) GetRedacted(pointer string) (json.RawMessage, []string, error) {
	parts, err := parsePointer(pointer)
	if err != nil {
		return nil, nil, err
	}
	if c == nil {
		return nil, nil, fmt.Errorf("%w: nil config", ErrInvalidArgument)
	}
	value, paths, err := redactedValueAt(c.root, c.classificationIndex, parts)
	if err != nil {
		return nil, nil, err
	}
	encoded, err := jsonBytes(value)
	return encoded, paths, err
}

func redactedValueAt(root any, classifications *classificationNode, requested []string) (any, []string, error) {
	current := root
	node := classifications
	path := make([]string, 0, len(requested))
	if node != nil && node.classification == Sensitive {
		return nil, []string{""}, nil
	}
	for _, part := range requested {
		object, ok := current.(map[string]any)
		if !ok {
			if node != nil && node.sensitive {
				return nil, []string{pointerFor(path)}, nil
			}
			return nil, nil, fmt.Errorf("%w: JSON Pointer traverses a non-object", ErrInvalidArgument)
		}
		child, exists := object[part]
		if !exists {
			return nil, nil, ErrNotFound
		}
		current = child
		path = append(path, part)
		if node != nil {
			node = node.children[part]
		}
		if node != nil && node.classification == Sensitive {
			return nil, []string{pointerFor(path)}, nil
		}
		if _, isObject := current.(map[string]any); !isObject && node != nil && node.sensitive {
			return nil, []string{pointerFor(path)}, nil
		}
	}
	value, boundaries := redactValue(current, node, path)
	paths := make([]string, len(boundaries))
	for i, boundary := range boundaries {
		paths[i] = pointerFor(boundary)
	}
	sort.Strings(paths)
	return value, paths, nil
}

func redactValue(value any, node *classificationNode, path []string) (any, [][]string) {
	if node != nil && node.classification == Sensitive {
		return nil, [][]string{append([]string(nil), path...)}
	}
	if object, ok := value.(map[string]any); ok {
		redacted := make(map[string]any, len(object))
		var boundaries [][]string
		for key, child := range object {
			var childNode *classificationNode
			if node != nil {
				childNode = node.children[key]
			}
			childPath := appendPointer(path, key)
			redactedChild, childBoundaries := redactValue(child, childNode, childPath)
			redacted[key] = redactedChild
			boundaries = append(boundaries, childBoundaries...)
		}
		return redacted, boundaries
	}
	if node != nil && node.sensitive {
		return nil, [][]string{append([]string(nil), path...)}
	}
	return cloneValue(value), nil
}

func marshalPayload(p payload) ([]byte, error) {
	if err := validatePayload(p); err != nil {
		return nil, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("%w: payload cannot be encoded", ErrInvalidFormat)
	}
	if len(data) > maxPayloadBytes {
		return nil, ErrLimitExceeded
	}
	return data, nil
}

func unmarshalPayload(data []byte) (payload, error) {
	if len(data) > maxPayloadBytes {
		return payload{}, ErrLimitExceeded
	}
	value, err := parseJSON(data)
	if err != nil {
		return payload{}, err
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) != 5 {
		return payload{}, fmt.Errorf("%w: payload must contain exactly the schema v1 fields", ErrInvalidFormat)
	}
	version, ok := object["schema_version"].(json.Number)
	if !ok || version.String() != "1" {
		return payload{}, fmt.Errorf("%w: unsupported payload schema version", ErrInvalidFormat)
	}
	service, ok := object["service"].(string)
	if !ok {
		return payload{}, fmt.Errorf("%w: invalid service field", ErrInvalidFormat)
	}
	defaults, ok := object["defaults"].(map[string]any)
	if !ok {
		return payload{}, fmt.Errorf("%w: defaults must be an object", ErrInvalidFormat)
	}
	environmentValues, ok := object["environments"].(map[string]any)
	if !ok {
		return payload{}, fmt.Errorf("%w: environments must be an object", ErrInvalidFormat)
	}
	environments := make(map[string]map[string]any, len(environmentValues))
	for name, value := range environmentValues {
		root, ok := value.(map[string]any)
		if !ok {
			return payload{}, fmt.Errorf("%w: environment configuration must be an object", ErrInvalidFormat)
		}
		environments[name] = root
	}
	classificationValues, ok := object["classifications"].(map[string]any)
	if !ok {
		return payload{}, fmt.Errorf("%w: classifications must be an object", ErrInvalidFormat)
	}
	classifications := make(map[string]Classification, len(classificationValues))
	for pointer, value := range classificationValues {
		classification, ok := value.(string)
		if !ok {
			return payload{}, fmt.Errorf("%w: invalid classification value", ErrInvalidFormat)
		}
		classifications[pointer] = Classification(classification)
	}
	p := payload{
		SchemaVersion:   1,
		Service:         service,
		Defaults:        defaults,
		Environments:    environments,
		Classifications: classifications,
	}
	if err := validatePayload(p); err != nil {
		return payload{}, err
	}
	return p, nil
}

func validatePayload(p payload) error {
	if p.SchemaVersion != 1 || p.Service == "" || !utf8.ValidString(p.Service) || p.Defaults == nil || p.Environments == nil || p.Classifications == nil {
		return fmt.Errorf("%w: invalid payload fields", ErrInvalidFormat)
	}
	if err := validateJSONTree(p.Defaults, 1); err != nil {
		return err
	}
	if err := validateConfigurationCoverage(p.Defaults, nil, p.Classifications); err != nil {
		return err
	}
	for name, root := range p.Environments {
		if !validEnvironment(name) {
			return fmt.Errorf("%w: invalid environment name", ErrInvalidFormat)
		}
		if root == nil {
			return fmt.Errorf("%w: environment must be an object", ErrInvalidFormat)
		}
		if err := validateJSONTree(root, 2); err != nil {
			return err
		}
		if err := validateConfigurationCoverage(root, nil, p.Classifications); err != nil {
			return err
		}
	}
	for pointer, classification := range p.Classifications {
		if _, err := parsePointer(pointer); err != nil {
			if errors.Is(err, ErrLimitExceeded) {
				return err
			}
			return fmt.Errorf("%w: invalid classification pointer", ErrInvalidFormat)
		}
		if classification != Sensitive && classification != NonSensitive {
			return fmt.Errorf("%w: invalid classification value", ErrInvalidFormat)
		}
	}
	return nil
}

func validateJSONTree(value any, parentDepth int) error {
	switch value := value.(type) {
	case map[string]any:
		depth := parentDepth + 1
		if depth > maxJSONDepth {
			return ErrLimitExceeded
		}
		for key, child := range value {
			if !utf8.ValidString(key) {
				return fmt.Errorf("%w: invalid object key encoding", ErrInvalidFormat)
			}
			if err := validateJSONTree(child, depth); err != nil {
				return err
			}
		}
	case []any:
		depth := parentDepth + 1
		if depth > maxJSONDepth {
			return ErrLimitExceeded
		}
		for _, child := range value {
			if err := validateJSONTree(child, depth); err != nil {
				return err
			}
		}
	case nil, bool, string, json.Number:
		if text, ok := value.(string); ok && !utf8.ValidString(text) {
			return fmt.Errorf("%w: invalid string encoding", ErrInvalidFormat)
		}
	default:
		return fmt.Errorf("%w: unsupported configuration value type", ErrInvalidFormat)
	}
	return nil
}

func validateConfigurationCoverage(root map[string]any, parts []string, classifications map[string]Classification) error {
	for key, value := range root {
		path := appendPointer(parts, key)
		if _, exists := classifications[pointerFor(path)]; !exists {
			return fmt.Errorf("%w: configuration node lacks a classification", ErrInvalidFormat)
		}
		if object, ok := value.(map[string]any); ok {
			if err := validateConfigurationCoverage(object, path, classifications); err != nil {
				return err
			}
		}
	}
	return nil
}

func payloadFromVault(v *Vault) payload {
	return payload{
		SchemaVersion:   1,
		Service:         v.service,
		Defaults:        v.defaults,
		Environments:    v.environments,
		Classifications: v.classifications,
	}
}

func vaultFromPayload(p payload) *Vault {
	return &Vault{
		service:         p.Service,
		defaults:        p.Defaults,
		environments:    p.Environments,
		classifications: p.Classifications,
	}
}
