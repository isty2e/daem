//go:build darwin || linux

package metadataconversion

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
)

func TestSourceAdmissionUsesOriginalImagePermissions(t *testing.T) {
	fixture := compilerFixture(t, 1)
	for _, source := range []struct {
		kind sourceKind
		path string
	}{{stateSource, fixture.paths.StatefilePath}, {ownershipSource, fixture.paths.OwnershipRegistryPath}, {carrierSource, fixture.paths.CarrierClaimRegistryPath}} {
		content := fixture.before[source.path]
		original, err := fileset.NewPublicationImage(content, 0o640)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSourceEntry(source.kind, sourceEntry{image: original, content: content}); err == nil {
			t.Fatal("original accessible metadata became admissible through private evidence storage", source.kind)
		}
		private, err := fileset.NewPublicationImage(content, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSourceEntry(source.kind, sourceEntry{image: private, content: content}); err != nil {
			t.Fatal("private metadata permission control was rejected", source.kind, err)
		}
	}
}
