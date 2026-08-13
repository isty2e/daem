package adopt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

func TestBuildPlanManifestContentMatchesAdoptionRenderer(t *testing.T) {
	tempDir := t.TempDir()
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	if err := os.WriteFile("AGENTS.md", []byte("# Agents\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(tempDir, "daem.imported.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, filepath.Join(tempDir, "daem.imported.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetCodex},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := BuildPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := adoptmodel.RenderManifestContent(
		plan.Sources(),
		plan.Skills(),
		plan.Hooks(),
		plan.MCPServers(),
		plan.Extensions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(plan.ManifestContent()) != string(rendered) {
		t.Fatalf("manifest content diverged from renderer:\nplan:\n%s\nrendered:\n%s", plan.ManifestContent(), rendered)
	}
}

func TestExtensionImportPlanAndExecuteWriteOnlyManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := []byte(`{"packages":["npm:@acme/tools@1.2.3"]}`)
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		output,
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Extensions()) != 1 ||
		plan.Extensions()[0].Source().Ref() != "npm:@acme/tools@1.2.3" {
		t.Fatalf("extensions = %#v", plan.Extensions())
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("planning wrote output manifest: %v", err)
	}

	executed, err := ExecuteCommandPlan(
		context.Background(),
		CommandPlan{request: request, plan: plan},
	)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "[[extension]]") ||
		!strings.Contains(string(content), `host_source = "npm:@acme/tools@1.2.3"`) {
		t.Fatalf("written manifest lacks exact extension:\n%s", content)
	}
	currentSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(currentSettings, settings) {
		t.Fatalf("extension import changed host settings:\n%s", currentSettings)
	}
	paths, err := daempaths.Resolve(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{
		paths.LockfilePath,
		paths.StatefilePath,
		paths.CarrierClaimRegistryPath,
	} {
		if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
			t.Fatalf("extension import wrote forbidden artifact %q: %v", forbidden, err)
		}
	}
	if executed.AdoptionPlan().ResourceCount() != 1 {
		t.Fatalf(
			"executed resource count = %d, want 1",
			executed.AdoptionPlan().ResourceCount(),
		)
	}
}

func TestExtensionImportRefusesChangedInventoryWithoutWritingManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		settingsPath,
		[]byte(`{"packages":["npm:@acme/tools@1.2.3"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		output,
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		settingsPath,
		[]byte(`{"packages":["npm:@acme/other@4.5.6"]}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, err = ExecuteCommandPlan(
		context.Background(),
		CommandPlan{request: request, plan: plan},
	)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteCommandPlan error = %v, want StaleSnapshotError", err)
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("stale extension import wrote manifest: %v", statErr)
	}
}

func TestBuildPlanSkipsOpenCodeMCPWhenAlternateConfigExistsWithoutWriting(t *testing.T) {
	root := t.TempDir()
	previousWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousWorkingDirectory); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	primary := []byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}}}`)
	alternate := []byte(`{"mcp":{}}`)
	if err := os.WriteFile(aggregate.OpenCodeProjectMCPConfigPath, primary, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("opencode.jsonc", alternate, 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetOpenCode},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = BuildPlan(context.Background(), request)
	if !errors.Is(err, adoptmodel.ErrNothingToImport) || !strings.Contains(err.Error(), "unsupported_mcp_alternate_config") {
		t.Fatalf("BuildPlan error = %v, want alternate-config skip", err)
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("alternate-config import wrote manifest: %v", statErr)
	}
	for path, want := range map[string][]byte{
		aggregate.OpenCodeProjectMCPConfigPath: primary,
		"opencode.jsonc":                       alternate,
	} {
		got, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("import changed host config %q: %q", path, got)
		}
	}
}

func TestExecuteCommandPlanRejectsMCPSourceContentDriftOutsideProjection(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	before := []byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}},"theme":"one"}`)
	after := []byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}},"theme":"two"}`)
	if err := os.WriteFile(aggregate.OpenCodeProjectMCPConfigPath, before, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "daem.toml")
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"opencode"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aggregate.OpenCodeProjectMCPConfigPath, after, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExecuteCommandPlan(t.Context(), planned)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteCommandPlan error = %v, want stale MCP source", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale MCP source published manifest: %v", statErr)
	}
}

func TestExecuteCommandPlanRejectsIdenticalMCPSourceReplacement(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	content := []byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}}}`)
	if err := os.WriteFile(aggregate.OpenCodeProjectMCPConfigPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "daem.toml")
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"opencode"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(aggregate.OpenCodeProjectMCPConfigPath, "displaced-opencode.json"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(aggregate.OpenCodeProjectMCPConfigPath, content, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExecuteCommandPlan(t.Context(), planned)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteCommandPlan error = %v, want stale replacement", err)
	}
	if _, statErr := os.Lstat(output); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("replacement MCP source published manifest: %v", statErr)
	}
}

