package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
	vaultlib "github.com/xifanyan/vault"
)

const maxValueInputBytes = 16 << 20

type sourceKind uint8

const (
	sourceNone sourceKind = iota
	sourceFile
	sourceStdin
	sourcePrompt
	sourceLiteral
)

type inputSource struct {
	kind  sourceKind
	file  string
	value string
}

type invocation struct {
	command           string
	vaultPath         string
	defaultVault      bool
	passphrase        inputSource
	newPassphrase     inputSource
	service           string
	generateFile      string
	envAction         string
	environment       string
	useDefaults       bool
	scopeSet          bool
	includeSecrets    bool
	classification    string
	classificationSet bool
	output            string
	overwrite         bool
	pointer           string
	valueSource       inputSource
}

func main() {
	os.Exit(runWithTerminal(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, true))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithTerminal(args, stdin, stdout, stderr, false)
}

func runWithTerminal(args []string, stdin io.Reader, stdout, stderr io.Writer, allowControlTerminal bool) int {
	inv, err := parseInvocation(args)
	if err != nil {
		if errors.Is(err, errDefaultVaultPath) {
			fmt.Fprintln(stderr, "vault: default vault path is unavailable")
			return 1
		}
		fmt.Fprintln(stderr, "vault: invalid command usage (use --help for syntax)")
		return 2
	}
	if inv.command == "help" {
		if err := writeAll(stdout, []byte(helpText)); err != nil {
			fmt.Fprintln(stderr, "vault: could not write help")
			return 1
		}
		return 0
	}

	terminal := hasTerminal(stdin, allowControlTerminal)
	needsCurrentPassphrase := inv.command != "init" || inv.generateFile == ""
	if needsCurrentPassphrase {
		switch inv.passphrase.kind {
		case sourceNone:
			if !terminal {
				fmt.Fprintln(stderr, "vault: a passphrase source is required in non-interactive use")
				return 2
			}
		case sourcePrompt:
			if !terminal {
				fmt.Fprintln(stderr, "vault: terminal passphrase input is unavailable")
				return 1
			}
		}
	}
	if inv.command == "passwd" {
		switch inv.newPassphrase.kind {
		case sourceNone:
			if !terminal {
				fmt.Fprintln(stderr, "vault: a new passphrase source is required in non-interactive use")
				return 2
			}
		case sourcePrompt:
			if !terminal {
				fmt.Fprintln(stderr, "vault: terminal passphrase input is unavailable")
				return 1
			}
		}
	}

	if err := execute(inv, stdin, stdout, stderr, allowControlTerminal); err != nil {
		fmt.Fprintf(stderr, "vault: %s failed: %s\n", inv.command, safeFailure(err))
		return 1
	}
	return 0
}

