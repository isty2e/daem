package archguard

import "strings"

const ruleTOMLGrammarOwner = "TOML library imported outside syntax owner"

func analyzeTOMLImports(packagePath, importPath string) []GuardrailFinding {
	if packagePath == "internal/encoding/toml" {
		return nil
	}
	if importPath != "github.com/BurntSushi/toml" && importPath != "github.com/pelletier/go-toml/v2" &&
		!strings.HasPrefix(importPath, "github.com/pelletier/go-toml/v2/") {
		return nil
	}
	return []GuardrailFinding{{
		Rule: ruleTOMLGrammarOwner, PackagePath: packagePath, ImportPath: importPath,
		Detail: "TOML syntax and source facts belong to encoding/toml; schema and host interpretation consume that boundary",
	}}
}
