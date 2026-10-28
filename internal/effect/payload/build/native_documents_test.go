package build

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func payloadDocumentConfig(t *testing.T, name string, spec source.Source, selected target.Target, scope target.Scope, path string, syntax hostconfig.Syntax) hostconfig.Config {
	t.Helper()
	config, err := hostconfig.New(hostconfig.Spec{
		Name: name, Source: spec, Target: selected, Scope: scope, Path: path,
		Ownership: hostconfig.OwnershipDocument, Syntax: syntax,
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

func payloadDocumentLock(t *testing.T, root string, environment desired.Environment) lock.File {
	t.Helper()
	resolver, err := sourceresolution.NewResolver(hostOutputTestPaths(t, root))
	if err != nil {
		t.Fatal(err)
	}
	file, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func payloadDocumentInput(t *testing.T, root string, environment desired.Environment, file lock.File, subjects ...topology.SubjectID) Input {
	t.Helper()
	selection, err := targetselection.ForAvailableTargets(target.SupportedTargets(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return Input{Paths: hostOutputTestPaths(t, root), Environment: environment, Lockfile: file, Selection: selection, ManagedPathPayloadSubjects: subjects}
}

func payloadDocumentSubject(t *testing.T, config hostconfig.Config) topology.SubjectID {
	t.Helper()
	subject, err := aggregate.HostConfigDocumentSubject(config.ID(), config.Target(), config.Scope())
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func TestNativeDocumentPayloadUsesActualLockSourceBytesAndPrivateMode(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		target  target.Target
		scope   target.Scope
		path    string
		content []byte
	}{
		{"pi-project-opaque", target.TargetPi, target.ScopeProject, "prompts/review.md", []byte("# literal\r\n\xff\x00\n")},
		{"pi-global-json", target.TargetPi, target.ScopeGlobal, "themes/review.json", []byte("[ 1, false ]\n")},
		{"codex-project-toml", target.TargetCodex, target.ScopeProject, "custom.toml", []byte("# preserve\nanswer=42\n")},
		{"codex-global-empty", target.TargetCodex, target.ScopeGlobal, "prompts/review.md", nil},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "source")
			if err := os.WriteFile(path, fixture.content, 0o700); err != nil {
				t.Fatal(err)
			}
			spec := sourcetest.Local(t, path, source.LocalSourceModeVendor)
			config := payloadDocumentConfig(t, "document", spec, fixture.target, fixture.scope, fixture.path, hostconfig.SyntaxDefault)
			environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
			file := payloadDocumentLock(t, root, environment)
			subject := payloadDocumentSubject(t, config)
			prepared, err := NativeDocumentPayloadSet(t.Context(), payloadDocumentInput(t, root, environment, file, subject))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := prepared.Cleanup(); err != nil {
					t.Error(err)
				}
			})
			value, present := prepared.LookupSubject(subject)
			content, regular := value.File()
			resource, _ := file.Locked.ExactSupplySubject(config.ID())
			identity, _ := resource.MaterializedFileIdentity()
			if !present || !regular || !bytes.Equal(content.Bytes(), fixture.content) || content.Mode() != 0o600 || value.Hash() != identity.ContentHash() {
				t.Fatalf("actual locked source payload lost bytes, mode or identity: present=%t regular=%t mode=%o", present, regular, content.Mode())
			}
			copied := content.Bytes()
			if len(copied) > 0 {
				copied[0] ^= 1
				if !bytes.Equal(content.Bytes(), fixture.content) {
					t.Fatal("payload exposed mutable backing bytes")
				}
			}
			if _, err := os.Stat(filepath.Join(root, "."+string(fixture.target), fixture.path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("payload preparation wrote a host destination: %v", err)
			}
		})
	}
}

func TestNativeDocumentPayloadPartitionsMixedRequestsAndFreezesDefault(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "source.md", "# same source\n")
	spec := sourcetest.Local(t, filepath.Join(root, "source.md"), source.LocalSourceModeVendor)
	config := payloadDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.SyntaxDefault)
	ordinary := instructionResource(t, "ordinary", spec, target.TargetCodex)
	environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}, Instructions: []instructions.Instructions{ordinary}})
	file := payloadDocumentLock(t, root, environment)
	var subjects []topology.SubjectID
	for _, contract := range file.Locked.Subjects() {
		if _, present := contract.Realization(); present {
			subjects = append(subjects, contract.SubjectID())
		}
	}
	input := payloadDocumentInput(t, root, environment, file, subjects...)
	prepared, err := NativeDocumentPayloadSet(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Cleanup()
	if len(subjects) != 2 {
		t.Fatal("fixture lacks ordinary/native requests")
	}
	for _, subject := range subjects {
		if _, present := prepared.LookupSubject(subject); !present {
			t.Fatalf("requested subject silently omitted: %q", subject)
		}
	}
	refused, err := PayloadSet(t.Context(), input)
	if err == nil {
		t.Fatal("default path admitted native requests")
	}
	for _, subject := range subjects {
		if _, present := refused.LookupSubject(subject); present {
			t.Fatal("default refusal leaked ordinary/native payload prefix")
		}
	}
	input.Lockfile.Version = lock.CurrentVersion + 1
	if _, err := NativeDocumentPayloadSet(t.Context(), input); err == nil {
		t.Fatal("native payload preparation admitted an unsupported lock version")
	}
}