func parseInvocation(args []string) (invocation, error) {
	inv := invocation{}
	if len(args) == 0 {
		inv.command = "help"
		return inv, nil
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			inv.command = "help"
			return inv, nil
		}
	}

	i := 0
	vaultSeen := false
	for i < len(args) {
		option := args[i]
		if !strings.HasPrefix(option, "-") {
			break
		}
		switch option {
		case "--vault":
			if vaultSeen || !hasOptionValue(args, i) {
				return invocation{}, errUsage
			}
			vaultSeen = true
			inv.vaultPath = args[i+1]
			i += 2
		case "--passphrase-file":
			if !hasOptionValue(args, i) || args[i+1] == "" || inv.passphrase.kind != sourceNone {
				return invocation{}, errUsage
			}
			inv.passphrase = inputSource{kind: sourceFile, file: args[i+1]}
			i += 2
		case "--passphrase-stdin":
			if inv.passphrase.kind != sourceNone {
				return invocation{}, errUsage
			}
			inv.passphrase = inputSource{kind: sourceStdin}
			i++
		case "--prompt-passphrase":
			if inv.passphrase.kind != sourceNone {
				return invocation{}, errUsage
			}
			inv.passphrase = inputSource{kind: sourcePrompt}
			i++
		default:
			return invocation{}, errUsage
		}
	}
	if i == len(args) {
		return invocation{}, errUsage
	}
	inv.command = args[i]
	rest := args[i+1:]
	if vaultSeen && inv.vaultPath == "" {
		return invocation{}, errUsage
	}
	switch inv.command {
	case "init":
		if err := parseInit(&inv, rest); err != nil {
			return invocation{}, err
		}
	case "env":
		if len(rest) == 0 {
			return invocation{}, errUsage
		}
		inv.envAction = rest[0]
		switch inv.envAction {
		case "list":
			if len(rest) != 1 {
				return invocation{}, errUsage
			}
		case "create", "delete":
			if len(rest) != 2 || startsOption(rest[1]) {
				return invocation{}, errUsage
			}
			inv.environment = rest[1]
		default:
			return invocation{}, errUsage
		}
	case "get":
		if err := parseGet(&inv, rest); err != nil {
			return invocation{}, err
		}
	case "set":
		if err := parseSet(&inv, rest); err != nil {
			return invocation{}, err
		}
	case "delete":
		if err := parseDelete(&inv, rest); err != nil {
			return invocation{}, err
		}
	case "classify":
		if len(rest) != 2 || startsOption(rest[0]) || startsOption(rest[1]) {
			return invocation{}, errUsage
		}
		inv.pointer, inv.classification = rest[0], rest[1]
	case "export":
		if err := parseExport(&inv, rest); err != nil {
			return invocation{}, err
		}
	case "passwd":
		if err := parsePasswd(&inv, rest); err != nil {
			return invocation{}, err
		}
	default:
		return invocation{}, errUsage
	}

	if inv.command == "init" {
		if inv.generateFile != "" && inv.passphrase.kind != sourceNone {
			return invocation{}, errUsage
		}
	} else if inv.passphrase.kind == sourceStdin && inv.command == "set" && inv.valueSource.kind == sourceStdin {
		return invocation{}, errUsage
	}
	if inv.command == "passwd" && inv.passphrase.kind == sourceStdin && inv.newPassphrase.kind == sourceStdin {
		return invocation{}, errUsage
	}
	if !vaultSeen {
		path, err := defaultVaultPath()
		if err != nil {
			return invocation{}, err
		}
		inv.vaultPath = path
		inv.defaultVault = true
	}
	return inv, nil
}

var errUsage = errors.New("invalid command usage")

var errDefaultVaultPath = errors.New("default vault path unavailable")

func defaultVaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %v", errDefaultVaultPath, err)
	}
	return filepath.Join(home, ".vault", "default.vault"), nil
}

func parseInit(inv *invocation, args []string) error {
	seenService, seenGenerate := false, false
	for i := 0; i < len(args); {
		switch args[i] {
		case "--service":
			if seenService || !hasOptionValue(args, i) {
				return errUsage
			}
			seenService = true
			inv.service = args[i+1]
			i += 2
		case "--generate-passphrase-file":
			if seenGenerate || !hasOptionValue(args, i) || args[i+1] == "" {
				return errUsage
			}
			seenGenerate = true
			inv.generateFile = args[i+1]
			i += 2
		default:
			return errUsage
		}
	}
	if !seenService || inv.service == "" {
		return errUsage
	}
	return nil
}

func parseScopeOption(inv *invocation, args []string, i int) (int, error) {
	switch args[i] {
	case "--defaults":
		if inv.scopeSet {
			return 0, errUsage
		}
		inv.scopeSet, inv.useDefaults = true, true
		return 1, nil
	case "--env":
		if inv.scopeSet || !hasOptionValue(args, i) {
			return 0, errUsage
		}
		inv.scopeSet, inv.environment = true, args[i+1]
		return 2, nil
	default:
		return 0, errUsage
	}
}

