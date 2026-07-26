package clipresent

import (
	"fmt"
	"io"
	"strings"
)

const defaultHelpWidth = 80

// UsageContext contains dynamic facts needed to render static CLI help.
type UsageContext struct {
	SupportedTargets          string
	ImportTargets             string
	MCPAuthoringTargets       string
	MCPAuthoringScopes        string
	MCPAuthoringPlacements    string
	MCPRuntimeProbeTargets    string
	MCPRuntimeProbeScopes     string
	MCPRuntimeProbePlacements string
	Width                     int
}

type helpPage struct {
	Path      string
	Usage     string
	Summary   string
	Sections  []helpSection
	Examples  []string
	Reference string
}

type helpSection struct {
	Title string
	Rows  []helpRow
}

type helpRow struct {
	Label string
	Text  string
}

// PrintUsage renders concise root command discovery.
func PrintUsage(output io.Writer, context UsageContext) {
	width := helpWidth(context.Width)
	lines := []string{
		"daem - manage declarative agent environments",
		"",
		"Usage: daem <command> [options]",
		"",
		"Start",
		"  init       Create a starter manifest.",
		"  import     Build desired state from existing agent configuration.",
		"Author",
		"  add        Add a resource and refresh the lockfile.",
		"  remove     Remove desired state and refresh the lockfile.",
		"Resolve",
		"  lock       Resolve the manifest into an exact lockfile.",
		"  outdated   Check whether locked sources can advance.",
		"Inspect",
		"  list       List declared resources or managed outputs.",
		"  status     Compare desired, locked, managed, and live state.",
		"  version    Show this executable's embedded build identity.",
		"Diagnose",
		"  doctor     Check passive environment prerequisites.",
		"  probe      Run an explicitly authorized runtime check.",
		"Operate",
		"  refresh    Refresh one explicitly selected host extension.",
		"  unmanage   Stop managing an extension while retaining host state.",
		"Reconcile",
		"  apply      Reconcile the locked environment.",
		"  recover    Resolve an interrupted operation.",
		"",
		"First project: daem init",
		"Existing setup: daem import --target <target>",
		"More help: daem help <command>",
	}
	printWrappedLines(output, lines, width)
}

// PrintCommandUsage renders help for one canonical command path.
func PrintCommandUsage(output io.Writer, path []string, context UsageContext) bool {
	page, ok := commandHelpPage(path, context)
	if !ok {
		return false
	}

	width := helpWidth(context.Width)
	printWrappedLines(output, []string{
		page.Path + " - " + page.Summary,
		"",
		"Usage: " + page.Usage,
	}, width)

	for _, section := range page.Sections {
		fmt.Fprintln(output)
		fmt.Fprintln(output, section.Title+":")
		for _, row := range section.Rows {
			printHelpRow(output, row, width)
		}
	}

	if len(page.Examples) != 0 {
		fmt.Fprintln(output)
		fmt.Fprintln(output, "Examples:")
		for _, example := range page.Examples {
			printShellExample(output, example, width)
		}
	}

	if page.Reference != "" {
		fmt.Fprintln(output)
		printWrappedLines(output, []string{"Reference: " + page.Reference}, width)
	}
	return true
}

func printShellExample(output io.Writer, example string, width int) {
	const (
		firstIndent        = "  "
		continuationIndent = "    "
		shellContinuation  = " \\"
	)

	remaining := strings.TrimSpace(example)
	indent := firstIndent
	for len(indent)+len(remaining) > width {
		breakAt := shellExampleBreak(remaining, width-len(indent)-len(shellContinuation))
		if breakAt < 0 {
			break
		}
		fmt.Fprintln(output, indent+strings.TrimSpace(remaining[:breakAt])+shellContinuation)
		remaining = strings.TrimLeft(remaining[breakAt:], " \t")
		indent = continuationIndent
	}
	fmt.Fprintln(output, indent+remaining)
}

