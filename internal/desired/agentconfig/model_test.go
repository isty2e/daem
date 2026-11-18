package agentconfig_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func local(t *testing.T, path string, mode source.LocalSourceMode) source.Source {
	t.Helper()
	value, err := source.NewLocalSource(path, mode)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func spec(t *testing.T) agentconfig.Spec {
	t.Helper()
	return agentconfig.Spec{
		Name: "review", Source: local(t, "config/review.json", source.LocalSourceModeVendor),
		Target: target.TargetPi, Scope: target.ScopeProject, Path: "extensions/subagent/config.json",
	}
}

func TestAgentConfigKeepsCanonicalIntentWithoutHostPolicy(t *testing.T) {
	for _, selected := range target.SupportedTargets() {
		input := spec(t)
		input.Target = selected
		value, err := agentconfig.New(input)
		if err != nil || value.Validate() != nil {
			t.Fatalf("new config: %v", err)
		}
		if value.ID().Kind() != entity.KindAgentConfig || value.ID().Name() != input.Name || value.Target() != selected || value.Scope() != input.Scope || value.Path() != input.Path {
			t.Fatal("configuration identity and placement intent were conflated")
		}
		if value.Ownership() != agentconfig.OwnershipKeys || value.Syntax() != agentconfig.SyntaxDefault {
			t.Fatal("default ownership/grammar intent lost")
		}
		decoded, err := entity.Parse(value.ID().String())
		if err != nil || decoded != value.ID() {
			t.Fatalf("authored identity round trip: %v", err)
		}
		left, err := source.SourceIDFor(input.Source)
		if err != nil {
			t.Fatal(err)
		}
		right, err := source.SourceIDFor(value.Source())
		if err != nil || left != right {
			t.Fatal("source identity changed")
		}
	}
}

func TestAgentConfigOwnershipAndGrammarIntent(t *testing.T) {
	for _, ownership := range []agentconfig.Ownership{agentconfig.OwnershipKeys, agentconfig.OwnershipDocument} {
		for _, syntax := range []agentconfig.Syntax{agentconfig.SyntaxDefault, agentconfig.SyntaxJSON, agentconfig.SyntaxJSONC, agentconfig.SyntaxJSONLineComments, agentconfig.SyntaxTOML} {
			input := spec(t)
			input.Ownership, input.Syntax = ownership, syntax
			input.Path = "agents/reviewer.md"
			value, err := agentconfig.New(input)
			if err != nil || value.Ownership() != ownership || value.Syntax() != syntax {
				t.Fatalf("intent %s/%s: %v", ownership, syntax, err)
			}
		}
	}
	if _, err := agentconfig.ParseOwnership("unknown"); err == nil {
		t.Fatal("unknown ownership admitted")
	}
	if _, err := agentconfig.ParseSyntax("yaml"); err == nil {
		t.Fatal("unselected grammar admitted")
	}
}

func TestAgentConfigRejectsUnsafeRelativeDestinations(t *testing.T) {
	for _, path := range []string{"", "/settings.json", "../settings.json", "a/../b", "a/./b", "a//b", "a/", "a\\b", "C:/settings.json", "C:settings.json", " a.json", "a.json ", "a\x00.json", "a\n.json", "a\u202e.json", "\xff"} {
		input := spec(t)
		input.Path = path
		if _, err := agentconfig.New(input); err == nil {
			t.Errorf("unsafe relative path %q admitted", path)
		}
	}
	for _, path := range []string{"config.json", "extensions/subagent/config.json", "agents/a b.md", "literal%2e%2e.json", "日本語.json"} {
		input := spec(t)
		input.Path = path
		value, err := agentconfig.New(input)
		if err != nil || value.Path() != path {
			t.Errorf("literal relative path %q changed or refused: %v", path, err)
		}
	}
}

func TestAgentConfigUsesFileSourceContract(t *testing.T) {
	input := spec(t)
	input.Scope = target.ScopeGlobal
	if _, err := agentconfig.New(input); err == nil {
		t.Fatal("global relative local source admitted")
	}
	input.Source = local(t, filepath.Join(t.TempDir(), "not-created.json"), source.LocalSourceModeVendor)
	if _, err := agentconfig.New(input); err != nil {
		t.Fatalf("absolute source intent required filesystem observation: %v", err)
	}
	input.Source = local(t, filepath.Join(t.TempDir(), "config.json"), source.LocalSourceModeLink)
	if _, err := agentconfig.New(input); err == nil {
		t.Fatal("unlocked link source admitted")
	}
	for _, format := range []source.S3ObjectFormat{source.S3ObjectFormatFile, source.S3ObjectFormatTar} {
		value, err := source.NewS3Source("s3://bucket/settings.json", "", "", format)
		if err != nil {
			t.Fatal(err)
		}
		input.Source = value
		_, err = agentconfig.New(input)
		if (err == nil) != (format == source.S3ObjectFormatFile) {
			t.Errorf("S3 file contract %s: %v", format, err)
		}
	}
}

func TestAgentConfigRefusesInvalidIdentityTargetAndScope(t *testing.T) {
	for _, modify := range []func(*agentconfig.Spec){
		func(input *agentconfig.Spec) { input.Name = "" },
		func(input *agentconfig.Spec) { input.Name = "x\ny" },
		func(input *agentconfig.Spec) { input.Target = "invalid" },
		func(input *agentconfig.Spec) { input.Scope = "invalid" },
		func(input *agentconfig.Spec) { input.Source = source.Source{} },
		func(input *agentconfig.Spec) { input.Ownership = "invalid" },
		func(input *agentconfig.Spec) { input.Syntax = "yaml" },
	} {
		input := spec(t)
		modify(&input)
		if _, err := agentconfig.New(input); err == nil {
			t.Fatal("invalid configuration intent admitted")
		}
	}
	if (agentconfig.Config{}).Validate() == nil {
		t.Fatal("zero configuration admitted")
	}
}