func parseGet(inv *invocation, args []string) error {
	seenInclude, seenKey := false, false
	i := 0
	for i < len(args) && startsOption(args[i]) {
		if args[i] == "--defaults" || args[i] == "--env" {
			consumed, err := parseScopeOption(inv, args, i)
			if err != nil {
				return err
			}
			i += consumed
			continue
		}
		switch args[i] {
		case "--key":
			if seenKey || !hasOptionValue(args, i) {
				return errUsage
			}
			pointer, err := dottedKeyPointer(args[i+1])
			if err != nil {
				return err
			}
			seenKey, inv.pointer = true, pointer
			i += 2
		case "--include-secrets":
			if seenInclude {
				return errUsage
			}
			seenInclude, inv.includeSecrets = true, true
			i++
		default:
			return errUsage
		}
	}
	if !inv.scopeSet || len(args)-i > 1 || seenKey && i < len(args) {
		return errUsage
	}
	if i < len(args) {
		if startsOption(args[i]) {
			return errUsage
		}
		inv.pointer = args[i]
	}
	return nil
}

func parseSet(inv *invocation, args []string) error {
	seenClassification, seenKey := false, false
	i := 0
	for i < len(args) && startsOption(args[i]) {
		if args[i] == "--defaults" || args[i] == "--env" {
			consumed, err := parseScopeOption(inv, args, i)
			if err != nil {
				return err
			}
			i += consumed
			continue
		}
		switch args[i] {
		case "--key":
			if seenKey || !hasOptionValue(args, i) {
				return errUsage
			}
			pointer, err := dottedKeyPointer(args[i+1])
			if err != nil {
				return err
			}
			seenKey, inv.pointer = true, pointer
			i += 2
		case "--value":
			if inv.valueSource.kind != sourceNone || !hasOptionValue(args, i) {
				return errUsage
			}
			inv.valueSource = inputSource{kind: sourceLiteral, value: args[i+1]}
			i += 2
		case "--value-file":
			if inv.valueSource.kind != sourceNone || !hasOptionValue(args, i) || args[i+1] == "" {
				return errUsage
			}
			inv.valueSource = inputSource{kind: sourceFile, file: args[i+1]}
			i += 2
		case "--value-stdin":
			if inv.valueSource.kind != sourceNone {
				return errUsage
			}
			inv.valueSource = inputSource{kind: sourceStdin}
			i++
		case "--classification":
			if seenClassification || !hasOptionValue(args, i) {
				return errUsage
			}
			seenClassification, inv.classificationSet = true, true
			inv.classification = args[i+1]
			i += 2
		default:
			return errUsage
		}
	}
	if !inv.scopeSet || inv.valueSource.kind == sourceNone {
		return errUsage
	}
	if seenKey {
		if i != len(args) {
			return errUsage
		}
	} else {
		if len(args)-i != 1 || startsOption(args[i]) {
			return errUsage
		}
		inv.pointer = args[i]
	}
	return nil
}

func dottedKeyPointer(key string) (string, error) {
	var pointer strings.Builder
	pointer.Grow(len(key) + 1)
	for part := range strings.SplitSeq(key, ".") {
		if part == "" {
			return "", errUsage
		}
		pointer.WriteByte('/')
		for i := range len(part) {
			switch part[i] {
			case '~':
				pointer.WriteString("~0")
			case '/':
				pointer.WriteString("~1")
			default:
				pointer.WriteByte(part[i])
			}
		}
	}
	return pointer.String(), nil
}

func parseDelete(inv *invocation, args []string) error {
	seenKey := false
	i := 0
	for i < len(args) && startsOption(args[i]) {
		if args[i] == "--defaults" || args[i] == "--env" {
			consumed, err := parseScopeOption(inv, args, i)
			if err != nil {
				return err
			}
			i += consumed
			continue
		}
		if args[i] != "--key" || seenKey || !hasOptionValue(args, i) {
			return errUsage
		}
		pointer, err := dottedKeyPointer(args[i+1])
		if err != nil {
			return err
		}
		seenKey, inv.pointer = true, pointer
		i += 2
	}
	if !inv.scopeSet {
		return errUsage
	}
	if seenKey {
		if i != len(args) {
			return errUsage
		}
	} else {
		if len(args)-i != 1 || startsOption(args[i]) {
			return errUsage
		}
		inv.pointer = args[i]
	}
	return nil
}