func shellExampleBreak(value string, limit int) int {
	best := -1
	singleQuoted := false
	doubleQuoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && !singleQuoted {
			escaped = true
			continue
		}
		if character == '\'' && !doubleQuoted {
			singleQuoted = !singleQuoted
			continue
		}
		if character == '"' && !singleQuoted {
			doubleQuoted = !doubleQuoted
			continue
		}
		if (character == ' ' || character == '\t') && !singleQuoted && !doubleQuoted && index <= limit {
			best = index
		}
	}
	return best
}

func printHelpRow(output io.Writer, row helpRow, width int) {
	if row.Label == "" {
		printWrappedLines(output, []string{"  " + row.Text}, width)
		return
	}
	prefix := "  " + row.Label
	if len(prefix) < 28 {
		prefix += strings.Repeat(" ", 28-len(prefix))
	}
	printWrappedWithContinuation(output, prefix+row.Text, strings.Repeat(" ", len(prefix)), width)
}

func printWrappedLines(output io.Writer, lines []string, width int) {
	for _, line := range lines {
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		printWrappedWithContinuation(output, line, indent, width)
	}
}

func printWrappedWithContinuation(output io.Writer, line string, continuation string, width int) {
	if line == "" || len(line) <= width {
		fmt.Fprintln(output, line)
		return
	}

	leading := line[:len(line)-len(strings.TrimLeft(line, " "))]
	current := leading
	for word := range strings.FieldsSeq(strings.TrimSpace(line)) {
		candidate := word
		if current != "" {
			separator := " "
			if strings.TrimSpace(current) == "" {
				separator = ""
			}
			candidate = current + separator + word
		}
		if strings.TrimSpace(current) != "" && len(candidate) > width {
			fmt.Fprintln(output, current)
			current = continuation + word
			continue
		}
		current = candidate
	}
	if current != "" {
		fmt.Fprintln(output, current)
	}
}

func helpWidth(width int) int {
	if width < 40 {
		return defaultHelpWidth
	}
	return width
}

const (
	manifestDocumentReference = "Manifest Reference for exhaustive declarative configuration."
	cliDocumentReference      = "CLI Reference for workspace selection, output, and exit behavior."
)

func commandHelpPage(path []string, context UsageContext) (helpPage, bool) {
	key := strings.Join(path, " ")
	page, ok := helpPages(context)[key]
	return page, ok
}

