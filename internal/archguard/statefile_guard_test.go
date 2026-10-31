package archguard

import "testing"

func TestStateCodecGuardRejectsOrdinaryOnlyProductionAdmission(t *testing.T) {
	for _, test := range []struct {
		name, imported, expression string
		want                       int
	}{
		{"restricted call", "statefile", "statefile.NewCodec(values)", 1},
		{"aliased restricted factory", "saved", "saved.NewCodec", 1},
		{"complete current codec", "statefile", "statefile.Codec{}", 0},
		{"selected current codec", "statefile", "statefile.NewNativeCodec(values)", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package example\nimport " + test.imported + " \"github.com/isty2e/daem/internal/assurance/statefile\"\nvar codec = " + test.expression
			record := PackageRecord{
				GoFiles:      []string{"reader.go"},
				FileContents: map[string]string{"reader.go": source},
			}
			findings := analyzeRestrictedStateCodecs("internal/workflow/example", record)
			if len(findings) != test.want {
				t.Fatalf("findings = %#v, want %d", findings, test.want)
			}
		})
	}
}
