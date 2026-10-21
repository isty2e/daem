package diagnose

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestPiSourceDiagnosticsAreSelectedSourceFreeWarnings(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, test := range []struct {
			ref, code string
		}{
			{"github:acme/PI_SOURCE_MARKER#v1", "extension.pi_source.bare_github"},
			{"git+https://router.home.arpa/acme/PI_SOURCE_MARKER.git#v1", "extension.pi_source.git_plus"},
			{"git:github.com/acme/PI_SOURCE_MARKER", ""},
			{"https://github.com/acme/PI_SOURCE_MARKER.git", ""},
			{"github:", ""},
		} {
			t.Run(string(scope)+"/"+test.ref, func(t *testing.T) {
				value := piSourceDiagnosticExtension(t, scope, test.ref)
				diagnostics, err := PiPackageSourceDiagnostics([]desiredextension.Extension{value}, selection)
				if err != nil {
					t.Fatal(err)
				}
				if test.code == "" {
					if len(diagnostics) != 0 {
						t.Fatalf("unexpected diagnostics: %#v", diagnostics)
					}
					return
				}
				if len(diagnostics) != 1 {
					t.Fatalf("diagnostics=%#v, want one warning", diagnostics)
				}
				diagnostic := diagnostics[0]
				if diagnostic.Code != test.code || diagnostic.Severity != findings.SeverityWarn ||
					diagnostic.EntityID != value.ID() || diagnostic.Target != target.TargetPi || diagnostic.Scope != scope ||
					findings.HasDiagnosticErrors(diagnostics) {
					t.Fatalf("incorrect warning: %#v", diagnostic)
				}
				encoded, err := json.Marshal(diagnostics)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(encoded), "PI_SOURCE_MARKER") || strings.Contains(string(encoded), "router.home.arpa") ||
					diagnostic.Command != "" || diagnostic.Event != "" {
					t.Fatal("diagnostic reflected source data")
				}
			})
		}
	}

	excluded, err := targetselection.ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := PiPackageSourceDiagnostics([]desiredextension.Extension{
		piSourceDiagnosticExtension(t, target.ScopeProject, "github:acme/tool"),
	}, excluded)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("unselected Pi diagnostics=%#v error=%v", diagnostics, err)
	}
}

func TestPiSourceDiagnosticsDependOnDifferenceNotLocator(t *testing.T) {
	selection, err := targetselection.ForDiagnostics([]string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	var previous []findings.Diagnostic
	for _, ref := range []string{"git+https://example.test/acme/first.git", "git+https://router.home.arpa/team/second.git#v2"} {
		diagnostics, err := PiPackageSourceDiagnostics([]desiredextension.Extension{
			piSourceDiagnosticExtension(t, target.ScopeProject, ref),
		}, selection)
		if err != nil || len(diagnostics) != 1 {
			t.Fatalf("diagnostics=%#v error=%v", diagnostics, err)
		}
		if previous != nil && !reflect.DeepEqual(previous, diagnostics) {
			t.Fatal("locator/privacy/ref changed fixed diagnostic facts")
		}
		previous = diagnostics
	}
}

func piSourceDiagnosticExtension(t *testing.T, scope target.Scope, ref string) desiredextension.Extension {
	t.Helper()
	source, err := desiredextension.NewAuthoredSourceRef(desiredextension.SourceKindHostSource, ref)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name: "source-example", Carrier: desiredextension.CarrierPiPackage,
		Target: target.TargetPi, Scope: scope, Source: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}