func helpPages(context UsageContext) map[string]helpPage {
	workspace := helpRow{"--manifest <path>", "Select a manifest explicitly. Otherwise use an existing ./daem.toml, then the user manifest; parent directories are not searched."}
	target := helpRow{"--target <target>", "Select one target; repeat for multiple targets. Values: " + context.SupportedTargets + ". Comma lists are not accepted."}
	scope := helpRow{"--scope <scope>", "Select project or global scope; repeat only where the command admits multiple scopes."}
	mcpAuthoringTarget := helpRow{"--target <target>", "Select one effective target. Values: " + context.MCPAuthoringTargets + "."}
	mcpAuthoringScope := helpRow{"--scope <scope>", "Select one effective scope. Values: " + context.MCPAuthoringScopes + "."}
	mcpProbeTarget := helpRow{"--target <target>", "Narrow the server to one runtime-probe target. Values: " + context.MCPRuntimeProbeTargets + "."}
	mcpProbeScope := helpRow{"--scope <scope>", "Narrow the server to one runtime-probe scope. Values: " + context.MCPRuntimeProbeScopes + "."}
	dryRun := helpRow{"--dry-run", "Preview without writing or launching runtime effects."}
	jsonOutput := helpRow{"--json", "Emit one schema-versioned JSON document. Mutually exclusive with --verbose."}
	jsonOutputWithDiff := helpRow{"--json", "Emit one schema-versioned JSON document. Mutually exclusive with --verbose and --diff."}
	verbose := helpRow{"--verbose", "Add bounded human-readable evidence. Mutually exclusive with --json."}
	diff := helpRow{"--diff", "Show content changes. Requires --dry-run and is mutually exclusive with --json."}
	check := helpRow{"--check", "Return non-zero when the command's clean predicate fails; output is unchanged."}
	yes := helpRow{"--yes", "Authorize non-interactive runtime effects after the same planning, stale-state, and authority checks."}
	pages := map[string]helpPage{
		"add": groupPage("daem add", "add a resource and refresh the lockfile", []helpRow{
			{"extension", "Host-native extension row."},
			{"instruction", "Instruction source rendered for selected agents."},
			{"hook", "Lifecycle hook command."},
			{"mcp-server", "Standalone stdio MCP server."},
			{"skill", "One local or Git-backed skill."},
			{"skill-group", "Selected skills from one source root."},
		}, "Selector flags accept one token per occurrence; each resource help page states its admitted cardinality. Add writes by default; use --dry-run to preview."),
		"remove": groupPage("daem remove", "remove desired state and refresh the lockfile", []helpRow{
			{"extension", "Remove an extension row."},
			{"instruction", "Remove an instruction declaration."},
			{"hook", "Remove a hook declaration."},
			{"mcp-server", "Remove a standalone MCP row."},
			{"skill", "Remove a skill or skill-group resource."},
		}, "Omitted selectors remove an unambiguous whole resource. Remove writes by default; use --dry-run to preview."),
		"unmanage": groupPage("daem unmanage", "release daem management while retaining host state", []helpRow{
			{"extension", "Release one exact extension relation."},
		}, "Unmanage writes metadata by default and never invokes a host route. Target and scope are safety filters."),
		"list": groupPage("daem list", "enumerate declarations or output ownership", []helpRow{
			{"resources", "Declared resources and stable remove keys."},
			{"outputs", "Managed outputs and conflicting live destinations."},
		}, "Bare list is navigation only; select one inventory basis."),
		"probe": groupPage("daem probe", "run an explicitly authorized active check", []helpRow{
			{"mcp-server", "Launch one locked MCP command and test runtime dimensions."},
		}, "A bare probe never executes. Runtime effects require terminal stdin/stdout/stderr or --yes."),
		"refresh": groupPage("daem refresh", "refresh explicitly selected host state", []helpRow{
			{"extension", "Refresh one exact declared and locked extension relation."},
		}, "A bare refresh never executes. Runtime effects require terminal stdin/stdout/stderr or --yes."),

		"init": leaf("daem init", "daem init [--manifest <path>] [--force] [--dry-run] [--json|--verbose]", "create a starter manifest", nil,
			[]helpRow{workspace, {"--force", "Replace a regular file after entry-identity revalidation."}, dryRun, jsonOutput, verbose},
			[]helpRow{{"", "Without --manifest, create ./daem.toml; never fall back to the user manifest."}, {"", "Writes by default. --dry-run previews the exact destination and content."}},
			[]string{"daem init", "daem init --manifest ~/.config/daem/daem.toml"}, cliDocumentReference),
		"import": leaf("daem import", "daem import --target <target> [--target <target> ...] [options]", "build desired state from existing agent configuration", nil,
			[]helpRow{workspace, target, scope, {"--source-dir <path>", "Destination for copied instruction sources."}, {"--merge", "Merge into an existing selected manifest instead of creating a new one."}, dryRun, diff, jsonOutputWithDiff, verbose},
			[]helpRow{{"", "At least one --target is required. Import targets: " + context.ImportTargets + "."}, {"", "Without --manifest, non-merge import creates ./daem.toml; --merge uses existing-workspace selection."}},
			[]string{"daem import --target codex", "daem import --target codex --target claude-code --dry-run --diff"}, cliDocumentReference),
		"lock": leaf("daem lock", "daem lock [--manifest <path>] [--dry-run] [--json|--verbose]", "resolve the manifest into an exact lockfile", nil,
			[]helpRow{workspace, dryRun, jsonOutput, verbose},
			[]helpRow{{"", "The lockfile is always daem.lock.toml beside the selected manifest."}, {"", "Writes by default; the same floating manifest may resolve differently over time."}},
			[]string{"daem lock", "daem lock --dry-run --verbose"}, cliDocumentReference),
		"outdated": leaf("daem outdated", "daem outdated [--manifest <path>] [--check] [--json|--verbose]", "check whether locked sources can advance", nil,
			[]helpRow{workspace, check, jsonOutput, verbose}, nil,
			[]string{"daem outdated --check"}, cliDocumentReference),
		"status": leaf("daem status", "daem status [--manifest <path>] [--target <target> ...] [--check] [--json|--verbose]", "compare desired, locked, managed, and live state", nil,
			[]helpRow{workspace, target, check, jsonOutput, verbose},
			[]helpRow{{"", "Omitted --target inspects every effective target represented by desired state."}},
			[]string{"daem status", "daem status --target codex --check --json"}, cliDocumentReference),
		"version": leaf("daem version", "daem version [--json]", "show this executable's embedded build identity", nil,
			[]helpRow{{"--json", "Emit the schema-versioned executable identity document."}},
			[]helpRow{{"", "This command is offline, reads no workspace, and is available on every buildable platform."}},
			[]string{"daem version", "daem version --json"}, cliDocumentReference),
		"apply": leaf("daem apply", "daem apply [--manifest <path>] [--target <target> ...] [options]", "reconcile the locked environment", nil,
			[]helpRow{workspace, target, {"--manage-existing", "Record exact-match unmanaged outputs or eligible external carriers as managed."}, dryRun, yes, diff, jsonOutputWithDiff, verbose},
			[]helpRow{{"", "Bare apply requires terminal stdin/stdout/stderr, discloses effects to stdout, then prompts on stderr."}, {"", "Non-interactive apply requires --yes. --json requires --dry-run or --yes."}, {"", "All admitted host and delegated routes are ordinary selected apply work."}},
			[]string{"daem apply --dry-run --diff", "daem apply --target codex --yes --json"}, cliDocumentReference),
		"recover": leaf("daem recover", "daem recover [--manifest <path>] [--dry-run|--yes] [--json|--verbose]", "resolve an interrupted operation", nil,
			[]helpRow{workspace, dryRun, yes, jsonOutput, verbose},
			[]helpRow{{"", "Bare recover requires terminal stdin/stdout/stderr and confirms after stdout disclosure."}, {"", "Non-interactive recovery requires --yes. --json requires --dry-run or --yes."}},
			[]string{"daem recover --dry-run", "daem recover --yes"}, cliDocumentReference),
		"doctor": leaf("daem doctor", "daem doctor [--manifest <path>] [--target <target> ...|--all-targets] [--json|--verbose]", "check passive environment prerequisites", nil,
			[]helpRow{workspace, target, {"--all-targets", "Check every supported target; mutually exclusive with --target."}, jsonOutput, verbose},
			[]helpRow{{"", "With no selectors, use manifest targets when available; otherwise check all supported target environments."}, {"", "Doctor never launches host CLIs, package managers, plugins, MCP servers, credential helpers, or network probes."}},
			[]string{"daem doctor", "daem doctor --all-targets --verbose"}, cliDocumentReference),
	}

	addAuthoringPages(pages, workspace, target, scope, dryRun, diff, jsonOutputWithDiff, verbose, manifestDocumentReference)
	addRemovalPages(pages, workspace, target, scope, dryRun, diff, jsonOutputWithDiff, verbose, manifestDocumentReference)
	addInventoryPages(pages, workspace, target, jsonOutput, verbose, cliDocumentReference)
	pages["add mcp-server"] = leaf("daem add mcp-server", "daem add mcp-server <name> <command> [--arg <value> ...] [options]", "add a standalone stdio MCP server and refresh the lockfile",
		[]helpRow{{"<name>", "Stable MCP server name."}, {"<command>", "Portable executable token; shell command strings are rejected."}},
		[]helpRow{{"--arg <value>", "Ordered argv entry; repeat to preserve ordering and duplicates."}, workspace, mcpAuthoringTarget, mcpAuthoringScope, dryRun, diff, jsonOutputWithDiff, verbose},
		[]helpRow{{"", "Admitted target/scope pairs: " + context.MCPAuthoringPlacements + "."}, {"", "Target omission succeeds only when the manifest identifies one admitted row."}, {"", "Environment mappings and non-stdio transports are manifest-only. Writes by default."}},
		[]string{"daem add mcp-server context7 npx --arg -y --arg @upstash/context7-mcp", "daem add mcp-server local-mcp local-mcp-server --target codex --dry-run"}, manifestDocumentReference)
	pages["probe mcp-server"] = leaf("daem probe mcp-server", "daem probe mcp-server <name> [--manifest <path>] [--target <target>] [--scope <scope>] [options]", "launch one locked MCP command and test runtime dimensions",
		[]helpRow{{"<name>", "Locked MCP server name; target and scope are needed only when the name is ambiguous."}},
		[]helpRow{workspace, mcpProbeTarget, mcpProbeScope, {"--timeout <duration>", "Positive probe timeout such as 30s."}, dryRun, yes, jsonOutput, verbose},
		[]helpRow{{"", "Admitted target/scope pairs: " + context.MCPRuntimeProbePlacements + "."}, {"", "Bare probe requires terminal stdin/stdout/stderr and confirms after stdout disclosure."}, {"", "Non-interactive execution requires --yes."}, {"", "The probe is observational and never mutates manifest, lockfile, ownership, or host configuration."}},
		[]string{"daem probe mcp-server context7 --dry-run", "daem probe mcp-server context7 --target opencode --scope project --yes"}, cliDocumentReference)
	pages["refresh extension"] = leaf("daem refresh extension", "daem refresh extension <id> [--manifest <path>] [--target <target>] [--scope <scope>] [--timeout <duration>] [options]", "refresh one exact declared and locked extension relation",
		[]helpRow{{"<id>", "Exact declared extension id; no bulk or wildcard selection."}},
		[]helpRow{workspace, {"--target <target>", "Require the selected extension to use this exact target."}, {"--scope <scope>", "Require the selected extension to use this exact scope."}, {"--timeout <duration>", "Host-command timeout from 1s through 1h; default 10m."}, dryRun, yes, jsonOutput, verbose},
		[]helpRow{{"", "Refresh never rewrites the manifest, lockfile, or managed ownership."}, {"", "Bare execution requires terminal stdin/stdout/stderr and confirms after complete stdout disclosure."}, {"", "Non-interactive execution requires --yes. JSON mutation writes its authorization disclosure to stderr and one final result document to stdout."}, {"", "The timeout bounds only the delegated host process, not planning, confirmation, observation, or history persistence."}, {"", "A required observer must prove the exact relation before and after execution; an admitted no-observer route reports attempted_unverified."}},
		[]string{"daem refresh extension context7 --dry-run", "daem refresh extension formatter --target opencode --scope project --yes"}, cliDocumentReference)
	pages["unmanage extension"] = leaf("daem unmanage extension", "daem unmanage extension <id> [--manifest <path>] [--target <target>] [--scope <scope>] [--dry-run] [--diff] [--json|--verbose]", "release one exact extension relation while retaining host state",
		[]helpRow{{"<id>", "Exact extension id; no bulk or wildcard selection."}},
		[]helpRow{workspace, {"--target <target>", "Require the selected extension to use this exact target."}, {"--scope <scope>", "Require the selected extension to use this exact scope."}, dryRun, diff, jsonOutputWithDiff, verbose},
		[]helpRow{{"", "Unmanage removes the declaration when present, refreshes the lockfile, and releases only the exact daem claim."}, {"", "It never invokes a host route and always retains host state. Global ambient or manual consumers remain unobservable."}, {"", "Writes by default. Target and scope are safety filters, not redirection."}},
		[]string{"daem unmanage extension context7 --dry-run --diff", "daem unmanage extension formatter --target opencode --scope project"}, cliDocumentReference)
	return pages
}

