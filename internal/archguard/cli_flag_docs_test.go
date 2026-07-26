package archguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestCLIFlagDocumentationMatchesRegistrations(t *testing.T) {
	root := findRepoRoot(t)
	registered, err := registeredCLIFlags(filepath.Join(root, "internal", "cli"))
	if err != nil {
		t.Fatalf("collect registered CLI flags: %v", err)
	}
	documented, err := documentedCLIFlags(filepath.Join(root, "docs", "cli.md"))
	if err != nil {
		t.Fatalf("collect documented CLI flags: %v", err)
	}
	if differences := compareCLIFlagInventories(registered, documented); len(differences) != 0 {
		t.Fatalf("CLI flag documentation drift:\n- %s", strings.Join(differences, "\n- "))
	}
}

func TestCompareCLIFlagInventoriesReportsBothDirections(t *testing.T) {
	registered := map[string][]string{
		"status": {"check", "json"},
	}
	documented := map[string][]string{
		"status": {"json", "verbose"},
	}
	differences := compareCLIFlagInventories(registered, documented)
	for _, want := range []string{
		`status: registered but undocumented flags [--check]`,
		`status: documented but unregistered flags [--verbose]`,
	} {
		if !slices.Contains(differences, want) {
			t.Fatalf("differences = %#v, want %q", differences, want)
		}
	}
}

func registeredCLIFlags(directory string) (map[string][]string, error) {
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, directory, func(info os.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		return nil, err
	}
	cliPackage, ok := packages["cli"]
	if !ok {
		return nil, fmt.Errorf("package cli not found in %q", directory)
	}

	inventory := make(map[string][]string)
	for _, file := range cliPackage.Files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			paths, found, err := commandPathsForFlagSet(function)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", function.Name.Name, err)
			}
			if !found {
				flags, err := registeredFlagsInFunction(function)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", function.Name.Name, err)
				}
				if len(flags) != 0 {
					return nil, fmt.Errorf(
						"%s registers flags outside a command parser: %s",
						function.Name.Name,
						renderFlags(flags),
					)
				}
				continue
			}
			flags, err := registeredFlagsInFunction(function)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", function.Name.Name, err)
			}
			for _, path := range paths {
				if _, duplicate := inventory[path]; duplicate {
					return nil, fmt.Errorf("command path %q has multiple flag owners", path)
				}
				inventory[path] = flags
			}
		}
	}

	versionFlags, err := manuallyParsedVersionFlags(cliPackage)
	if err != nil {
		return nil, err
	}
	inventory["version"] = versionFlags
	return inventory, nil
}

func commandPathsForFlagSet(function *ast.FuncDecl) ([]string, bool, error) {
	var paths []string
	var inspectErr error
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if inspectErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee, ok := call.Fun.(*ast.Ident)
		if !ok || callee.Name != "newCommandFlagSet" {
			return true
		}
		if len(paths) != 0 {
			inspectErr = fmt.Errorf("multiple flag sets in one function")
			return false
		}
		if len(call.Args) == 0 {
			inspectErr = fmt.Errorf("flag set path is missing")
			return false
		}
		literal, ok := call.Args[0].(*ast.CompositeLit)
		if !ok {
			inspectErr = fmt.Errorf("flag set path is not a literal")
			return false
		}
		parts := make([]string, 0, len(literal.Elts))
		for _, element := range literal.Elts {
			value, ok := stringLiteral(element)
			if ok {
				parts = append(parts, value)
				continue
			}
			identifier, ok := element.(*ast.Ident)
			if !ok || len(parts) != 1 || parts[0] != "list" || identifier.Name != "subcommand" {
				inspectErr = fmt.Errorf("unsupported dynamic flag set path")
				return false
			}
			paths = []string{"list outputs", "list resources"}
			return false
		}
		paths = []string{strings.Join(parts, " ")}
		return false
	})
	return paths, len(paths) != 0, inspectErr
}

func registeredFlagsInFunction(function *ast.FuncDecl) ([]string, error) {
	flagNames := make(map[string]struct{})
	var inspectErr error
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if inspectErr != nil {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "flags" {
			return true
		}
		nameIndex, registration := flagNameArgument(selector.Sel.Name)
		if !registration {
			switch selector.Sel.Name {
			case "Arg", "NArg", "Parse", "SetOutput", "Visit":
				return true
			default:
				inspectErr = fmt.Errorf("unclassified FlagSet method %q", selector.Sel.Name)
				return false
			}
		}
		if len(call.Args) <= nameIndex {
			inspectErr = fmt.Errorf("FlagSet.%s is missing its name", selector.Sel.Name)
			return false
		}
		name, ok := stringLiteral(call.Args[nameIndex])
		if !ok || name == "" {
			inspectErr = fmt.Errorf("FlagSet.%s name is not a non-empty literal", selector.Sel.Name)
			return false
		}
		flagNames[name] = struct{}{}
		return true
	})
	if inspectErr != nil {
		return nil, inspectErr
	}
	return sortedSet(flagNames), nil
}

