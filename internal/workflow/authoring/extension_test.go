package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestExtensionAddBehaviorAppendsAdmittedClaudeMarketplaceDeclaration(t *testing.T) {
	original := []byte("version = 1\ntargets = [\"claude-code\"]\n")
	extension, err := ExtensionFromAddRequest(AddExtensionRequest{
		ID:     "context7-managed",
		Source: "context7@market",
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("ExtensionFromAddRequest returned error: %v", err)
	}
	updated, changeKind, err := ApplyAddExtensionToManifest(original, extension, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddExtensionToManifest returned error: %v", err)
	}
	if changeKind != "append extension resource" {
		t.Fatalf("changeKind = %q, want append extension resource", changeKind)
	}
	for _, want := range []string{
		"[[extension]]",
		`id = "context7-managed"`,
		`carrier = "claude-code-plugin"`,
		`targets = ["claude-code"]`,
		`scope = "project"`,
		`source = { marketplace = "context7@market" }`,
	} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated = %q, want %q", updated, want)
		}
	}
}

func TestExtensionAddBehaviorAppendsAdmittedClaudeGlobalMarketplaceDeclaration(t *testing.T) {
	original := []byte("version = 1\ntargets = [\"claude-code\"]\n")
	extension, err := ExtensionFromAddRequest(AddExtensionRequest{
		ID:     "context7-global",
		Source: "context7@market",
		Scope:  "global",
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}}, daempaths.ManifestOriginExplicit)
	if err != nil {
		t.Fatalf("ExtensionFromAddRequest returned error: %v", err)
	}
	updated, _, err := ApplyAddExtensionToManifest(original, extension, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddExtensionToManifest returned error: %v", err)
	}
	for _, want := range []string{
		`id = "context7-global"`,
		`carrier = "claude-code-plugin"`,
		`targets = ["claude-code"]`,
		`scope = "global"`,
		`source = { marketplace = "context7@market" }`,
	} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated = %q, want %q", updated, want)
		}
	}
}

func TestExtensionAddBehaviorRejectsDuplicateIDAndSubject(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }
`)
	_, _, err := ApplyAddExtensionToManifest(original, declarationcodec.Extension{
		ID:      "context7-managed",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "project",
		Source:  declarationcodec.ExtensionSource{Marketplace: "other@market"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate extension id "context7-managed"`) {
		t.Fatalf("err = %v, want duplicate id", err)
	}

	_, _, err = ApplyAddExtensionToManifest(original, declarationcodec.Extension{
		ID:      "other-id",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "project",
		Source:  declarationcodec.ExtensionSource{Marketplace: "context7@market"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate extension relation subject`) {
		t.Fatalf("err = %v, want duplicate subject", err)
	}

	_, _, err = ApplyAddExtensionToManifest(original, declarationcodec.Extension{
		ID:      "context7-managed",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "project",
		Source:  declarationcodec.ExtensionSource{Marketplace: "context7@market"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err == nil || !strings.Contains(err.Error(), `extension "context7-managed" already exists`) {
		t.Fatalf("err = %v, want already exists", err)
	}

	_, _, err = ApplyAddExtensionToManifest(original, declarationcodec.Extension{
		ID:      "context7-global",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "global",
		Source:  declarationcodec.ExtensionSource{Marketplace: "context7@market"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddExtensionToManifest global same marketplace returned error: %v", err)
	}

	hostSourceOriginal := []byte(`version = 1
targets = ["opencode"]

[[extension]]
id = "formatter-managed"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "project"
source = { host_source = "@acme/formatter" }
`)
	_, _, err = ApplyAddExtensionToManifest(hostSourceOriginal, declarationcodec.Extension{
		ID:      "formatter-alias",
		Carrier: "opencode-plugin",
		Targets: []string{"opencode"},
		Scope:   "project",
		Source:  declarationcodec.ExtensionSource{HostSource: "@acme/formatter"},
	}, declaration.ManifestHeader{Targets: []string{"opencode"}})
	if err == nil || !strings.Contains(err.Error(), `duplicate extension relation subject`) {
		t.Fatalf("host-source err = %v, want duplicate subject", err)
	}
}

func TestExtensionAddBehaviorToleratesExistingHostSourceRows(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "formatter-managed"
carrier = "opencode-plugin"
targets = ["opencode"]
scope = "global"
source = { host_source = "@acme/opencode-formatter" }

[[extension]]
id = "tools-managed"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "github:acme/pi-tools" }

[[extension]]
id = "guidance-managed"
carrier = "antigravity-cli-plugin"
targets = ["antigravity-cli"]
scope = "global"
source = { host_source = "modern-web-guidance@google" }
`)
	updated, _, err := ApplyAddExtensionToManifest(original, declarationcodec.Extension{
		ID:      "context7-managed",
		Carrier: "claude-code-plugin",
		Targets: []string{"claude-code"},
		Scope:   "project",
		Source:  declarationcodec.ExtensionSource{Marketplace: "context7@market"},
	}, declaration.ManifestHeader{Targets: []string{"claude-code"}})
	if err != nil {
		t.Fatalf("ApplyAddExtensionToManifest returned error: %v", err)
	}
	for _, want := range []string{
		`carrier = "opencode-plugin"`,
		`source = { host_source = "@acme/opencode-formatter" }`,
		`carrier = "pi-package"`,
		`source = { host_source = "github:acme/pi-tools" }`,
		`carrier = "antigravity-cli-plugin"`,
		`source = { host_source = "modern-web-guidance@google" }`,
		`id = "context7-managed"`,
	} {
		if !strings.Contains(string(updated), want) {
			t.Fatalf("updated = %q, want %q", updated, want)
		}
	}
}

func TestExtensionRemoveBehaviorRemovesAdmittedDeclaration(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-managed"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }

[[skill]]
name = "keep"
source = { path = "skills/keep" }
`)
	updated, changeKind, err := ApplyRemoveExtensionToManifest(original, RemoveExtensionRequest{ID: "context7-managed"})
	if err != nil {
		t.Fatalf("ApplyRemoveExtensionToManifest returned error: %v", err)
	}
	if changeKind != "remove extension resource" {
		t.Fatalf("changeKind = %q, want remove extension resource", changeKind)
	}
	if strings.Contains(string(updated), "[[extension]]") {
		t.Fatalf("updated = %q, want extension removed", updated)
	}
	if !strings.Contains(string(updated), "[[skill]]") {
		t.Fatalf("updated = %q, want following skill preserved", updated)
	}
}

func TestExtensionRemoveBehaviorRemovesAdmittedGlobalDeclaration(t *testing.T) {
	original := []byte(`version = 1
targets = ["claude-code"]

[[extension]]
id = "context7-global"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "global"
source = { marketplace = "context7@market" }
`)
	updated, _, err := ApplyRemoveExtensionToManifest(original, RemoveExtensionRequest{ID: "context7-global", Scope: "global"})
	if err != nil {
		t.Fatalf("ApplyRemoveExtensionToManifest returned error: %v", err)
	}
	if strings.Contains(string(updated), "[[extension]]") {
		t.Fatalf("updated = %q, want global extension removed", updated)
	}
}