func addAuthoringPages(pages map[string]helpPage, workspace helpRow, target helpRow, scope helpRow, dryRun helpRow, diff helpRow, jsonOutput helpRow, verbose helpRow, reference string) {
	common := []helpRow{workspace, target, scope, dryRun, diff, jsonOutput, verbose}
	pages["add extension"] = leaf("daem add extension", "daem add extension <id> <source> [options]", "add a host-native extension row and refresh the lockfile",
		[]helpRow{{"<id>", "Stable resource id."}, {"<source>", "One opaque host source or marketplace locator validated for the selected target."}}, common,
		[]helpRow{{"", "Target omission succeeds only when manifest targets and source compatibility identify one admitted row."}, {"", "Writes by default. Source registries and host-specific policy remain manifest-only."}},
		[]string{"daem add extension context7 context7@market --target claude-code", "daem add extension formatter @acme/formatter --target opencode --dry-run --diff"}, reference)
	pages["add instruction"] = leaf("daem add instruction", "daem add instruction <name> <source> [options]", "add an instruction and refresh the lockfile",
		[]helpRow{{"<name>", "Stable instruction name."}, {"<source>", "Local source path accepted by common authoring."}}, common,
		[]helpRow{{"", "Omitted target and scope preserve manifest inheritance."}, {"", "Non-local and target-specific rendering fields are manifest-only. Writes by default."}},
		[]string{"daem add instruction project ./instructions/AGENTS.md", "daem add instruction project ./AGENTS.md --target codex --dry-run"}, reference)
	pages["add hook"] = leaf("daem add hook", "daem add hook <name> <event> <command> [--matcher <matcher>] [--timeout <duration>] [options]", "add a lifecycle hook and refresh the lockfile",
		[]helpRow{{"<name>", "Stable hook name."}, {"<event>", "Canonical hook lifecycle event."}, {"<command>", "One opaque shell-command string."}}, append([]helpRow{{"--matcher <matcher>", "Optional hook matcher."}, {"--timeout <duration>", "Positive duration; must be representable as whole seconds."}}, common...),
		[]helpRow{{"", "Omitted target and scope preserve manifest inheritance."}, {"", "Status messages and target overrides are manifest-only. Writes by default."}},
		[]string{"daem add hook format PostToolUse 'make fmt'", "daem add hook test Stop 'go test ./...' --timeout 2m --dry-run"}, reference)
	pages["add skill"] = leaf("daem add skill", "daem add skill <source> [--path <repo-path>] [--ref <git-ref>] [--name <installed-name>] [options]", "add one skill and refresh the lockfile",
		[]helpRow{{"<source>", "Local path or Git repository locator."}}, append([]helpRow{{"--path <repo-path>", "Skill directory within a Git repository."}, {"--ref <git-ref>", "Strict branch, tag, or full object id."}, {"--name <installed-name>", "Target-visible installed directory name."}}, common...),
		[]helpRow{{"", "Omitted target and scope preserve manifest inheritance."}, {"", "Resource ids, install modes, repair policy, and advanced sources are manifest-only. Writes by default."}},
		[]string{"daem add skill ./skills/review", "daem add skill https://github.com/acme/skills.git --path review --ref v1.2.0 --dry-run"}, reference)
	pages["add skill-group"] = leaf("daem add skill-group", "daem add skill-group <source-root> --member <name> [--member <name> ...] [options]", "add selected skills from one source root and refresh the lockfile",
		[]helpRow{{"<source-root>", "Local root or Git repository locator."}, {"--member <name>", "Required skill member; repeat one token at a time. Duplicates collapse."}}, append([]helpRow{{"--path <repo-path>", "Group root within a Git repository."}, {"--ref <git-ref>", "Strict branch, tag, or full object id."}}, common...),
		[]helpRow{{"", "Selectors, install mode, and repair policy are manifest-only. Writes by default."}},
		[]string{"daem add skill-group ./skills --member review --member test", "daem add skill-group https://github.com/acme/skills.git --member review --ref v1.2.0 --dry-run"}, reference)
}