func flagNameArgument(method string) (int, bool) {
	switch method {
	case "Bool", "BoolFunc", "Duration", "Float64", "Func", "Int", "Int64", "String", "Uint", "Uint64":
		return 0, true
	case "BoolVar", "DurationVar", "Float64Var", "Int64Var", "IntVar", "StringVar", "TextVar", "Uint64Var", "UintVar", "Var":
		return 1, true
	default:
		return 0, false
	}
}

func manuallyParsedVersionFlags(cliPackage *ast.Package) ([]string, error) {
	flags := make(map[string]struct{})
	for _, file := range cliPackage.Files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Name.Name != "runVersion" || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil || !strings.HasPrefix(value, "--") || value == "--help" {
					return true
				}
				flags[strings.TrimPrefix(value, "--")] = struct{}{}
				return true
			})
		}
	}
	if len(flags) == 0 {
		return nil, fmt.Errorf("runVersion has no manually parsed long-form flags")
	}
	return sortedSet(flags), nil
}

func documentedCLIFlags(path string) (map[string][]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	heading := slices.Index(lines, "### Command Flag Inventory")
	if heading < 0 {
		return nil, fmt.Errorf("Command Flag Inventory heading is missing")
	}

	inventory := make(map[string][]string)
	for _, line := range lines[heading+1:] {
		if !strings.HasPrefix(line, "|") {
			if len(inventory) != 0 {
				break
			}
			continue
		}
		columns := strings.Split(line, "|")
		if len(columns) != 4 {
			return nil, fmt.Errorf("malformed flag inventory row %q", line)
		}
		command := strings.Trim(strings.TrimSpace(columns[1]), "`")
		flagColumn := strings.TrimSpace(columns[2])
		if command == "Command" || strings.HasPrefix(command, "---") {
			continue
		}
		if command == "" || flagColumn == "" {
			return nil, fmt.Errorf("empty flag inventory row %q", line)
		}
		if _, duplicate := inventory[command]; duplicate {
			return nil, fmt.Errorf("duplicate documented command %q", command)
		}
		var flags []string
		for field := range strings.SplitSeq(flagColumn, ",") {
			flag := strings.Trim(strings.TrimSpace(field), "`")
			if !strings.HasPrefix(flag, "--") || len(flag) == 2 {
				return nil, fmt.Errorf("invalid documented flag %q for %q", flag, command)
			}
			flags = append(flags, strings.TrimPrefix(flag, "--"))
		}
		if !sort.StringsAreSorted(flags) {
			return nil, fmt.Errorf("flags for %q are not sorted", command)
		}
		inventory[command] = flags
	}
	if len(inventory) == 0 {
		return nil, fmt.Errorf("Command Flag Inventory table is empty")
	}
	return inventory, nil
}

func compareCLIFlagInventories(registered map[string][]string, documented map[string][]string) []string {
	commandSet := make(map[string]struct{}, len(registered)+len(documented))
	for command := range registered {
		commandSet[command] = struct{}{}
	}
	for command := range documented {
		commandSet[command] = struct{}{}
	}
	commands := sortedSet(commandSet)

	var differences []string
	for _, command := range commands {
		registeredFlags, registeredCommand := registered[command]
		documentedFlags, documentedCommand := documented[command]
		switch {
		case !registeredCommand:
			differences = append(differences, fmt.Sprintf("%s: documented command has no parser", command))
			continue
		case !documentedCommand:
			differences = append(differences, fmt.Sprintf("%s: registered command is undocumented", command))
			continue
		}
		if missing := cliFlagDifference(registeredFlags, documentedFlags); len(missing) != 0 {
			differences = append(differences, fmt.Sprintf(
				"%s: registered but undocumented flags %s",
				command,
				renderFlags(missing),
			))
		}
		if extra := cliFlagDifference(documentedFlags, registeredFlags); len(extra) != 0 {
			differences = append(differences, fmt.Sprintf(
				"%s: documented but unregistered flags %s",
				command,
				renderFlags(extra),
			))
		}
	}
	return differences
}

func cliFlagDifference(left []string, right []string) []string {
	rightSet := make(map[string]struct{}, len(right))
	for _, value := range right {
		rightSet[value] = struct{}{}
	}
	var difference []string
	for _, value := range left {
		if _, found := rightSet[value]; !found {
			difference = append(difference, value)
		}
	}
	sort.Strings(difference)
	return difference
}

func sortedSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func renderFlags(flags []string) string {
	rendered := make([]string, 0, len(flags))
	for _, flag := range flags {
		rendered = append(rendered, "--"+flag)
	}
	return "[" + strings.Join(rendered, " ") + "]"
}