func parseExport(inv *invocation, args []string) error {
	seenOutput, seenOverwrite, seenInclude := false, false, false
	for i := 0; i < len(args); {
		switch args[i] {
		case "--output":
			if seenOutput || !hasOptionValue(args, i) || args[i+1] == "" {
				return errUsage
			}
			seenOutput = true
			inv.output = args[i+1]
			i += 2
		case "--overwrite":
			if seenOverwrite {
				return errUsage
			}
			seenOverwrite, inv.overwrite = true, true
			i++
		case "--include-secrets":
			if seenInclude {
				return errUsage
			}
			seenInclude, inv.includeSecrets = true, true
			i++
		default:
			return errUsage
		}
	}
	if inv.overwrite && inv.output == "" {
		return errUsage
	}
	return nil
}

func parsePasswd(inv *invocation, args []string) error {
	for i := 0; i < len(args); {
		switch args[i] {
		case "--new-passphrase-file":
			if inv.newPassphrase.kind != sourceNone || !hasOptionValue(args, i) || args[i+1] == "" {
				return errUsage
			}
			inv.newPassphrase = inputSource{kind: sourceFile, file: args[i+1]}
			i += 2
		case "--new-passphrase-stdin":
			if inv.newPassphrase.kind != sourceNone {
				return errUsage
			}
			inv.newPassphrase = inputSource{kind: sourceStdin}
			i++
		case "--prompt-new-passphrase":
			if inv.newPassphrase.kind != sourceNone {
				return errUsage
			}
			inv.newPassphrase = inputSource{kind: sourcePrompt}
			i++
		default:
			return errUsage
		}
	}
	return nil
}

func startsOption(value string) bool {
	return strings.HasPrefix(value, "-")
}

func hasOptionValue(args []string, index int) bool {
	if index+1 >= len(args) {
		return false
	}
	switch args[index+1] {
	case "--vault", "--passphrase-file", "--passphrase-stdin", "--prompt-passphrase",
		"--service", "--generate-passphrase-file", "--defaults", "--env", "--include-secrets",
		"--key", "--value", "--value-file", "--value-stdin", "--classification", "--output", "--overwrite",
		"--new-passphrase-file", "--new-passphrase-stdin", "--prompt-new-passphrase":
		return false
	default:
		return true
	}
}

func execute(inv invocation, stdin io.Reader, stdout, stderr io.Writer, allowControlTerminal bool) error {
	if inv.command == "init" {
		return executeInit(inv, stdin, stderr, allowControlTerminal)
	}
	v, err := openVault(inv, stdin, stderr, allowControlTerminal)
	if err != nil {
		return err
	}
	switch inv.command {
	case "env":
		switch inv.envAction {
		case "list":
			names := v.Environments()
			if names == nil {
				names = []string{}
			}
			return writeJSON(stdout, names)
		case "create":
			if err := v.CreateEnvironment(inv.environment); err != nil {
				return err
			}
			return v.Save()
		case "delete":
			if err := v.DeleteEnvironment(inv.environment); err != nil {
				return err
			}
			return v.Save()
		}
	case "get":
		return executeGet(v, inv, stdout, stderr)
	case "set":
		layer, err := selectedLayer(v, inv)
		if err != nil {
			return err
		}
		value, err := readValue(inv.valueSource, stdin)
		if err != nil {
			return err
		}
		if err := layer.Set(inv.pointer, value); err != nil {
			return err
		}
		if inv.classificationSet {
			if err := v.Classify(inv.pointer, vaultlib.Classification(inv.classification)); err != nil {
				return err
			}
		}
		return v.Save()
	case "delete":
		layer, err := selectedLayer(v, inv)
		if err != nil {
			return err
		}
		if err := layer.Delete(inv.pointer); err != nil {
			return err
		}
		return v.Save()
	case "classify":
		if err := v.Classify(inv.pointer, vaultlib.Classification(inv.classification)); err != nil {
			return err
		}
		return v.Save()
	case "export":
		data, err := v.ExportJSON(inv.includeSecrets)
		if err != nil {
			return err
		}
		if inv.output == "" {
			return writeAll(stdout, data)
		}
		forbidden := []string{inv.vaultPath}
		if inv.passphrase.kind == sourceFile {
			forbidden = append(forbidden, inv.passphrase.file)
		}
		return vaultlib.WriteExportFile(inv.output, data, inv.overwrite, forbidden...)
	case "passwd":
		return executePasswd(v, inv, stdin, stderr, allowControlTerminal)
	}
	return errUsage
}

