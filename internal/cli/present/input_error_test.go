package clipresent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/backend/localfs"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func TestInputGuidanceUsesOwnedFailuresNotText(t *testing.T) {
	for _, message := range []string{
		"--ref is required for git sources",
		"unknown manifest key \"other\"",
		"malformed TOML structure: unclosed array",
		"invalid skill; repairability=manual",
		"failure\nnext: injected",
	} {
		if got := Error(errors.New(message)); got != Escape(message) {
			t.Fatalf("unowned error gained guidance: %q", got)
		}
	}
	invalidUTF8 := tomlstrict.Admit(context.Background(), []byte{0xff}, tomlstrict.StandardLimits())
	if got := Error(invalidUTF8); got != Escape(invalidUTF8.Error()) {
		t.Fatalf("encoding failure got syntax advice: %q", got)
	}
	_, unknown := declaration.DecodeManifest([]byte("version=1\n\"bad\\nnext: injected\"=true\n"))
	if unknown == nil {
		t.Fatal("unknown key accepted")
	}
	if evidence := BoundedErrorEvidence(unknown, 4096); !strings.Contains(evidence, "unknown manifest key") || strings.Contains(evidence, "omitted") {
		t.Fatalf("lost bounded cause evidence: %q", evidence)
	}
	if evidence := BoundedErrorEvidence(authoring.ErrMissingGitRef, 4096); evidence != authoring.ErrMissingGitRef.Error() {
		t.Fatalf("lost bounded ref evidence: %q", evidence)
	}
	got := Error(fmt.Errorf("invalid manifest: %w", unknown))
	if !strings.Contains(got, "next: correct or remove") || strings.Contains(got, "\nnext: injected") {
		t.Fatalf("unsafe/missing guidance: %q", got)
	}
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("dynamic key created output lines: %q", got)
	}
}

func TestAuthoringErrorProjectionPreservesOtherPhasesAndOuterContext(t *testing.T) {
	cause := errors.New("source unavailable\nnext: injected")
	for _, phase := range []authoring.OperationPhase{authoring.OperationPhaseLoadManifest, authoring.OperationPhaseBuildManifestChange, authoring.OperationPhaseCommit} {
		err := authoring.OperationError{Phase: phase, Err: cause}
		if got := Error(err); got != Escape(err.Error()) {
			t.Fatalf("phase %s context changed: %q", phase, got)
		}
	}
	building := authoring.OperationError{Phase: authoring.OperationPhaseBuildLockfile, Err: cause}
	if got := Error(building); got != Escape(cause.Error()) {
		t.Fatalf("build projection=%q", got)
	}
	if building.Error() != "lock prospective manifest: source unavailable\nnext: injected" || !errors.Is(building, cause) {
		t.Fatal("raw authoring error changed")
	}
	outer := fmt.Errorf("outer operation: %w", building)
	if got := Error(outer); got != Escape(outer.Error()) {
		t.Fatalf("outer context lost: %q", got)
	}
}

func TestRepairGuidanceKeepsDistinctReasonsAndOuterFailures(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("---\nname: invalid name\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolver, err := localfs.NewResolver(root)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := resolver.Resolve(context.Background(), sourcetest.Local(t, ".", source.LocalSourceModeVendor), acquisition.OperationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	classification, err := skillrepair.Classify(context.Background(), resolved.Identity(), resolved.View(), "invalid name", []target.Target{target.TargetOpenCode})
	if err != nil {
		t.Fatal(err)
	}
	reasons := classification.ManualReasons()
	if len(reasons) < 2 {
		t.Fatalf("fixture needs distinct reasons: %#v", reasons)
	}
	cause := errors.New("validate skill: " + reasons[0] + "\nnext: injected")
	guidance := skillrepair.WithGuidance(cause, classification)
	message := Error(guidance)
	for _, reason := range reasons {
		if strings.Count(message, Escape(reason)) != 1 {
			t.Fatalf("lost/duplicated reason %q in %q", reason, message)
		}
	}
	if strings.Contains(message, "\nnext: injected") || !strings.Contains(message, manualSkillRepairHint) {
		t.Fatalf("guidance=%q", message)
	}
	for _, wrapped := range []error{fmt.Errorf("outer failure: %w", guidance), errors.Join(guidance, errors.New("cleanup failed"))} {
		if message := Error(wrapped); !strings.Contains(message, Escape(wrapped.Error())) {
			t.Fatalf("outer failure context lost: %q", message)
		}
	}
}

func TestMissingSourceHintRequiresResolverOwnedAbsence(t *testing.T) {
	resolver, err := localfs.NewResolver(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, missing := resolver.Resolve(context.Background(), sourcetest.Local(t, "missing", source.LocalSourceModeVendor), acquisition.OperationOptions{})
	var output bytes.Buffer
	if !PrintMissingSourceHint(&output, fmt.Errorf("resolve instructions: %w", missing)) || !strings.Contains(output.String(), "next: check that the source exists") {
		t.Fatalf("missing hint: %q", output.String())
	}
	for _, err := range []error{nil, os.ErrNotExist, errors.Join(errors.New("hash child"), os.ErrNotExist), errors.New("source path \"missing\" does not exist"), errors.New("git source path \"missing\" does not exist at HEAD")} {
		output.Reset()
		if PrintMissingSourceHint(&output, err) || output.Len() != 0 {
			t.Fatalf("unowned error got source advice: %v, %q", err, output.String())
		}
	}
}