func TestNativeDocumentPayloadSkipsUnrequestedSourcesAndRefusesChangedContent(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "source.md", "# locked\n")
	spec := sourcetest.Local(t, filepath.Join(root, "source.md"), source.LocalSourceModeVendor)
	config := payloadDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.SyntaxDefault)
	environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
	file := payloadDocumentLock(t, root, environment)
	subject := payloadDocumentSubject(t, config)
	input := payloadDocumentInput(t, root, environment, file)
	if err := os.Remove(filepath.Join(root, "source.md")); err != nil {
		t.Fatal(err)
	}
	prepared, err := NativeDocumentPayloadSet(t.Context(), input)
	if err != nil {
		t.Fatalf("unrequested native source was resolved: %v", err)
	}
	defer prepared.Cleanup()
	if _, present := prepared.LookupSubject(subject); present {
		t.Fatal("unrequested source materialized")
	}
	writeHostOutputTestFile(t, root, "source.md", "# changed\n")
	input.ManagedPathPayloadSubjects = []topology.SubjectID{subject}
	refused, err := NativeDocumentPayloadSet(t.Context(), input)
	if err == nil {
		t.Fatal("changed source accepted with old lock")
	}
	if _, present := refused.LookupSubject(subject); present {
		t.Fatal("changed source returned a usable payload")
	}
}

func TestNativeDocumentPayloadPreflightRejectsStaleIntentAndBadRequests(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "source", "answer=42\n")
	spec := sourcetest.Local(t, filepath.Join(root, "source"), source.LocalSourceModeVendor)
	config := payloadDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "custom.conf", hostconfig.SyntaxDefault)
	environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
	file := payloadDocumentLock(t, root, environment)
	subject := payloadDocumentSubject(t, config)
	otherSource := sourcetest.Local(t, filepath.Join(root, "absent-source"), source.LocalSourceModeVendor)
	for _, changed := range []hostconfig.Config{
		payloadDocumentConfig(t, "native", spec, target.TargetCodex, config.Scope(), config.Path(), config.Syntax()),
		payloadDocumentConfig(t, "native", spec, config.Target(), target.ScopeGlobal, config.Path(), config.Syntax()),
		payloadDocumentConfig(t, "native", spec, config.Target(), config.Scope(), "other.conf", config.Syntax()),
		payloadDocumentConfig(t, "native", spec, config.Target(), config.Scope(), config.Path(), hostconfig.SyntaxTOML),
		payloadDocumentConfig(t, "native", otherSource, config.Target(), config.Scope(), config.Path(), config.Syntax()),
	} {
		input := payloadDocumentInput(t, root, hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{changed}}), file, subject)
		if _, _, err := preflightNativeDocumentPayloads(t.Context(), input); err == nil {
			t.Fatal("stale intent passed pure preflight")
		}
		if _, err := NativeDocumentPayloadSet(t.Context(), input); err == nil {
			t.Fatal("stale declaration produced payload")
		}
	}
	foreignID, err := entity.New(entity.KindHostConfig, "foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := aggregate.HostConfigDocumentSubject(foreignID, target.TargetPi, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("key")
	if err != nil {
		t.Fatal(err)
	}
	keySubject, err := topologyprojection.HostConfigKeySubject(config.ID(), config.Target(), config.Scope(), key)
	if err != nil {
		t.Fatal(err)
	}
	for _, requests := range [][]topology.SubjectID{{subject, subject}, {foreign}, {keySubject}, {subject, foreign}} {
		input := payloadDocumentInput(t, root, environment, file, requests...)
		prepared, err := NativeDocumentPayloadSet(t.Context(), input)
		if err == nil {
			t.Fatalf("invalid request admitted: %#v", requests)
		}
		if _, present := prepared.LookupSubject(subject); present {
			t.Fatal("bad request returned usable native prefix")
		}
	}
	input := payloadDocumentInput(t, root, hostOutputEnvironment(t, desired.Spec{}), file, subject)
	if _, err := NativeDocumentPayloadSet(t.Context(), input); err == nil {
		t.Fatal("missing declaration was ignored")
	}
}