func executeInit(inv invocation, stdin io.Reader, stderr io.Writer, allowControlTerminal bool) error {
	return executeInitWithPrompt(inv, stdin, stderr, allowControlTerminal, promptPassphrase)
}

func executeInitWithPrompt(inv invocation, stdin io.Reader, stderr io.Writer, allowControlTerminal bool, prompt func(io.Reader, io.Writer, string, bool) (string, error)) error {
	if inv.defaultVault {
		if err := os.MkdirAll(filepath.Dir(inv.vaultPath), 0o700); err != nil {
			return fmt.Errorf("create default vault directory: %w", err)
		}
	}
	if inv.generateFile != "" {
		aliases, err := pathsAlias(inv.vaultPath, inv.generateFile)
		if err != nil {
			return err
		}
		if aliases {
			return vaultlib.ErrInvalidArgument
		}
		if err := vaultlib.GenerateCredentials(inv.generateFile); err != nil {
			return err
		}
		_, err = vaultlib.Create(inv.vaultPath, inv.generateFile, inv.service)
		return err
	}
	if inv.passphrase.kind == sourceFile {
		_, err := vaultlib.Create(inv.vaultPath, inv.passphrase.file, inv.service)
		return err
	}
	var passphrase string
	var err error
	if inv.passphrase.kind == sourceStdin {
		passphrase, err = readCurrentPassphrase(inv.passphrase, stdin, stderr, allowControlTerminal)
	} else if inv.passphrase.kind == sourcePrompt || inv.passphrase.kind == sourceNone {
		passphrase, err = readConfirmedPassphrase(func(text string) (string, error) {
			return prompt(stdin, stderr, text, allowControlTerminal)
		})
	} else {
		return errUsage
	}
	if err != nil {
		return err
	}
	_, err = vaultlib.CreateWithPassphrase(inv.vaultPath, inv.service, passphrase)
	return err
}

func readConfirmedPassphrase(prompt func(string) (string, error)) (string, error) {
	first, err := prompt("New passphrase: ")
	if err != nil {
		return "", err
	}
	second, err := prompt("Confirm new passphrase: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("new passphrase confirmation mismatch")
	}
	return first, nil
}

func openVault(inv invocation, stdin io.Reader, stderr io.Writer, allowControlTerminal bool) (*vaultlib.Vault, error) {
	if inv.passphrase.kind == sourceFile {
		return vaultlib.Open(inv.vaultPath, inv.passphrase.file)
	}
	passphrase, err := readCurrentPassphrase(inv.passphrase, stdin, stderr, allowControlTerminal)
	if err != nil {
		return nil, err
	}
	return vaultlib.OpenWithPassphrase(inv.vaultPath, passphrase)
}

func readCurrentPassphrase(source inputSource, stdin io.Reader, stderr io.Writer, allowControlTerminal bool) (string, error) {
	switch source.kind {
	case sourceStdin:
		return readPassphraseInput(stdin)
	case sourcePrompt, sourceNone:
		return promptPassphrase(stdin, stderr, "Passphrase: ", allowControlTerminal)
	default:
		return "", errUsage
	}
}

func executePasswd(v *vaultlib.Vault, inv invocation, stdin io.Reader, stderr io.Writer, allowControlTerminal bool) error {
	switch inv.newPassphrase.kind {
	case sourceFile:
		return v.SaveWithCredentials(inv.newPassphrase.file)
	case sourceStdin:
		passphrase, err := readPassphraseInput(stdin)
		if err != nil {
			return err
		}
		return v.SaveWithPassphrase(passphrase)
	case sourcePrompt, sourceNone:
		passphrase, err := readConfirmedPassphrase(func(text string) (string, error) {
			return promptPassphrase(stdin, stderr, text, allowControlTerminal)
		})
		if err != nil {
			return err
		}
		return v.SaveWithPassphrase(passphrase)
	default:
		return errUsage
	}
}

