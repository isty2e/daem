package configdocument_test

import (
	"context"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
)

func TestTOMLDocumentDoesNotInheritHostKeyCeiling(t *testing.T) {
	key := strings.Repeat("k", tomlsyntax.MaximumKeyBytes+1)
	content := []byte(key + " = true\n")
	limits := configdocument.Limits{MaximumBytes: int64(len(content)), MaximumDepth: 4}
	document, err := configdocument.Parse(t.Context(), content, configdocument.TOML, limits)
	if err != nil {
		t.Fatal("byte/depth-bounded document inherited another operation's key limit", err)
	}
	leaves, err := document.Leaves(t.Context(), int64(len(key)+1))
	if err != nil || len(leaves) != 1 || leaves[0].Path.String() != "/"+key {
		t.Fatal("a complete admitted key was lost", err)
	}
	if err := tomlsyntax.Admit(context.Background(), content, tomlsyntax.StandardLimits()); err == nil {
		t.Fatal("explicit host structure policy stopped enforcing its own ceiling")
	}
}
