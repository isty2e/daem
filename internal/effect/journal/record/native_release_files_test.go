package record

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/effect/fileset"
)

func releaseTargetForTest(t *testing.T, path string) NativeReleaseNamespace {
	t.Helper()
	value, err := NewNativeReleaseExactNamespace(pathtest.Exact(path))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func releaseFileForTest(t *testing.T, role NativeReleaseFileRole, write bool) NativeReleaseFileIntent {
	t.Helper()
	after := ""
	if write {
		after = Fingerprint([]byte("after"))
	}
	value, err := NewNativeReleaseFileIntent(NativeReleaseFileInput{
		Role: role, Target: releaseTargetForTest(t, filepath.Join(t.TempDir(), string(role))), BeforeExists: true,
		BeforeSHA256: Fingerprint([]byte("before")), BeforeMode: 0o640, AfterSHA256: after, Write: write,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNativeReleaseFilesBindOneFiniteIndependentlySelectedTargetSet(t *testing.T) {
	manifest := releaseFileForTest(t, NativeReleaseManifest, true)
	lock, err := NewNativeReleaseFileIntent(NativeReleaseFileInput{Role: NativeReleaseDefaultLock, Target: releaseTargetForTest(t, filepath.Join(t.TempDir(), "default.lock"))})
	if err != nil {
		t.Fatal(err)
	}
	state := releaseFileForTest(t, NativeReleaseState, true)
	want := []NativeReleaseFileIntent{manifest, lock, state}
	for _, input := range [][]NativeReleaseFileIntent{{manifest, lock, state}, {state, manifest, lock}, {lock, state, manifest}} {
		got, err := canonicalReleaseFiles(input)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("finite file roles changed their meaning or order", err)
		}
		input[0] = NativeReleaseFileIntent{}
		if got[0] != manifest || got[1].BeforeExists() || got[1].Writes() || state.BeforeMode() != 0o640 {
			t.Fatal("release file facts aliased caller memory or lost modes/absence")
		}
	}
	for _, invalid := range [][]NativeReleaseFileIntent{{manifest, state}, {manifest, manifest, state}, {manifest, lock, {}}} {
		if _, err := canonicalReleaseFiles(invalid); err == nil {
			t.Fatal("incomplete or repeated roles admitted a file-set intent")
		}
	}
	lock.target = manifest.target
	if _, err := canonicalReleaseFiles([]NativeReleaseFileIntent{manifest, lock, state}); err == nil {
		t.Fatal("different local roles acquired the same recorded target")
	}
}

func TestNativeReleaseFilesRefuseCreationEqualStateAndInvalidImageFacts(t *testing.T) {
	base := NativeReleaseFileInput{Role: NativeReleaseState, Target: releaseTargetForTest(t, filepath.Join(t.TempDir(), "state.json")), BeforeExists: true, BeforeSHA256: Fingerprint([]byte("before")), BeforeMode: 0o600, AfterSHA256: Fingerprint([]byte("after")), Write: true}
	for name, change := range map[string]func(*NativeReleaseFileInput){
		"host-role":    func(input *NativeReleaseFileInput) { input.Role = "host" },
		"equal-state":  func(input *NativeReleaseFileInput) { input.AfterSHA256 = input.BeforeSHA256 },
		"state-retain": func(input *NativeReleaseFileInput) { input.Write = false; input.AfterSHA256 = "" },
		"authoring-create": func(input *NativeReleaseFileInput) {
			input.Role = NativeReleaseDefaultLock
			input.BeforeExists = false
			input.BeforeSHA256 = ""
			input.BeforeMode = 0
		},
		"short-digest":      func(input *NativeReleaseFileInput) { input.BeforeSHA256 = "abc" },
		"unprefixed-digest": func(input *NativeReleaseFileInput) { input.BeforeSHA256 = strings.Repeat("a", 64) },
		"uppercase-digest":  func(input *NativeReleaseFileInput) { input.AfterSHA256 = "sha256:" + strings.Repeat("A", 64) },
		"special-mode":      func(input *NativeReleaseFileInput) { input.BeforeMode |= os.ModeSymlink },
		"absent-with-mode":  func(input *NativeReleaseFileInput) { input.BeforeExists = false; input.BeforeSHA256 = "" },
	} {
		t.Run(name, func(t *testing.T) {
			input := base
			change(&input)
			if _, err := NewNativeReleaseFileIntent(input); err == nil {
				t.Fatal("invalid local image facts admitted")
			}
		})
	}
}

func TestNativeReleaseImageFactsMatchTheExistingFileSetPublication(t *testing.T) {
	root := t.TempDir()
	paths := map[NativeReleaseFileRole]string{
		NativeReleaseManifest:    filepath.Join(root, "daem.toml"),
		NativeReleaseDefaultLock: filepath.Join(root, "daem.lock.json"),
		NativeReleaseState:       filepath.Join(root, "state.json"),
	}
	var targets []fileset.FileTarget
	var expected []fileset.RecoveryTarget
	for _, role := range []NativeReleaseFileRole{NativeReleaseManifest, NativeReleaseDefaultLock, NativeReleaseState} {
		input := NativeReleaseFileInput{Role: role, Target: releaseTargetForTest(t, paths[role])}
		var target fileset.FileTarget
		var err error
		if role == NativeReleaseDefaultLock {
			target, err = fileset.NewFileRetain(paths[role])
		} else {
			before, after := []byte("before-"+string(role)), []byte("after-"+string(role))
			input.BeforeExists, input.Write = true, true
			input.BeforeSHA256, input.AfterSHA256 = Fingerprint(before), Fingerprint(after)
			input.BeforeMode = 0o600
			if role == NativeReleaseManifest {
				input.BeforeMode = 0o640
			}
			if err := os.WriteFile(paths[role], before, input.BeforeMode); err != nil {
				t.Fatal(err)
			}
			if role == NativeReleaseState {
				target, err = fileset.NewFileCommitPointWrite(paths[role], after)
			} else {
				target, err = fileset.NewFileWrite(paths[role], after)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		intent, err := NewNativeReleaseFileIntent(input)
		if err != nil {
			t.Fatal(err)
		}
		targets = append(targets, target)
		expected = append(expected, fileset.RecoveryTarget{
			Path: paths[role], BeforeExists: intent.BeforeExists(),
			BeforeHash: intent.BeforeSHA256(), BeforeMode: intent.BeforeMode(), AfterHash: intent.AfterSHA256(),
			Write: intent.Writes(), CommitPoint: role == NativeReleaseState,
		})
	}
	if err := fileset.CommitFreshFileSetForIntent(t.Context(), fileset.FileSetInput{StateDir: filepath.Join(root, ".daem"), Targets: targets}, expected); err != nil {
		t.Fatal("release image facts diverged from the actual file-set producer", err)
	}
	for role, path := range paths {
		if role == NativeReleaseDefaultLock {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatal("release created an absent default lock", err)
			}
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil || string(content) != "after-"+string(role) {
			t.Fatal("file-set publication did not settle its declared image", role, err)
		}
	}
}

func TestNativeReleaseMissingTargetSurvivesARefinedParentNamespace(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	candidate := filepath.Join(parent, "caf\u00e9.lock.json")
	witness := pathtest.DarwinCaseSensitive(candidate).Witness()
	old, err := pathauthority.NewProvisional(candidate, witness, root, pathtest.DarwinCaseSensitive(root).Witness())
	if err != nil {
		t.Fatal(err)
	}
	refined, err := pathauthority.NewProvisional(candidate, witness, parent, pathtest.DarwinCaseSensitive(parent).Witness())
	if err != nil {
		t.Fatal(err)
	}
	before, err := NewNativeReleaseProvisionalNamespace(old)
	if err != nil {
		t.Fatal(err)
	}
	after, err := NewNativeReleaseProvisionalNamespace(refined)
	if err != nil {
		t.Fatal(err)
	}
	if !before.matches(after) || !after.matches(before) {
		t.Fatal("unchanged missing target lost its selected identity when a parent appeared")
	}
	other := releaseTargetForTest(t, filepath.Join(parent, "other.lock.json"))
	if before.matches(other) {
		t.Fatal("namespace refinement selected another missing target")
	}
}
