package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit"
)

func TestRunLockAdmitsHostSourceExtensionCarriers(t *testing.T) {
	tests := []struct {
		name                string
		manifest            string
		wantNamespace       string
		wantName            string
		wantTarget          string
		wantScope           string
		wantSourceRef       string
		wantRelationKey     string
		wantRouteID         string
		wantContractVersion string
	}{
		{
			name: "opencode global host source",
			manifest: `
version = 1
targets = ["opencode"]

[[extension]]
id = "formatter-managed"
carrier = "opencode-plugin"
scope = "global"
source = { host_source = "@acme/opencode-formatter" }
`,
			wantNamespace:       "opencode.plugin-carrier",
			wantName:            "formatter-managed",
			wantTarget:          "opencode",
			wantScope:           "global",
			wantSourceRef:       "@acme/opencode-formatter",
			wantRouteID:         openCodePluginRoute(t).RouteID(),
			wantContractVersion: openCodePluginRoute(t).AdapterContractVersion(),
		},
		{
			name: "pi project host source",
			manifest: `
version = 1
targets = ["pi"]

[[extension]]
id = "tools-managed"
carrier = "pi-package"
scope = "project"
source = { host_source = "github:acme/pi-tools" }
`,
			wantNamespace:       "pi.package-carrier",
			wantName:            "tools-managed",
			wantTarget:          "pi",
			wantScope:           "project",
			wantSourceRef:       "github:acme/pi-tools",
			wantRouteID:         piPackageRoute(t).RouteID(),
			wantContractVersion: piPackageRoute(t).AdapterContractVersion(),
		},
		{
			name: "antigravity cli global host source",
			manifest: `
version = 1
targets = ["antigravity-cli"]

[[extension]]
id = "guidance-managed"
carrier = "antigravity-cli-plugin"
scope = "global"
source = { host_source = "modern-web-guidance@google" }
`,
			wantNamespace:       "antigravity-cli.plugin-carrier",
			wantName:            "guidance-managed",
			wantTarget:          "antigravity-cli",
			wantScope:           "global",
			wantSourceRef:       "modern-web-guidance@google",
			wantRelationKey:     "modern-web-guidance",
			wantRouteID:         antigravityCLIPluginRoute(t).RouteID(),
			wantContractVersion: antigravityCLIPluginRoute(t).AdapterContractVersion(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tempDir := t.TempDir()
			manifestPath := filepath.Join(tempDir, "daem.toml")
			lockfilePath := filepath.Join(tempDir, "daem.lock.toml")
			testkit.WriteFile(t, tempDir, "daem.toml", test.manifest)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			exitCode := testkit.RunVerboseCLI([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr)
			if exitCode != 0 || stderr.Len() != 0 {
				t.Fatalf("exitCode=%d stdout=%q stderr=%q", exitCode, stdout.String(), stderr.String())
			}

			locked, err := lockfile.Load(t.Context(), lockfilePath)
			if err != nil {
				t.Fatalf("Load returned error: %v", err)
			}
			if len(locked.Locked.Subjects()) != 1 {
				t.Fatalf("locked subjects = %#v, want one", locked.Locked.Subjects())
			}
			record := locked.Locked.Subjects()[0]
			wantSubject, err := topology.NewSubjectID(topology.SubjectHostRelation, test.wantNamespace, test.wantName)
			if err != nil {
				t.Fatalf("NewSubjectID returned error: %v", err)
			}
			if record.SubjectID() != wantSubject {
				t.Fatalf("subject = %#v, want %s/%s", record.SubjectID(), test.wantNamespace, test.wantName)
			}
			relation := testkit.LockedDelegatedRelation(t, record)
			if relation.RouteContractVersion() != test.wantContractVersion {
				t.Fatalf("route contract = %q, want %q", relation.RouteContractVersion(), test.wantContractVersion)
			}
			source, err := desiredextension.ParseSourceRef(relation.SourceNamespace())
			if err != nil {
				t.Fatalf("ParseSourceRef returned error: %v", err)
			}
			if string(relation.Target()) != test.wantTarget ||
				string(relation.Scope()) != test.wantScope ||
				source.Ref() != test.wantSourceRef {
				t.Fatalf("carrier subject = target %q scope %q source %q, want %q/%q/%q",
					relation.Target(), relation.Scope(), source.Ref(), test.wantTarget, test.wantScope, test.wantSourceRef)
			}
			wantRelationKey := test.wantRelationKey
			if wantRelationKey == "" {
				wantRelationKey = test.wantSourceRef
			}
			if relation.SourceNamespace() != "host-source:"+test.wantSourceRef ||
				string(relation.ExpectedRelation().SubjectKey()) != wantRelationKey ||
				relation.RouteID() != test.wantRouteID ||
				relation.RouteContractVersion() != test.wantContractVersion {
				t.Fatalf("relation = %#v, want host-source delegated route", relation)
			}
		})
	}
}