func TestExecuteCommandPlanRetainsNoopMCPSourceAuthority(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	if err := os.Mkdir(".codex", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		aggregate.OpenCodeProjectMCPConfigPath,
		[]byte(`{"mcp":{"context7":{"type":"local","command":["npx"]}}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		aggregate.CodexProjectMCPConfigPath,
		[]byte("[mcp_servers.other]\ncommand = \"node\"\nargs = [\"server.js\"]\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "daem.toml")
	original := []byte(`version = 1
targets = ["opencode", "codex"]

[[mcp_server]]
name = "context7"
targets = ["opencode"]
scope = "project"
transport = "stdio"
command = "npx"
`)
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"opencode", "codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		Merge:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(planned.AdoptionPlan().MCPServers()); got != 1 {
		t.Fatalf("writable MCP candidates = %d, want only the Codex addition", got)
	}
	if got := len(planned.AdoptionPlan().MCPSourceAuthorities()); got != 2 {
		t.Fatalf("MCP source authorities = %d, want noop and add routes", got)
	}
	if err := os.Mkdir("opencode.jsonc", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("opencode.jsonc", "unrelated"), []byte("large tree must not be read"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = ExecuteCommandPlan(t.Context(), planned)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("ExecuteCommandPlan error = %v, want stale noop authority", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("stale noop authority changed manifest: %q", current)
	}
}

func TestExecuteCommandPlanRejectsMergeTargetSkillRouteDriftAfterRebuild(t *testing.T) {
	root := enterAdoptTestDirectory(t)
	firstSource := filepath.Join(root, "skill-sources", "first")
	secondSource := filepath.Join(root, "skill-sources", "second")
	for _, source := range []string{firstSource, secondSource} {
		if err := os.MkdirAll(source, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("same skill\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	claudeLink := filepath.Join(root, ".claude", "skills", "review")
	codexLink := filepath.Join(root, ".agents", "skills", "review")
	for _, link := range []string{claudeLink, codexLink} {
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(firstSource, link); err != nil {
			t.Fatal(err)
		}
	}

	output := filepath.Join(root, "daem.toml")
	initial, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"claude-code"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	original := initial.AdoptionPlan().ManifestContent()
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}

	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		Merge:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(planned.AdoptionPlan().Skills()); got != 0 {
		t.Fatalf("writable skill candidates = %d, want target-only merge", got)
	}
	if got := len(planned.AdoptionPlan().SkillSourceAuthorities()); got != 1 {
		t.Fatalf("skill source authorities = %d, want merge-target route", got)
	}

	_, err = executeCommandPlan(
		t.Context(),
		planned,
		func(ctx context.Context, request adoptmodel.Request) (adoptmodel.Plan, error) {
			current, buildErr := BuildPlan(ctx, request)
			if buildErr != nil {
				return adoptmodel.Plan{}, buildErr
			}
			if removeErr := os.Remove(codexLink); removeErr != nil {
				return adoptmodel.Plan{}, removeErr
			}
			if linkErr := os.Symlink(secondSource, codexLink); linkErr != nil {
				return adoptmodel.Plan{}, linkErr
			}
			return current, nil
		},
	)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("executeCommandPlan error = %v, want stale skill source route", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("stale merge-target route changed manifest: %q", current)
	}
}

func TestBuildCommandPlanNoopsLockedSelectorBackedSkillMember(t *testing.T) {
	output, _, original := selectorSkillMergeFixture(t, true)

	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		Merge:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	results := planned.AdoptionPlan().MergeResults()
	if len(results) != 2 {
		t.Fatalf("merge results = %#v, want selector-backed skill noop", results)
	}
	for _, result := range results {
		if result.Status != adoptmodel.MergeStatusNoop {
			t.Fatalf("merge result = %#v, want selector-backed skill noop", result)
		}
	}
	if len(planned.AdoptionPlan().Skills()) != 0 {
		t.Fatalf("writable skills = %#v, want no direct skill addition", planned.AdoptionPlan().Skills())
	}
	if !bytes.Equal(planned.AdoptionPlan().ManifestContent(), original) {
		t.Fatal("selector-backed skill noop changed manifest")
	}
}

func TestBuildCommandPlanRequiresLockForSelectorBackedSkillMember(t *testing.T) {
	output, _, original := selectorSkillMergeFixture(t, false)

	_, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		Merge:        true,
	})
	if err == nil || !strings.Contains(err.Error(), "run daem lock") {
		t.Fatalf("BuildCommandPlan error = %v, want missing lock guidance", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("missing selector membership authority changed manifest")
	}
}

func TestExecuteCommandPlanRejectsSelectorMembershipLockDriftAfterRebuild(t *testing.T) {
	output, lockfilePath, original := selectorSkillMergeFixture(t, true)
	planned, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
		Merge:        true,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = executeCommandPlan(
		t.Context(),
		planned,
		func(ctx context.Context, request adoptmodel.Request) (adoptmodel.Plan, error) {
			current, buildErr := BuildPlan(ctx, request)
			if buildErr != nil {
				return adoptmodel.Plan{}, buildErr
			}
			if writeErr := os.WriteFile(lockfilePath, []byte("not a lockfile\n"), 0o600); writeErr != nil {
				return adoptmodel.Plan{}, writeErr
			}
			return current, nil
		},
	)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("executeCommandPlan error = %v, want stale selector membership authority", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("stale selector membership authority changed manifest")
	}
}

func selectorSkillMergeFixture(
	t *testing.T,
	writeLock bool,
) (string, string, []byte) {
	t.Helper()
	root := enterAdoptTestDirectory(t)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg-state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "xdg-cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg-data"))

	for _, name := range []string{"other", "review"} {
		skillRoot := filepath.Join(root, ".agents", "skills", name)
		if err := os.MkdirAll(skillRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		skillContent := []byte(fmt.Sprintf(
			"---\nname: %s\ndescription: %s skill\n---\n\n%s skill.\n",
			name,
			name,
			name,
		))
		if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), skillContent, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	output := filepath.Join(root, "daem.toml")
	initial, err := BuildCommandPlan(t.Context(), CommandInput{
		TargetValues: []string{"codex"},
		ScopeValues:  []string{"project"},
		ManifestPath: output,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecuteCommandPlan(t.Context(), initial); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(original), "\n")
	replacedNames := false
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "names = [") {
			lines[index] = "include = [\"glob:*\"]"
			replacedNames = true
		}
	}
	if !replacedNames {
		t.Fatalf("initial import manifest has no explicit skill_group names:\n%s", original)
	}
	original = []byte(strings.Join(lines, "\n"))
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(output)
	if err != nil {
		t.Fatal(err)
	}
	if writeLock {
		if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{
			ManifestPath: output,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return output, paths.LockfilePath, original
}

func enterAdoptTestDirectory(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
	return root
}

func TestMalformedExtensionInventoryProducesNoPartialManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))
	settingsPath := filepath.Join(root, ".pi", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, []byte(`{"packages":[`), 0o600); err != nil {
		t.Fatal(err)
	}

	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		output,
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		false,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPlan(context.Background(), request); err == nil {
		t.Fatal("BuildPlan accepted malformed Pi extension inventory")
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		t.Fatalf("malformed extension import wrote manifest: %v", statErr)
	}
}

func TestBuildPlanRejectsMultiTargetExtensionBeforeImportMerge(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg-config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))

	output := filepath.Join(root, "daem.toml")
	original := []byte(`version = 1
targets = ["pi", "opencode"]

[[extension]]
id = "invalid-shared"
carrier = "pi-package"
targets = ["pi", "opencode"]
scope = "project"
source = { host_source = "npm:@acme/tool@1.2.3" }
`)
	if err := os.WriteFile(output, original, 0o600); err != nil {
		t.Fatal(err)
	}
	sourceDirectory, err := adoptmodel.NewSourceDirectory(
		output,
		filepath.Join(root, "daem.d"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest(
		[]target.Target{target.TargetPi},
		[]target.Scope{target.ScopeProject},
		output,
		sourceDirectory,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}

	_, err = BuildPlan(context.Background(), request)
	if err == nil || !strings.Contains(err.Error(), "supports exactly one target") {
		t.Fatalf("BuildPlan error = %v, want multi-target extension rejection", err)
	}
	current, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("rejected merge changed manifest:\n%s", current)
	}
}
