package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
)

func TestPublicationBeforeEvidenceSelectsBoundBytesWithoutOpeningTargets(t *testing.T) {
	root, intent, revision := publicationBeforeEvidenceFixture(t)
	for _, target := range intent.Targets() {
		view, err := ReadPublicationBeforeEvidence5(t.Context(), root, target.Path(), revision,
			mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
		if err != nil {
			t.Fatal(err)
		}
		if view.Namespace() != target.Path() || view.MarkerRevision() != revision || !view.Image().Equal(target.Before()) {
			t.Fatal("selected Before image lost its namespace or raw evidence binding")
		}
		if target.Before().Exists() {
			if hashBytes(view.Content()) != target.Before().Hash() || int64(len(view.Content())) != target.Before().Size() {
				t.Fatal("selected Before bytes differ from the containing facts")
			}
			content := view.Content()
			if len(content) != 0 {
				content[0] ^= 1
				if hashBytes(view.Content()) != target.Before().Hash() {
					t.Fatal("Before evidence leaked mutable bytes")
				}
			}
		} else if view.Content() != nil {
			t.Fatal("absent Before acquired a byte image")
		}
		assertMissing(t, target.Path())
	}
}

func TestPublicationBeforeEvidenceRefusesChangedRevisionNamespaceOrImage(t *testing.T) {
	root, intent, revision := publicationBeforeEvidenceFixture(t)
	read := func(namespace, expected string) (PublicationBeforeEvidenceView, error) {
		return ReadPublicationBeforeEvidence5(t.Context(), root, namespace, expected,
			mutationfs.MaximumPhysicalPathDepth, &fenceObservationBudget{})
	}
	for _, expected := range []string{"", hashBytes([]byte("other marker"))} {
		view, err := read(intent.targets[0].path, expected)
		if !errors.Is(err, ErrRecoveryIntentMismatch) || view.Image().Validate() == nil || view.Content() != nil {
			t.Fatal("changed or missing marker binding became usable Before evidence", err)
		}
	}
	if _, err := read(filepath.Join(filepath.Dir(root), "unselected"), revision); !errors.Is(err, ErrRecoveryIntentMismatch) {
		t.Fatal("selected a namespace outside the containing intent", err)
	}
	writeFixture(t, filepath.Join(transactionDir(root), publicationBackupName(0)),
		string(bytes.Repeat([]byte("x"), int(intent.targets[0].before.size))), transactionEvidenceMode)
	view, err := read(intent.targets[0].path, revision)
	if !errors.Is(err, ErrFileSetEvidenceInvalid) || view.Image().Validate() == nil || view.Content() != nil {
		t.Fatal("corrupt backup became usable Before evidence", err)
	}

	header, err := os.ReadFile(markerPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var reformatted bytes.Buffer
	if err := json.Indent(&reformatted, header, "", "  "); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, markerPath(root), reformatted.String(), transactionEvidenceMode)
	if _, err := read(intent.targets[0].path, revision); !errors.Is(err, ErrRecoveryIntentMismatch) {
		t.Fatal("backup was consumed before checking the selected raw marker revision", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ReadPublicationBeforeEvidence5(ctx, root, "", "", 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost precedence to Before evidence prerequisites", err)
	}
}

func publicationBeforeEvidenceFixture(t *testing.T) (string, PublicationIntent, string) {
	t.Helper()
	root := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), "metadata"))
	var targets []PublicationTarget
	beforeBytes := make(map[string][]byte)
	for _, name := range []string{"c-absent", "a-registry", "b-empty"} {
		before := AbsentPublicationImage()
		path := filepath.Join(filepath.Dir(root), name)
		if name != "c-absent" {
			content := []byte("registry Before")
			if name == "b-empty" {
				content = nil
			}
			before = publicationImageForTest(t, content)
			beforeBytes[path] = content
		}
		targets = append(targets, publicationTargetForTest(t, path, before,
			publicationImageForTest(t, []byte("After"))))
	}
	intent, err := NewPublicationIntent([]byte("original selection"), targets)
	if err != nil {
		t.Fatal(err)
	}
	for index, target := range intent.Targets() {
		if target.Before().Exists() {
			writeFixture(t, filepath.Join(transactionDir(root), publicationBackupName(index)),
				string(beforeBytes[target.Path()]), transactionEvidenceMode)
		}
	}
	body, header, err := encodePublicationEvidence(intent)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, markerPath(root), string(header), transactionEvidenceMode)
	writeFixture(t, filepath.Join(transactionDir(root), publicationIntentFile), string(body), transactionEvidenceMode)
	return root, intent, hashBytes(header)
}
