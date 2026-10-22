package authoring

import (
	"bytes"
	"strings"
	"testing"

	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestPiSourceWarningsPreserveAuthoredAndUnchangedDeclarations(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		for _, test := range []struct {
			ref      string
			warnings int
		}{
			{"github:acme/PI_SOURCE_MARKER#v1", 1},
			{"git+https://router.home.arpa/acme/PI_SOURCE_MARKER.git#v1", 1},
			{"git@github.com:acme/PI_SOURCE_MARKER.git", 1},
			{"git@router.home.arpa:acme/PI_SOURCE_MARKER.git#v1", 1},
			{"deploy@router.home.arpa:team/PI_SOURCE_MARKER.git", 1},
			{"git:github.com/acme/tool", 0},
			{"git:git@github.com:acme/tool", 0},
			{"ssh://git@github.com/acme/tool.git", 0},
			{"ssh://deploy@router.home.arpa/team/tool.git", 0},
			{"https://github.com/acme/tool.git", 0},
			{"github:", 0},
		} {
			t.Run(scope+"/"+test.ref, func(t *testing.T) {
				document := ManifestDocument{
					Path: "daem.toml", Content: []byte("version = 1\ntargets = [\"pi\"]\n"),
					Paths: daempaths.Paths{ManifestOrigin: daempaths.ManifestOriginExplicit},
				}
				request := AddExtensionRequest{ID: "source-example", Source: test.ref, Targets: []string{"pi"}, Scope: scope}
				first, err := BuildAddExtensionChange(document, request)
				if err != nil {
					t.Fatal(err)
				}
				if len(first.Warnings) != test.warnings {
					t.Fatalf("warnings=%#v, want %d", first.Warnings, test.warnings)
				}
				for _, warning := range first.Warnings {
					if warning.Resource.Name() != request.ID || strings.Contains(warning.Detail, "PI_SOURCE_MARKER") ||
						strings.Contains(warning.Detail, "router.home.arpa") {
						t.Fatalf("source-reflecting warning: %#v", warning)
					}
				}
				if !strings.Contains(first.ManifestBlock, test.ref) {
					t.Fatal("warning changed the intentional source-bearing manifest block")
				}

				document.Content = first.Content
				unchanged, err := BuildAddExtensionChange(document, request)
				if err != nil || unchanged.ChangeKind != "unchanged" || !bytes.Equal(first.Content, unchanged.Content) ||
					len(unchanged.Warnings) != test.warnings {
					t.Fatalf("unchanged add lost its contract: %#v, error=%v", unchanged, err)
				}
			})
		}
	}
}

func TestPiSourceSCPAuthoringWarningsQualifySSHAdvice(t *testing.T) {
	for _, scope := range []string{"project", "global"} {
		document := ManifestDocument{
			Path: "daem.toml", Content: []byte("version = 1\ntargets = [\"pi\"]\n"),
			Paths: daempaths.Paths{ManifestOrigin: daempaths.ManifestOriginExplicit},
		}
		change, err := BuildAddExtensionChange(document, AddExtensionRequest{
			ID: "source-example", Source: "deploy@router.home.arpa:team/PI_SOURCE_MARKER.git",
			Targets: []string{"pi"}, Scope: scope,
		})
		if err != nil || len(change.Warnings) != 1 {
			t.Fatalf("warnings=%#v error=%v", change.Warnings, err)
		}
		detail := change.Warnings[0].Detail
		urlAdvice := strings.Index(detail, "ssh://user@host/owner/repo")
		scpAdvice := strings.Index(detail, "git:git@host:owner/repo")
		if urlAdvice < 0 || scpAdvice <= urlAdvice ||
			!strings.Contains(detail, "only an option when the SSH user is git") ||
			!strings.Contains(detail, "repository paths may differ") ||
			!strings.Contains(detail, "the authored source is not converted") {
			t.Fatalf("SSH advice is not qualified: %q", detail)
		}
	}
}