func executeGet(v *vaultlib.Vault, inv invocation, stdout, stderr io.Writer) error {
	var value json.RawMessage
	var redactedPaths []string
	var err error
	if inv.useDefaults {
		layer := v.Defaults()
		if inv.includeSecrets {
			value, err = layer.Get(inv.pointer)
		} else {
			value, redactedPaths, err = layer.GetRedacted(inv.pointer)
		}
	} else {
		var config *vaultlib.Config
		config, err = v.Resolve(inv.environment)
		if err == nil {
			if inv.includeSecrets {
				value, err = config.Get(inv.pointer)
			} else {
				value, redactedPaths, err = config.GetRedacted(inv.pointer)
			}
		}
	}
	if err != nil {
		return err
	}
	if len(redactedPaths) != 0 {
		diagnostic := struct {
			RedactedPaths []string `json:"redacted_paths"`
		}{RedactedPaths: redactedPaths}
		if err := writeJSON(stderr, diagnostic); err != nil {
			return err
		}
	}
	return writeJSON(stdout, value)
}

func selectedLayer(v *vaultlib.Vault, inv invocation) (*vaultlib.Layer, error) {
	if inv.useDefaults {
		return v.Defaults(), nil
	}
	return v.Environment(inv.environment)
}

func readValue(source inputSource, stdin io.Reader) ([]byte, error) {
	if source.kind == sourceLiteral {
		if len(source.value) > maxValueInputBytes {
			return nil, vaultlib.ErrLimitExceeded
		}
		if !utf8.ValidString(source.value) {
			return nil, vaultlib.ErrInvalidArgument
		}
		number := strings.TrimSpace(source.value)
		if number != "" && (number[0] == '-' || number[0] >= '0' && number[0] <= '9') {
			value := []byte(number)
			if json.Valid(value) {
				return value, nil
			}
		}
		value, err := json.Marshal(source.value)
		if len(value) > maxValueInputBytes {
			return nil, vaultlib.ErrLimitExceeded
		}
		return value, err
	}
	if source.kind == sourceStdin {
		return readLimited(stdin, maxValueInputBytes)
	}
	if source.kind != sourceFile {
		return nil, errUsage
	}
	file, err := os.Open(source.file)
	if err != nil {
		return nil, err
	}
	value, readErr := readLimited(file, maxValueInputBytes)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	return value, nil
}

