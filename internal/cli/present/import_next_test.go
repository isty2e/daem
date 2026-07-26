package clipresent

import (
	"strings"
	"testing"
)

func TestImportNextActionScopesManageExistingGuidance(t *testing.T) {
	manifestPath := "/tmp/project with space/daem.toml"
	for _, test := range []struct {
		name      string
		plan      ImportPlan
		want      []string
		forbidden []string
	}{
		{
			name:      "conflict",
			plan:      ImportPlan{HasErrors: true, ResourceCount: 1, ManifestPath: manifestPath},
			want:      []string{"resolve reported import conflicts"},
			forbidden: []string{"--manage-existing", "daem lock"},
		},
		{
			name:      "dry run",
			plan:      ImportPlan{DryRun: true, ResourceCount: 1, ManifestPath: manifestPath},
			want:      []string{"rerun daem import without --dry-run"},
			forbidden: []string{"--manage-existing", "daem lock"},
		},
		{
			name:      "successful empty write",
			plan:      ImportPlan{ManifestPath: manifestPath},
			want:      []string{mustShellCommand(t, "daem", "lock", "--manifest", manifestPath, "--dry-run")},
			forbidden: []string{"--manage-existing"},
		},
		{
			name: "successful resource write",
			plan: ImportPlan{ResourceCount: 1, ManifestPath: manifestPath},
			want: []string{
				mustShellCommand(t, "daem", "lock", "--manifest", manifestPath, "--dry-run"),
				"after writing the lockfile",
				mustShellCommand(t, "daem", "apply", "--manifest", manifestPath, "--manage-existing", "--dry-run"),
			},
			forbidden: []string{"--yes"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output strings.Builder
			printImportNextAction(&output, test.plan)
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("output = %q, want %q", output.String(), want)
				}
			}
			for _, forbidden := range test.forbidden {
				if strings.Contains(output.String(), forbidden) {
					t.Fatalf("output = %q, contains forbidden %q", output.String(), forbidden)
				}
			}
		})
	}
}
