package clipresent_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/contractversion"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestLockOutputReportsOrderOnlyChanges(t *testing.T) {
	first := presentOrderExtension(t, "first", "first")
	second := presentOrderExtension(t, "second", "second")
	before := presentOrderFile(t, []desiredextension.Extension{first, second})
	after := presentOrderFile(t, []desiredextension.Extension{second, first})
	delta := lock.BuildDelta(before, after)

	var human bytes.Buffer
	clipresent.PrintDeltaSummaryWithOptions(
		&human,
		delta,
		clipresent.HumanOptions{Verbose: true},
	)
	for _, want := range []string{
		"lockfile changes: added=0 changed=0 removed=0 unchanged=2",
		"lockfile order changes: added=0 changed=1 removed=0 unchanged=0",
		"lockfile.order_constraint.changed:",
		"before=host_relation/pi.package-carrier/first=first",
		"after=host_relation/pi.package-carrier/second=second",
	} {
		if !strings.Contains(human.String(), want) {
			t.Fatalf("human output = %q, want %q", human.String(), want)
		}
	}

	var structured bytes.Buffer
	if err := clipresent.PrintJSON(&structured, clipresent.JSONInput{
		Command:       "lock",
		Mode:          "dry-run",
		ManifestPath:  "/repo/daem.toml",
		LockfilePath:  "/repo/daem.lock.toml",
		PreviousFound: true,
		Lockfile:      after,
		Delta:         delta,
	}); err != nil {
		t.Fatalf("PrintJSON returned error: %v", err)
	}
	var payload struct {
		SchemaVersion int `json:"schema_version"`
		EntryCounts   struct {
			OrderConstraints int `json:"order_constraints"`
		} `json:"entry_counts"`
		OrderChangeCounts struct {
			Changed int `json:"changed"`
		} `json:"order_change_counts"`
		OrderChanges []struct {
			Status string `json:"status"`
			Before struct {
				Members []struct {
					Subject struct {
						Name string `json:"name"`
					} `json:"subject"`
				} `json:"members"`
			} `json:"before"`
			After struct {
				Members []struct {
					Subject struct {
						Name string `json:"name"`
					} `json:"subject"`
				} `json:"members"`
			} `json:"after"`
		} `json:"order_constraint_changes"`
	}
	if err := json.Unmarshal(structured.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON output: %v", err)
	}
	if payload.SchemaVersion != contractversion.LockComparisonJSON ||
		payload.EntryCounts.OrderConstraints != 1 ||
		payload.OrderChangeCounts.Changed != 1 ||
		len(payload.OrderChanges) != 1 ||
		payload.OrderChanges[0].Status != "changed" ||
		payload.OrderChanges[0].Before.Members[0].Subject.Name != "first" ||
		payload.OrderChanges[0].After.Members[0].Subject.Name != "second" {
		t.Fatalf("order JSON payload = %#v\n%s", payload, structured.String())
	}
}

func presentOrderExtension(
	t *testing.T,
	name string,
	source string,
) desiredextension.Extension {
	t.Helper()
	return desiredtest.Extension(t, desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierPiPackage,
		Target:  target.TargetPi,
		Scope:   target.ScopeProject,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			source,
		),
	})
}

func presentOrderFile(
	t *testing.T,
	extensions []desiredextension.Extension,
) lock.File {
	t.Helper()
	subjects, err := refine.Extensions(extensions)
	if err != nil {
		t.Fatalf("refine.Extensions returned error: %v", err)
	}
	constraints, err := refine.ExtensionOrderConstraints(
		extensions,
		func(value desiredextension.CarrierKey) (hostrelation.HostLoadIdentity, error) {
			return hostrelation.NewHostLoadIdentity(value.Source().Ref())
		},
	)
	if err != nil {
		t.Fatalf("ExtensionOrderConstraints returned error: %v", err)
	}
	section, err := lock.NewLockedSection(subjects, constraints)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: section}
}