func readLimited(reader io.Reader, limit int) ([]byte, error) {
	if reader == nil {
		return nil, errors.New("input is unavailable")
	}
	value, err := io.ReadAll(io.LimitReader(reader, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(value) > limit {
		return nil, vaultlib.ErrLimitExceeded
	}
	return value, nil
}

func readPassphraseInput(reader io.Reader) (string, error) {
	if reader == nil {
		return "", errors.New("input is unavailable")
	}
	return vaultlib.ReadPassphrase(reader)
}

func promptPassphrase(stdin io.Reader, stderr io.Writer, prompt string, allowControlTerminal bool) (string, error) {
	file, owned := terminalFile(stdin, allowControlTerminal)
	if file == nil {
		return "", errors.New("terminal input is unavailable")
	}
	if owned {
		defer file.Close()
	}
	if err := writeAll(stderr, []byte(prompt)); err != nil {
		return "", err
	}
	secret, err := term.ReadPassword(int(file.Fd()))
	_ = writeAll(stderr, []byte("\n"))
	if err != nil {
		return "", err
	}
	return string(secret), nil
}

func hasTerminal(stdin io.Reader, allowControlTerminal bool) bool {
	file, owned := terminalFile(stdin, allowControlTerminal)
	if file == nil {
		return false
	}
	if owned {
		_ = file.Close()
	}
	return true
}

func terminalFile(stdin io.Reader, allowControlTerminal bool) (*os.File, bool) {
	if file, ok := stdin.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return file, false
	}
	if !allowControlTerminal {
		return nil, false
	}
	path := "/dev/tty"
	if runtime.GOOS == "windows" {
		path = "CONIN$"
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, false
	}
	if !term.IsTerminal(int(file.Fd())) {
		_ = file.Close()
		return nil, false
	}
	return file, true
}

func writeJSON(writer io.Writer, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeAll(writer, append(encoded, '\n'))
}

func writeAll(writer io.Writer, value []byte) error {
	written, err := writer.Write(value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

func safeFailure(err error) string {
	switch {
	case errors.Is(err, vaultlib.ErrAuthentication):
		return "authentication failed"
	case errors.Is(err, vaultlib.ErrNotFound):
		return "not found"
	case errors.Is(err, vaultlib.ErrAlreadyExists), errors.Is(err, fs.ErrExist):
		return "already exists"
	case errors.Is(err, vaultlib.ErrInvalidArgument):
		return "invalid input"
	case errors.Is(err, vaultlib.ErrInvalidFormat):
		return "invalid vault or input format"
	case errors.Is(err, vaultlib.ErrLimitExceeded):
		return "input or vault limit exceeded"
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return "file access failed"
	default:
		return "operation error"
	}
}

func pathsAlias(first, second string) (bool, error) {
	firstAbs, err := filepath.Abs(first)
	if err != nil {
		return false, err
	}
	secondAbs, err := filepath.Abs(second)
	if err != nil {
		return false, err
	}
	firstClean, secondClean := filepath.Clean(firstAbs), filepath.Clean(secondAbs)
	if runtime.GOOS == "windows" {
		if strings.EqualFold(firstClean, secondClean) {
			return true, nil
		}
	} else if firstClean == secondClean {
		return true, nil
	}
	firstInfo, firstErr := os.Stat(firstClean)
	secondInfo, secondErr := os.Stat(secondClean)
	if firstErr != nil && !errors.Is(firstErr, fs.ErrNotExist) {
		return false, firstErr
	}
	if secondErr != nil && !errors.Is(secondErr, fs.ErrNotExist) {
		return false, secondErr
	}
	if firstErr == nil && secondErr == nil {
		return os.SameFile(firstInfo, secondInfo), nil
	}
	return false, nil
}

const helpText = `vault - encrypted service configuration vault

Usage:
  vault [--vault FILE] [passphrase source] COMMAND [command options]

Vault file:
  --vault FILE             Use FILE instead of the default <home>/.vault/default.vault
  Default home is $HOME (or USERPROFILE on Windows).

Passphrase source (mutually exclusive, before COMMAND):
  --passphrase-file FILE   Read a single-line passphrase file
  --passphrase-stdin       Read one passphrase from standard input
  --prompt-passphrase      Read a passphrase without terminal echo
  (If omitted, an interactive terminal is prompted; non-interactive use must be explicit.)

Commands:
  init --service ID [--generate-passphrase-file FILE]
  env list
  env create NAME
  env delete NAME
  get (--defaults | --env NAME) [--include-secrets] [--key KEY | POINTER]
  set (--defaults | --env NAME) (--value TEXT | --value-file FILE | --value-stdin) [--classification CLASS] (--key KEY | POINTER)
  delete (--defaults | --env NAME) (--key KEY | POINTER)
  classify POINTER (sensitive | non-sensitive)
  export [--output FILE] [--overwrite] [--include-secrets]
  passwd [--new-passphrase-file FILE | --new-passphrase-stdin | --prompt-new-passphrase]

Get/set/delete: --key database.host selects the host property inside the database object.
Empty dot-separated segments are rejected; get without a key or pointer reads the root.
Use a positional JSON Pointer for literal dots or empty property names.
--value saves a JSON number when TEXT is a valid JSON number; otherwise it saves a string.
It never infers booleans, null, objects, or arrays. JSON files/stdin retain their existing types.
Command-line values may be visible in process arguments and shell history; use file/stdin for secrets.
Ordinary reads and exports redact sensitive values. --include-secrets explicitly emits plaintext secrets.
Get writes the selected JSON value directly to stdout; redacted_paths diagnostics go to stderr.
Interactive init and passwd prompt twice for a new passphrase; explicit file/stdin sources are read once.
Use --help before a command or after the command name to display this help.
`
