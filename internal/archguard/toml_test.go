package archguard

import "testing"

func TestTOMLDependencyBoundaryAdmitsOnlySyntaxOwner(t *testing.T) {
	for _, dependency := range []string{"github.com/BurntSushi/toml", "github.com/pelletier/go-toml/v2", "github.com/pelletier/go-toml/v2/unstable"} {
		if violations := analyzeTOMLImports("internal/encoding/toml", dependency); len(violations) != 0 {
			t.Fatal("the syntax owner cannot consume its parser/writer", violations)
		}
		for _, consumer := range []string{"internal/declaration", "internal/encoding/configdocument", "internal/encoding/toml/fields", "internal/assurance/observe/live"} {
			violations := analyzeTOMLImports(consumer, dependency)
			if len(violations) != 1 || violations[0].Rule != ruleTOMLGrammarOwner || violations[0].PackagePath != consumer || violations[0].ImportPath != dependency {
				t.Fatal("a consumer bypassed the shared TOML boundary", consumer, dependency, violations)
			}
		}
	}
	if violations := analyzeTOMLImports("internal/declaration", "encoding/json"); len(violations) != 0 {
		t.Fatal("the TOML boundary claimed an unrelated format", violations)
	}
}
