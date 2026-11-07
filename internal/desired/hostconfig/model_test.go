package hostconfig_test

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
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

func spec(t *testing.T) hostconfig.Spec {
	t.Helper()
	return hostconfig.Spec{
		Name: "review", Source: local(t, "config/review.json", source.LocalSourceModeVendor),
		Target: target.TargetPi, Scope: target.ScopeProject, Path: "extensions/subagent/config.json",
	}
}

func TestHostConfigKeepsCanonicalIntentWithoutHostPolicy(t *testing.T) {
	for _, selected := range target.SupportedTargets() {
		input := spec(t)
		input.Target = selected
		value, err := hostconfig.New(input)
		if err != nil || value.Validate() != nil {
			t.Fatalf("new config: %v", err)
		}
		if value.ID().Kind() != entity.KindHostConfig || value.ID().Name() != input.Name || value.Target() != selected || value.Scope() != input.Scope || value.Path() != input.Path {
			t.Fatal("configuration identity and placement intent were conflated")
		}
		if value.Ownership() != hostconfig.OwnershipKeys || value.Syntax() != hostconfig.SyntaxDefault {
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

func TestHostConfigOwnershipAndGrammarIntent(t *testing.T) {
	for _, ownership := range []hostconfig.Ownership{hostconfig.OwnershipKeys, hostconfig.OwnershipDocument} {
		for _, syntax := range []hostconfig.Syntax{hostconfig.SyntaxDefault, hostconfig.SyntaxJSON, hostconfig.SyntaxJSONC, hostconfig.SyntaxJSONLineComments, hostconfig.SyntaxTOML} {
			input := spec(t)
			input.Ownership, input.Syntax = ownership, syntax
			input.Path = "agents/reviewer.md"
			value, err := hostconfig.New(input)
			if err != nil || value.Ownership() != ownership || value.Syntax() != syntax {
				t.Fatalf("intent %s/%s: %v", ownership, syntax, err)
			}
		}
	}
	if _, err := hostconfig.ParseOwnership("unknown"); err == nil {
		t.Fatal("unknown ownership admitted")
	}
	if _, err := hostconfig.ParseSyntax("yaml"); err == nil {
		t.Fatal("unselected grammar admitted")
	}
}

func TestHostConfigRejectsUnsafeRelativeDestinations(t *testing.T) {
	for _, path := range []string{"", "/settings.json", "../settings.json", "a/../b", "a/./b", "a//b", "a/", "a\\b", "C:/settings.json", "C:settings.json", " a.json", "a.json ", "a\x00.json", "a\n.json", "a\u202e.json", "\xff"} {
		input := spec(t)
		input.Path = path
		if _, err := hostconfig.New(input); err == nil {
			t.Errorf("unsafe relative path %q admitted", path)
		}
	}
	for _, path := range []string{"config.json", "extensions/subagent/config.json", "agents/a b.md", "literal%2e%2e.json", "日本語.json"} {
		input := spec(t)
		input.Path = path
		value, err := hostconfig.New(input)
		if err != nil || value.Path() != path {
			t.Errorf("literal relative path %q changed or refused: %v", path, err)
		}
	}
}

func TestHostConfigUsesFileSourceContract(t *testing.T) {
	input := spec(t)
	input.Scope = target.ScopeGlobal
	if _, err := hostconfig.New(input); err == nil {
		t.Fatal("global relative local source admitted")
	}
	input.Source = local(t, filepath.Join(t.TempDir(), "not-created.json"), source.LocalSourceModeVendor)
	if _, err := hostconfig.New(input); err != nil {
		t.Fatalf("absolute source intent required filesystem observation: %v", err)
	}
	input.Source = local(t, filepath.Join(t.TempDir(), "config.json"), source.LocalSourceModeLink)
	if _, err := hostconfig.New(input); err == nil {
		t.Fatal("unlocked link source admitted")
	}
	for _, format := range []source.S3ObjectFormat{source.S3ObjectFormatFile, source.S3ObjectFormatTar} {
		value, err := source.NewS3Source("s3://bucket/settings.json", "", "", format)
		if err != nil {
			t.Fatal(err)
		}
		input.Source = value
		_, err = hostconfig.New(input)
		if (err == nil) != (format == source.S3ObjectFormatFile) {
			t.Errorf("S3 file contract %s: %v", format, err)
		}
	}
}

func TestHostConfigRefusesInvalidIdentityTargetAndScope(t *testing.T) {
	for _, modify := range []func(*hostconfig.Spec){
		func(input *hostconfig.Spec) { input.Name = "" },
		func(input *hostconfig.Spec) { input.Name = "x\ny" },
		func(input *hostconfig.Spec) { input.Target = "invalid" },
		func(input *hostconfig.Spec) { input.Scope = "invalid" },
		func(input *hostconfig.Spec) { input.Source = source.Source{} },
		func(input *hostconfig.Spec) { input.Ownership = "invalid" },
		func(input *hostconfig.Spec) { input.Syntax = "yaml" },
	} {
		input := spec(t)
		modify(&input)
		if _, err := hostconfig.New(input); err == nil {
			t.Fatal("invalid configuration intent admitted")
		}
	}
	if (hostconfig.Config{}).Validate() == nil {
		t.Fatal("zero configuration admitted")
	}
}
