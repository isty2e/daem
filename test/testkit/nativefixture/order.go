//go:build darwin || linux

package nativefixture

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/isty2e/daem/test/testkit"
)

func RequirePiOrderValues(t *testing.T, path, theme string, packages ...string) {
	t.Helper()
	var document struct {
		Theme    string   `json:"theme"`
		Packages []string `json:"packages"`
		Foreign  struct {
			Retained bool `json:"retained"`
		} `json:"foreign"`
	}
	content := testkit.ReadFile(t, path)
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	if document.Theme != theme || !document.Foreign.Retained || !slices.Equal(document.Packages, packages) {
		t.Fatalf("key/order convergence changed selected or unrelated values: %s", content)
	}
}
