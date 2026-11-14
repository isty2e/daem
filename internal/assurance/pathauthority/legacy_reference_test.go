package pathauthority_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
)

func TestLegacyLocatorCandidateRequiresEncodingEvidence(t *testing.T) {
	root := t.TempDir()
	retained := filepath.Join(root, "İstanbul", "key.json")
	depth := len(strings.Split(strings.TrimPrefix(retained, filepath.VolumeName(retained)+string(filepath.Separator)), string(filepath.Separator)))
	nativeModes := strings.Repeat("s", depth-2) + "ui"
	native, err := pathauthority.NewExact(retained, "darwin-case-v2:"+nativeModes)
	if err != nil {
		t.Fatal(err)
	}
	for _, identity := range []pathauthority.Exact{
		pathtest.Exact(filepath.Join(root, "Literal.json")),
		pathtest.DarwinCaseSensitive(filepath.Join(root, "Ångström.json")),
		native,
	} {
		candidate, available := identity.LegacyLocatorCandidate()
		if !available || candidate != identity.Key() {
			t.Fatal("spelling-preserving or normalization-safe identity lost its candidate", identity)
		}
	}
	if candidate, available := (pathauthority.Exact{}).LegacyLocatorCandidate(); available || candidate != "" {
		t.Fatal("zero identity invented access evidence")
	}
	windows, err := pathauthority.NewExact(strings.ToLower(filepath.Join(root, "folded.json")), "windows-fold-v1:")
	if err != nil {
		t.Fatal(err)
	}
	if candidate, available := windows.LegacyLocatorCandidate(); available || candidate != "" {
		t.Fatal("unadmitted Windows folding invented a legacy locator")
	}
	filesystemRoot := pathtest.Exact(filepath.VolumeName(root) + string(filepath.Separator))
	if candidate, available := filesystemRoot.LegacyLocatorCandidate(); available || candidate != "" {
		t.Fatal("filesystem root invented an admitted metadata target")
	}
}

func TestDarwinV1FoldedReferenceCannotInferASCIIOrigin(t *testing.T) {
	root := strings.ToLower(t.TempDir())
	for _, pair := range [][2]string{{"Istanbul", "İstanbul"}, {"Key", "Key"}, {"alpha", "ALPHA"}} {
		if strings.ToLower(pair[0]) != strings.ToLower(pair[1]) {
			t.Fatal("fixture does not expose a folded-key collision", pair)
		}
		key := filepath.Join(root, strings.ToLower(pair[0]))
		depth := len(strings.Split(strings.TrimPrefix(key, filepath.VolumeName(key)+string(filepath.Separator)), string(filepath.Separator)))
		for _, modes := range []string{strings.Repeat("i", depth), strings.Repeat("s", depth-1) + "i"} {
			identity, err := pathauthority.NewExact(key, "darwin-case-v1:"+modes)
			if err != nil {
				t.Fatal(err)
			}
			if candidate, available := identity.LegacyLocatorCandidate(); available || candidate != "" {
				t.Fatal("folded key was promoted to access without its original spelling", identity)
			}
		}
	}
}
