package initworkflow

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestInitRefusesInterruptedMetadataTransaction(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "daem.toml")
	paths, err := daempaths.ResolveCreation(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WriteInterrupted(t, paths.StateDir)

	for _, test := range []struct {
		name string
		run  func() error
	}{
		{
			name: "dry-run",
			run: func() error {
				_, err := BuildPlan(context.Background(), Input{
					ManifestPath: manifestPath,
					Force:        true,
				})
				return err
			},
		},
		{
			name: "write",
			run: func() error {
				_, err := Execute(context.Background(), Input{
					ManifestPath: manifestPath,
					Force:        true,
				})
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
				t.Fatalf("error = %v, want interrupted file-set transaction", err)
			}
		})
	}
}
