package record

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestNative16BindingsRetainTheirRecordedVersionMeaning(t *testing.T) {
	for _, test := range []struct {
		selected         target.Target
		scope            target.Scope
		alias            string
		identity, policy string
		relative         string
	}{
		{target.TargetPi, target.ScopeGlobal, "@pi-agent/settings.json", "pi-agent", "pi-agent-location-v1", "settings.json"},
		{target.TargetPi, target.ScopeGlobal, "~/.pi/agent/mcp.json", "pi-agent", "pi-agent-location-v1", "mcp.json"},
		{target.TargetCodex, target.ScopeGlobal, "@codex-home/review.config.toml", "codex-home", "codex-home-location-v1", "review.config.toml"},
		{target.TargetCodex, target.ScopeGlobal, "~/.codex/config.toml", "codex-home", "codex-home-location-v1", "config.toml"},
		{target.TargetPi, target.ScopeProject, ".pi/settings.json", "project", "selected-project-v1", ".pi/settings.json"},
		{target.TargetCodex, target.ScopeProject, "retained/document.bin", "project", "selected-project-v1", "retained/document.bin"},
	} {
		t.Run(test.alias, func(t *testing.T) {
			destination, err := output.Parse(test.alias)
			if err != nil {
				t.Fatal(err)
			}
			address, err := aggregate.NewDocumentAddress(test.selected, test.scope, destination)
			if err != nil {
				t.Fatal(err)
			}
			location, err := native16RootBindingForAlias(address)
			reference := location.RootReference()
			if err != nil || reference.Identity() != test.identity || reference.LocatorPolicy() != test.policy || location.RelativePath() != test.relative {
				t.Fatal("historical binding changed", reference, location.RelativePath(), err)
			}
		})
	}
}

func TestNative16BindingsRefuseUnadmittedRootsAndHosts(t *testing.T) {
	for _, test := range []struct {
		selected target.Target
		alias    string
	}{
		{target.TargetPi, "@codex-home/settings.json"},
		{target.TargetCodex, "~/.codex/other.toml"},
		{target.TargetClaudeCode, "~/.claude.json"},
		{target.TargetOpenCode, "~/.config/opencode/opencode.json"},
		{target.TargetAntigravityCLI, "~/.gemini/config/mcp_config.json"},
	} {
		destination, err := output.Parse(test.alias)
		if err != nil {
			t.Fatal(err)
		}
		address, err := aggregate.NewDocumentAddress(test.selected, target.ScopeGlobal, destination)
		if err != nil {
			t.Fatal(err)
		}
		location, err := native16RootBindingForAlias(address)
		if err == nil || location != (configuration.AliasLocation{}) {
			t.Fatal("unsupported native16 alias supplied partial binding facts", test.alias, location, err)
		}
	}
}

func TestNative16ManifestReferenceKeepsItsRecordedVersion(t *testing.T) {
	fixture := nativeJournalFixture(t)
	value := Native{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	reference, err := value.ManifestRootReference()
	if err != nil || reference.Identity() != "project" || reference.LocatorPolicy() != "selected-project-v1" {
		t.Fatal("native16 manifest reference followed a current catalog revision", reference, err)
	}
	if reference, err := (Native{}).ManifestRootReference(); err == nil || reference != (output.RootReference{}) {
		t.Fatal("an uninitialized journal supplied a manifest reference", reference, err)
	}
}

func TestNative16ConsumersDoNotReinterpretUnadmittedBindings(t *testing.T) {
	fixture := nativeJournalFixture(t)
	fixture.record.DocumentGroups[0].Aliases[0].Address = recoveryDocumentAliasRef{
		Target: string(target.TargetClaudeCode), Scope: string(target.ScopeGlobal), Destination: "~/.claude/settings.json",
	}
	value := Native{record: fixture.record, owner: fixture.claims.owner, before: fixture.before, after: fixture.after}
	if roots, err := value.RecordedRootAnchors(); err == nil || roots != nil {
		t.Fatal("native16 census reinterpreted a third-host alias through the current catalog", roots, err)
	}
	if err := fixture.record.validateWriterSpellingPartition(t.Context()); err == nil {
		t.Fatal("native16 writer partition reinterpreted a third-host alias through the current catalog")
	}
}