func TestNativeDocumentPayloadLateFailureDiscardsOrdinaryAndNativePrefix(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "first.md", "# first\n")
	writeHostOutputTestFile(t, root, "second.md", "# second\n")
	firstSource := sourcetest.Local(t, filepath.Join(root, "first.md"), source.LocalSourceModeVendor)
	secondSource := sourcetest.Local(t, filepath.Join(root, "second.md"), source.LocalSourceModeVendor)
	first := payloadDocumentConfig(t, "a-first", firstSource, target.TargetPi, target.ScopeProject, "prompts/first.md", hostconfig.SyntaxDefault)
	second := payloadDocumentConfig(t, "z-second", secondSource, target.TargetPi, target.ScopeProject, "prompts/second.md", hostconfig.SyntaxDefault)
	ordinary := instructionResource(t, "ordinary", firstSource, target.TargetCodex)
	environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{first, second}, Instructions: []instructions.Instructions{ordinary}})
	file := payloadDocumentLock(t, root, environment)
	var subjects []topology.SubjectID
	for _, contract := range file.Locked.Subjects() {
		if _, present := contract.Realization(); present {
			subjects = append(subjects, contract.SubjectID())
		}
	}
	writeHostOutputTestFile(t, root, "second.md", "# changed\n")
	input := payloadDocumentInput(t, root, environment, file, subjects...)
	prepared, err := NativeDocumentPayloadSet(t.Context(), input)
	if err == nil {
		t.Fatal("late changed source admitted")
	}
	for _, subject := range subjects {
		if _, present := prepared.LookupSubject(subject); present {
			t.Fatal("late native failure returned earlier payload prefix")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NativeDocumentPayloadSet(ctx, input); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preparation: %v", err)
	}
}

func TestNativeDocumentPayloadRejectsChangedSourceExecutableClass(t *testing.T) {
	root := t.TempDir()
	writeHostOutputTestFile(t, root, "source.md", "# same bytes\n")
	path := filepath.Join(root, "source.md")
	spec := sourcetest.Local(t, path, source.LocalSourceModeVendor)
	config := payloadDocumentConfig(t, "native", spec, target.TargetPi, target.ScopeProject, "prompts/review.md", hostconfig.SyntaxDefault)
	environment := hostOutputEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}})
	file := payloadDocumentLock(t, root, environment)
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	observed, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Mode().Perm()&0o111 == 0 {
		t.Skip("filesystem does not expose an executable class")
	}
	subject := payloadDocumentSubject(t, config)
	prepared, err := NativeDocumentPayloadSet(t.Context(), payloadDocumentInput(t, root, environment, file, subject))
	if err == nil {
		t.Fatal("same bytes with changed executable identity accepted")
	}
	if _, present := prepared.LookupSubject(subject); present {
		t.Fatal("changed executable source returned a payload")
	}
}