func addRemovalPages(pages map[string]helpPage, workspace helpRow, target helpRow, scope helpRow, dryRun helpRow, diff helpRow, jsonOutput helpRow, verbose helpRow, reference string) {
	common := []helpRow{workspace, target, scope, dryRun, diff, jsonOutput, verbose}
	resources := []struct {
		kind, operand, summary string
	}{
		{"extension", "<id>", "remove an extension row and refresh the lockfile"},
		{"instruction", "<name>", "remove an instruction and refresh the lockfile"},
		{"hook", "<name>", "remove a hook and refresh the lockfile"},
		{"mcp-server", "<name>", "remove a standalone MCP row and refresh the lockfile"},
		{"skill", "<resource-key>", "remove a skill or skill-group resource and refresh the lockfile"},
	}
	for _, resource := range resources {
		pages["remove "+resource.kind] = leaf("daem remove "+resource.kind, "daem remove "+resource.kind+" "+resource.operand+" [options]", resource.summary,
			[]helpRow{{resource.operand, "Stable key shown by daem list resources."}}, common,
			[]helpRow{{"", "Omitted target and scope remove the unique whole resource; ambiguous matches require enough selectors to identify one row."}, {"", "Writes by default. Carrier uninstall effects remain apply work, not manifest authoring."}},
			[]string{"daem remove " + resource.kind + " example", "daem remove " + resource.kind + " example --target codex --dry-run --diff"}, reference)
	}
}

