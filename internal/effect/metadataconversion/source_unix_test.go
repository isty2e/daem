//go:build darwin || linux

package metadataconversion

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
)

func TestSourceAdmissionVerifiesContentRegardlessOfPermissions(t *testing.T) {
	fixture := compilerFixture(t, 1)
	for _, path := range []string{fixture.paths.StatefilePath, fixture.paths.OwnershipRegistryPath, fixture.paths.CarrierClaimRegistryPath} {
		content := fixture.before[path]
		original, err := fileset.NewPublicationImage(content, 0o640)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSourceEntry(sourceEntry{image: original, content: content}); err != nil {
			t.Fatal("readable metadata rejected", path, err)
		}
		if err := validateSourceEntry(sourceEntry{image: original, content: []byte("another image")}); err == nil {
			t.Fatal("changed backup admitted", path)
		}
	}
}
