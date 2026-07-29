package extension

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestAssignExtensionIDsAdvancesAllCollidingCandidatesTogether(t *testing.T) {
	left := testCarrierKey(t, "alpha/beta")
	right := testCarrierKey(t, "alpha@beta")
	candidates := map[desiredextension.CarrierKey]candidateFact{
		left: {
			key:          left,
			loadIdentity: testLoadIdentity(t, "alpha/beta"),
		},
		right: {
			key:          right,
			loadIdentity: testLoadIdentity(t, "alpha@beta"),
		},
	}
	digests := map[desiredextension.CarrierKey]string{
		left:  strings.Repeat("a", 12) + strings.Repeat("1", 52),
		right: strings.Repeat("a", 12) + strings.Repeat("2", 52),
	}

	assigned, err := assignExtensionIDsWithDigest(
		candidates,
		nil,
		func(key desiredextension.CarrierKey) string { return digests[key] },
	)
	if err != nil {
		t.Fatalf("assignExtensionIDsWithDigest: %v", err)
	}
	base := "alpha-beta-opencode-project"
	if assigned[left] != base+"-"+digests[left][:24] ||
		assigned[right] != base+"-"+digests[right][:24] {
		t.Fatalf("assigned = %#v", assigned)
	}
}

func TestAssignExtensionIDsPreservesFixedIDAndAdvancesNewCandidate(t *testing.T) {
	fixedKey := testCarrierKey(t, "existing")
	fixed, err := desiredextension.New(desiredextension.Spec{
		Name:    "alpha-opencode-project",
		Carrier: fixedKey.Carrier(),
		Target:  fixedKey.Target(),
		Scope:   fixedKey.Scope(),
		Source:  fixedKey.Source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	incoming := testCarrierKey(t, "incoming")
	digest := strings.Repeat("b", 64)
	assigned, err := assignExtensionIDsWithDigest(
		map[desiredextension.CarrierKey]candidateFact{
			incoming: {
				key:          incoming,
				loadIdentity: testLoadIdentity(t, "alpha"),
			},
		},
		[]desiredextension.Extension{fixed},
		func(desiredextension.CarrierKey) string { return digest },
	)
	if err != nil {
		t.Fatal(err)
	}
	if assigned[incoming] != "alpha-opencode-project-"+digest[:12] {
		t.Fatalf("assigned ID = %q", assigned[incoming])
	}
}

func TestAssignExtensionIDsBlocksFullDigestCollision(t *testing.T) {
	left := testCarrierKey(t, "left")
	right := testCarrierKey(t, "right")
	candidates := map[desiredextension.CarrierKey]candidateFact{
		left: {
			key:          left,
			loadIdentity: testLoadIdentity(t, "same"),
		},
		right: {
			key:          right,
			loadIdentity: testLoadIdentity(t, "same"),
		},
	}
	_, err := assignExtensionIDsWithDigest(
		candidates,
		nil,
		func(desiredextension.CarrierKey) string { return strings.Repeat("c", 64) },
	)
	if err == nil || !strings.Contains(err.Error(), "full digest") {
		t.Fatalf("assignExtensionIDsWithDigest error = %v", err)
	}
}

func TestAssignExtensionIDsRejectsExistingRelationUnderDifferentIDs(t *testing.T) {
	key := testCarrierKey(t, "existing")
	first, err := desiredextension.New(desiredextension.Spec{
		Name:    "first",
		Carrier: key.Carrier(),
		Target:  key.Target(),
		Scope:   key.Scope(),
		Source:  key.Source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := desiredextension.New(desiredextension.Spec{
		Name:    "second",
		Carrier: key.Carrier(),
		Target:  key.Target(),
		Scope:   key.Scope(),
		Source:  key.Source(),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = assignExtensionIDs(nil, []desiredextension.Extension{first, second})
	if err == nil || !strings.Contains(err.Error(), "appears under ids") {
		t.Fatalf("assignExtensionIDs error = %v, want duplicate relation rejection", err)
	}
}

func testCarrierKey(t *testing.T, source string) desiredextension.CarrierKey {
	t.Helper()
	ref, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		target.ScopeProject,
		ref,
	)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testLoadIdentity(
	t *testing.T,
	value string,
) hostrelation.HostLoadIdentity {
	t.Helper()
	identity, err := hostrelation.NewHostLoadIdentity(value)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
