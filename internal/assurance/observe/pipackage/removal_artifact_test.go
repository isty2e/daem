package pipackage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology/extension"
)

func TestObserveRemovalClassifiesManagedArtifactsByFreshFilesystemState(t *testing.T) {
	tests := []struct {
		name     string
		scope    target.Scope
		source   string
		relative string
	}{
		{
			name:     "project npm",
			scope:    target.ScopeProject,
			source:   "npm:@acme/tools@1.2.3",
			relative: filepath.Join("npm", "node_modules", "@acme", "tools"),
		},
		{
			name:     "global npm",
			scope:    target.ScopeGlobal,
			source:   "npm:tools@1.2.3",
			relative: filepath.Join("npm", "node_modules", "tools"),
		},
		{
			name:     "project git",
			scope:    target.ScopeProject,
			source:   "git:github.com/acme/tools@v1",
			relative: filepath.Join("git", "github.com", "acme", "tools"),
		},
		{
			name:     "global git",
			scope:    target.ScopeGlobal,
			source:   "https://github.com/acme/tools.git@v1",
			relative: filepath.Join("git", "github.com", "acme", "tools"),
		},
		{
			name:     "single segment git repository",
			scope:    target.ScopeGlobal,
			source:   "git+https://git.example/repo.git#v1",
			relative: filepath.Join("git", "git.example", "repo"),
		},
		{
			name:     "encoded at sign in git path",
			scope:    target.ScopeGlobal,
			source:   "git+https://github.com/acme/tools%40scope.git",
			relative: filepath.Join("git", "github.com", "acme", "tools@scope"),
		},
		{
			name:     "literal percent in git path",
			scope:    target.ScopeGlobal,
			source:   "git+https://example.com/acme/100%25-tool.git#v1",
			relative: filepath.Join("git", "example.com", "acme", "100%-tool"),
		},
		{
			name:     "plus-host git",
			scope:    target.ScopeGlobal,
			source:   "git:git@short+host:acme/tools.git@v1",
			relative: filepath.Join("git", "short+host", "acme", "tools"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := resolvedTempDir(t)
			projectRoot := filepath.Join(root, "project")
			agentRoot := filepath.Join(root, "agent")
			commandRoot := projectRoot
			settingsBase := agentRoot
			if test.scope == target.ScopeProject {
				settingsBase = filepath.Join(projectRoot, ".pi")
			}
			writePiSettings(t, filepath.Join(settingsBase, "settings.json"), []string{test.source})

			settings := SettingsInput{
				ConfigRoot:  agentRoot,
				WorkDir:     projectRoot,
				ProjectRoot: projectRoot,
				Scope:       test.scope,
			}
			inventory, err := ReadSettings(settings)
			if err != nil {
				t.Fatal(err)
			}
			pending := mustPiPendingRemoval(t, commandRoot, test.scope, test.source, durablecarrier.EffectBaselineSet{})
			source, err := extension.InterpretCarrierSource(pending.Identity().Carrier().Key())
			if err != nil {
				t.Fatal(err)
			}

			assertRemovalEffectState(
				t,
				inventory,
				commandRoot,
				pending,
				source,
				observepostcondition.EvidenceSatisfied,
			)
			observation, err := ObserveRemoval(context.Background(), RemovalObservationInput{
				Settings:    settings,
				CommandRoot: commandRoot,
				Pending:     pending,
			})
			if err != nil {
				t.Fatal(err)
			}
			correlation, observed := observation.Correlation()
			if !observed || correlation.State() != observerelation.StateExactCorrelation {
				t.Fatalf("correlation = (%q, %t), want exact correlation", correlation.State(), observed)
			}

			artifactPath := filepath.Join(settingsBase, test.relative)
			if err := os.MkdirAll(artifactPath, 0o755); err != nil {
				t.Fatal(err)
			}
			assertRemovalEffectState(
				t,
				inventory,
				commandRoot,
				pending,
				source,
				observepostcondition.EvidenceUnsatisfied,
			)

			if err := os.RemoveAll(artifactPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(root, artifactPath); err != nil {
				t.Fatal(err)
			}
			assertRemovalEffectState(
				t,
				inventory,
				commandRoot,
				pending,
				source,
				observepostcondition.EvidenceUnavailable,
			)
		})
	}
}

func TestObserveRemovalNeverTurnsUnsafeOrMalformedEvidenceIntoAbsence(t *testing.T) {
	root := resolvedTempDir(t)
	projectRoot := filepath.Join(root, "project")
	agentRoot := filepath.Join(root, "agent")
	settings := SettingsInput{
		ConfigRoot:  agentRoot,
		WorkDir:     projectRoot,
		ProjectRoot: projectRoot,
		Scope:       target.ScopeProject,
	}
	writePiSettings(t, filepath.Join(projectRoot, ".pi", "settings.json"), nil)
	inventory, err := ReadSettings(settings)
	if err != nil {
		t.Fatal(err)
	}

	unsafePending := mustPiPendingRemoval(
		t,
		projectRoot,
		target.ScopeProject,
		"npm:../../escape",
		durablecarrier.EffectBaselineSet{},
	)
	unsafeSource, err := extension.InterpretCarrierSource(unsafePending.Identity().Carrier().Key())
	if err != nil {
		t.Fatal(err)
	}
	assertRemovalEffectState(
		t,
		inventory,
		projectRoot,
		unsafePending,
		unsafeSource,
		observepostcondition.EvidenceUnavailable,
	)

	uppercaseGit := mustPiPendingRemoval(
		t,
		projectRoot,
		target.ScopeProject,
		"https://EXAMPLE.com/acme/tools.git@v1",
		durablecarrier.EffectBaselineSet{},
	)
	uppercaseSource, err := extension.InterpretCarrierSource(uppercaseGit.Identity().Carrier().Key())
	if err != nil {
		t.Fatal(err)
	}
	lowercaseArtifact := filepath.Join(projectRoot, ".pi", "git", "example.com", "acme", "tools")
	if err := os.MkdirAll(lowercaseArtifact, 0o755); err != nil {
		t.Fatal(err)
	}
	assertRemovalEffectState(
		t,
		inventory,
		projectRoot,
		uppercaseGit,
		uppercaseSource,
		observepostcondition.EvidenceUnavailable,
	)

	if _, err := ObserveRemoval(context.Background(), RemovalObservationInput{
		Settings: SettingsInput{
			ConfigRoot: agentRoot,
			WorkDir:    projectRoot,
			Scope:      target.ScopeGlobal,
		},
		CommandRoot: projectRoot,
		Pending:     unsafePending,
	}); err == nil || !strings.Contains(err.Error(), "does not match carrier scope") {
		t.Fatalf("scope mismatch error = %v", err)
	}

	if err := os.WriteFile(
		filepath.Join(projectRoot, ".pi", "settings.json"),
		[]byte(`{"packages":null}`),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ObserveRemoval(context.Background(), RemovalObservationInput{
		Settings:    settings,
		CommandRoot: projectRoot,
		Pending: mustPiPendingRemoval(
			t,
			projectRoot,
			target.ScopeProject,
			"npm:tools@1.0.0",
			durablecarrier.EffectBaselineSet{},
		),
	}); err == nil || !strings.Contains(err.Error(), "packages must be an array") {
		t.Fatalf("malformed settings error = %v", err)
	}
}

func TestGitInstallRelativeRejectsUnobservableHostsAndControl(t *testing.T) {
	if relative, err := gitInstallRelative("short+host/acme/tools"); err != nil {
		t.Fatalf("plus-host gitInstallRelative: %v", err)
	} else if relative != filepath.FromSlash("short+host/acme/tools") {
		t.Fatalf("plus-host relative = %q", relative)
	}
	for _, identity := range []string{
		"2001:db8::1/acme/tool",
		"example.com/acme/tool\nforged",
	} {
		if _, err := gitInstallRelative(identity); err == nil {
			t.Fatalf("gitInstallRelative(%q) admitted unobservable identity", identity)
		}
	}
}

func assertRemovalEffectState(
	t *testing.T,
	inventory Inventory,
	commandRoot string,
	pending durablecarrier.PendingCarrierRemoval,
	source extension.CarrierSource,
	want observepostcondition.EvidenceState,
) {
	t.Helper()
	evidence, err := removalEffectEvidence(
		context.Background(),
		inventory,
		commandRoot,
		pending.Identity().Carrier().Key(),
		source,
		pending,
	)
	if err != nil {
		t.Fatal(err)
	}
	facts := evidence.Evidence()
	if len(facts) != 1 || facts[0].State() != want {
		t.Fatalf("effect evidence = %#v, want one %q fact", facts, want)
	}
}