func addInventoryPages(pages map[string]helpPage, workspace helpRow, target helpRow, jsonOutput helpRow, verbose helpRow, reference string) {
	pages["list resources"] = leaf("daem list resources", "daem list resources [--manifest <path>] [--target <target> ...] [--json|--verbose]", "list declared resources and stable remove keys", nil,
		[]helpRow{workspace, target, jsonOutput, verbose}, nil,
		[]string{"daem list resources", "daem list resources --target codex --json"}, reference)
	pages["list outputs"] = leaf("daem list outputs", "daem list outputs [--manifest <path>] [--target <target> ...] [--json|--verbose]", "list managed outputs and conflicting live destinations", nil,
		[]helpRow{workspace, target, jsonOutput, verbose},
		[]helpRow{{"", "This is an ownership inventory, not a convergence report; use daem status for planned reconciliation."}},
		[]string{"daem list outputs", "daem list outputs --target claude-code --verbose"}, reference)
}

func groupPage(path string, summary string, children []helpRow, rule string) helpPage {
	examples := make([]string, 0, len(children))
	for _, child := range children {
		examples = append(examples, "daem help "+strings.TrimPrefix(path, "daem ")+" "+child.Label)
	}
	return helpPage{
		Path: path, Usage: path + " <resource>", Summary: summary,
		Sections: []helpSection{{Title: "Resources", Rows: children}, {Title: "Shared rules", Rows: []helpRow{{Text: rule}}}},
		Examples: examples, Reference: manifestDocumentReference,
	}
}

func leaf(path string, usage string, summary string, operands []helpRow, options []helpRow, rules []helpRow, examples []string, reference string) helpPage {
	sections := make([]helpSection, 0, 3)
	if len(operands) != 0 {
		sections = append(sections, helpSection{Title: "Operands", Rows: operands})
	}
	if len(options) != 0 {
		sections = append(sections, helpSection{Title: "Options", Rows: options})
	}
	if len(rules) != 0 {
		sections = append(sections, helpSection{Title: "Rules", Rows: rules})
	}
	return helpPage{Path: path, Usage: usage, Summary: summary, Sections: sections, Examples: examples, Reference: reference}
}
