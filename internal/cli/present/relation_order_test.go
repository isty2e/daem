package clipresent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/contractversion"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestRelationOrderIdentityDisclosurePreservesOnlySafeBoundedIdentities(
	t *testing.T,
) {
	longIdentity := strings.Repeat("a", maxRelationOrderIdentityDisclosureBytes+1)
	for _, test := range []struct {
		name     string
		value    string
		redacted bool
	}{
		{name: "npm package", value: "npm:@acme/tool"},
		{name: "npm selector", value: "npm:@acme/tool@>=1.0.0"},
		{name: "credential-free git URL", value: "git:https://example.test/acme/tool.git"},
		{name: "quotes and backslashes", value: `npm:quote"and\slash`},
		{name: "URL userinfo", value: "https://user:secret@example.test/plugin", redacted: true},
		{name: "nested URL userinfo", value: "git:https://user:secret@example.test/plugin", redacted: true},
		{name: "URL query", value: "https://example.test/plugin?token=secret", redacted: true},
		{name: "encoded secret fragment", value: "https://example.test/plugin#token%3Dsecret", redacted: true},
		{name: "secret assignment", value: "npm:tool#api_key=secret", redacted: true},
		{name: "generic assignment", value: "npm:tool;credential=secret", redacted: true},
		{name: "secret colon", value: "npm:tool;authorization:Bearer-secret", redacted: true},
		{name: "absolute path", value: "/Users/alice/private/plugin.ts", redacted: true},
		{name: "file URL", value: "file:///Users/alice/private/plugin.ts", redacted: true},
		{name: "nested file URL", value: "git:file:///Users/alice/private/plugin.ts", redacted: true},
		{name: "local identity", value: "local:project:/Users/alice/private/plugin.ts", redacted: true},
		{name: "relative path", value: "../private/plugin.ts", redacted: true},
		{name: "Windows path", value: `C:\Users\alice\private\plugin.ts`, redacted: true},
		{name: "overlong", value: longIdentity, redacted: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			disclosure := relationOrderIdentityDisclosureFor(test.value)
			if disclosure.Redacted() != test.redacted {
				t.Fatalf(
					"Redacted() = %t, want %t for %q",
					disclosure.Redacted(),
					test.redacted,
					test.value,
				)
			}
			if !test.redacted {
				if disclosure.Value() != test.value {
					t.Fatalf("Value() = %q, want %q", disclosure.Value(), test.value)
				}
				return
			}
			digest := sha256.Sum256([]byte(test.value))
			want := "redacted:sha256:" + hex.EncodeToString(digest[:])
			if disclosure.Value() != want ||
				relationOrderIdentityDisclosureFor(test.value).Value() != want ||
				strings.Contains(disclosure.Value(), test.value) {
				t.Fatalf("redacted disclosure = %#v, want %q", disclosure, want)
			}
		})
	}
}

func TestRelationOrderRiskDisclosureRedactsUnsafeIdentityAcrossFormats(
	t *testing.T,
) {
	const unsafeIdentity = "https://user:secret@example.test/plugin?token=secret"
	decision := presentRelationOrderDecisionWithForeignIdentities(
		t,
		target.TargetOpenCode,
		hostrelation.ConfigOrderOnly,
		[]string{unsafeIdentity},
	)
	wantIdentity := relationOrderIdentityDisclosureFor(unsafeIdentity).Value()
	if changes := decision.PrecedenceChanges(); len(changes) != 2 ||
		string(changes[0].ForeignIdentity()) != unsafeIdentity {
		t.Fatalf("canonical decision identity changed before projection: %#v", changes)
	}

	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision})
	if len(rows) != 1 || len(rows[0].Risks) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	for _, risk := range rows[0].Risks {
		if risk.ForeignIdentity != wantIdentity || !risk.ForeignIdentityRedacted {
			t.Fatalf("risk = %#v", risk)
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(unsafeIdentity)) ||
		bytes.Contains(encoded, []byte("secret")) {
		t.Fatalf("JSON discloses unsafe identity: %s", encoded)
	}

	for _, options := range []HumanOptions{
		{},
		{Verbose: true},
	} {
		var output bytes.Buffer
		PrintRelationOrderActionsWithOptions(
			&output,
			[]reconcile.RelationOrderDecision{decision},
			options,
		)
		text := output.String()
		if !strings.Contains(text, `foreign="`+wantIdentity+`"`) ||
			strings.Contains(text, unsafeIdentity) ||
			strings.Contains(text, "secret") {
			t.Fatalf("human output = %q", text)
		}
	}
	if changes := decision.PrecedenceChanges(); len(changes) != 2 ||
		string(changes[0].ForeignIdentity()) != unsafeIdentity {
		t.Fatalf("canonical decision identity changed after projection: %#v", changes)
	}
}

func TestRelationOrderRiskDisclosureEscapesSafeIdentityAcrossFormats(
	t *testing.T,
) {
	const safeIdentity = `npm:quote"and\slash`
	decision := presentRelationOrderDecisionWithForeignIdentities(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
		[]string{safeIdentity},
	)
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision})
	if len(rows) != 1 || len(rows[0].Risks) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	for _, risk := range rows[0].Risks {
		if risk.ForeignIdentity != safeIdentity || risk.ForeignIdentityRedacted {
			t.Fatalf("risk = %#v", risk)
		}
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []relationOrderJSON
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode JSON projection: %v\n%s", err, encoded)
	}
	if len(decoded) != 1 || len(decoded[0].Risks) != 2 ||
		decoded[0].Risks[0].ForeignIdentity != safeIdentity {
		t.Fatalf("decoded projection = %#v", decoded)
	}

	for _, options := range []HumanOptions{
		{},
		{Verbose: true},
	} {
		var output bytes.Buffer
		PrintRelationOrderActionsWithOptions(
			&output,
			[]reconcile.RelationOrderDecision{decision},
			options,
		)
		if text := output.String(); !strings.Contains(
			text,
			`foreign="npm:quote\"and\\\\slash"`,
		) {
			t.Fatalf("human output = %q", text)
		}
	}
}

func TestRelationOrderJSONDisclosesPhysicalSequenceAndForeignRisk(t *testing.T) {
	decision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision})
	if len(rows) != 1 {
		t.Fatalf("rows = %#v", rows)
	}
	row := rows[0]
	if row.ClassID != "extension:pi:project:packages" ||
		row.SequenceID != "pi:project:settings.packages" ||
		row.RuntimeMeaning != string(hostrelation.RuntimePrecedence) ||
		row.Kind != string(reconcile.OrderNormalize) ||
		len(row.DesiredMembers) != 2 ||
		len(row.ObservedMembers) != 2 ||
		row.ForeignRowCount != 1 ||
		len(row.Risks) != 2 ||
		row.Risks[0].Code != foreignPrecedenceChangeRisk ||
		row.Risks[0].ForeignIdentityRedacted ||
		!row.Risks[0].ManagedWasBefore ||
		row.Risks[0].ManagedWillBeBefore ||
		row.Risks[1].ManagedWasBefore ||
		!row.Risks[1].ManagedWillBeBefore ||
		!row.RequiresMutation ||
		row.BlocksOrdinaryApply {
		t.Fatalf("row = %#v", row)
	}
}

func TestPlanAndApplyJSONEnvelopesIncludeRelationOrderActions(t *testing.T) {
	decision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextDryRun,
		RelationOrders: []reconcile.RelationOrderDecision{decision},
	})
	if err != nil {
		t.Fatal(err)
	}

	var planOutput bytes.Buffer
	if err := PrintPlanJSON(&planOutput, PlanJSONInput{
		Command:        "status",
		Mode:           "status",
		Reconciliation: result,
	}); err != nil {
		t.Fatal(err)
	}
	var planPayload planJSONOutput
	if err := json.Unmarshal(planOutput.Bytes(), &planPayload); err != nil {
		t.Fatal(err)
	}
	if planPayload.SchemaVersion != contractversion.ReconciliationPlanJSON ||
		len(planPayload.RelationOrders) != 1 {
		t.Fatalf("plan payload = %#v", planPayload)
	}

	var applyOutput bytes.Buffer
	if err := PrintApplyResultJSON(&applyOutput, ApplyResultJSONInput{
		Reconciliation: result,
	}); err != nil {
		t.Fatal(err)
	}
	var applyPayload applyResultJSONOutput
	if err := json.Unmarshal(applyOutput.Bytes(), &applyPayload); err != nil {
		t.Fatal(err)
	}
	if applyPayload.SchemaVersion != contractversion.ApplyResultJSON ||
		len(applyPayload.RelationOrders) != 1 {
		t.Fatalf("apply payload = %#v", applyPayload)
	}
}

func TestPrintRelationOrderActionsDistinguishesRuntimeAndConfigOrder(t *testing.T) {
	runtimeDecision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	configDecision := presentRelationOrderDecision(
		t,
		target.TargetOpenCode,
		hostrelation.ConfigOrderOnly,
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{runtimeDecision, configDecision},
		HumanOptions{},
	)
	text := output.String()
	for _, want := range []string{
		"runtime extension precedence",
		"extension config order",
		"includes 2 managed/foreign precedence changes:",
		`managed="host_relation/test.extension/beta" foreign="foreign" managed_position=before -> after`,
		`managed="host_relation/test.extension/alpha" foreign="foreign" managed_position=after -> before`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintVerboseRelationOrderActionsDisclosesConcretePrecedenceChanges(
	t *testing.T,
) {
	decision := presentRelationOrderDecision(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{Verbose: true},
	)
	text := output.String()
	for _, want := range []string{
		"includes 2 managed/foreign precedence changes:",
		`managed="host_relation/test.extension/beta" foreign="foreign" managed_position=before -> after`,
		`managed="host_relation/test.extension/alpha" foreign="foreign" managed_position=after -> before`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("verbose output lacks %q:\n%s", want, text)
		}
	}
}

func TestPrintRelationOrderActionsDisclosesEveryForeignCrossingInStableOrder(
	t *testing.T,
) {
	decision := presentRelationOrderDecisionWithForeignIdentities(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
		[]string{"foreign-one", "foreign-two"},
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{},
	)
	text := output.String()
	wantInOrder := []string{
		`managed="host_relation/test.extension/beta" foreign="foreign-one" managed_position=before -> after`,
		`managed="host_relation/test.extension/beta" foreign="foreign-two" managed_position=before -> after`,
		`managed="host_relation/test.extension/alpha" foreign="foreign-one" managed_position=after -> before`,
		`managed="host_relation/test.extension/alpha" foreign="foreign-two" managed_position=after -> before`,
	}
	previous := -1
	for _, want := range wantInOrder {
		index := strings.Index(text, want)
		if index <= previous {
			t.Fatalf("risk %q is missing or out of order:\n%s", want, text)
		}
		previous = index
	}
}

func TestPrintRelationOrderActionsDoesNotInventZeroRiskDetails(t *testing.T) {
	decision := presentRelationOrderDecisionWithoutForeign(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{},
	)
	if text := output.String(); !strings.Contains(text, "normalize runtime extension precedence") ||
		strings.Contains(text, "managed_position=") ||
		strings.Contains(text, "precedence changes") {
		t.Fatalf("zero-risk output = %q", text)
	}
}

func TestPrintRelationOrderActionsDisclosesTypedObservationBlock(t *testing.T) {
	constraint := presentOrderConstraint(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     target.TargetPi,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonObservationUnavailable,
			Detail:     "settings file is malformed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{},
	)
	if text := output.String(); !strings.Contains(text, "blocked runtime extension precedence") ||
		!strings.Contains(text, "settings file is malformed") {
		t.Fatalf("output = %q", text)
	}
}

func TestRelationOrderJSONDisclosesTypedResourceLimitBlock(t *testing.T) {
	constraint := presentOrderConstraint(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
	)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("pi:project:settings.packages")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     target.TargetPi,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonResourceLimitExceeded,
			Detail:     "extension order resource limit exceeded: observed_rows observed=4097 limit=4096",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision})
	if len(rows) != 1 ||
		rows[0].Kind != string(reconcile.OrderBlocked) ||
		rows[0].Reason != string(reconcile.OrderReasonResourceLimitExceeded) ||
		!strings.Contains(rows[0].Detail, "observed_rows observed=4097 limit=4096") {
		t.Fatalf("resource-limit JSON row = %#v", rows)
	}
}

func presentRelationOrderDecision(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) reconcile.RelationOrderDecision {
	return presentRelationOrderDecisionWithForeignIdentities(
		t,
		selectedTarget,
		meaning,
		[]string{"foreign"},
	)
}

func presentRelationOrderDecisionWithoutForeign(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) reconcile.RelationOrderDecision {
	return presentRelationOrderDecisionWithForeignIdentities(
		t,
		selectedTarget,
		meaning,
		nil,
	)
}

func presentRelationOrderDecisionWithForeignIdentities(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
	foreignIdentities []string,
) reconcile.RelationOrderDecision {
	t.Helper()
	constraint := presentOrderConstraint(t, selectedTarget, meaning)
	members := constraint.Members()
	rows := []observerelation.ObservedRelationRow{
		presentOrderRow(t, members[1]),
	}
	for _, foreignIdentityValue := range foreignIdentities {
		foreignIdentity, err := hostrelation.NewHostLoadIdentity(foreignIdentityValue)
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := observerelation.NewObservedRelationRow(foreignIdentity)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, foreign)
	}
	rows = append(rows, presentOrderRow(t, members[0]))
	classPrefix := string(selectedTarget)
	sequenceSuffix := "settings.packages"
	if selectedTarget == target.TargetOpenCode {
		sequenceSuffix = "server.plugins"
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID(
		classPrefix + ":project:" + sequenceSuffix,
	)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := observerelation.NewSequenceAuthority(
		classPrefix + ":project:" + sequenceSuffix,
	)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := observerelation.NewSequenceRevision("sha256:present")
	if err != nil {
		t.Fatal(err)
	}
	sequence, err := observerelation.NewObservedRelationSequence(
		constraint.ClassID(),
		sequenceID,
		authority,
		revision,
		rows,
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewRelationOrderDecision(
		reconcile.RelationOrderDecisionInput{
			Target:     selectedTarget,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			Sequence:   sequence,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return decision
}

func presentOrderConstraint(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) hostrelation.RelationOrderConstraint {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID(
		"extension:" + string(selectedTarget) + ":project:packages",
	)
	if err != nil {
		t.Fatal(err)
	}
	members := []hostrelation.RelationOrderMember{
		presentOrderMember(t, "alpha", "alpha"),
		presentOrderMember(t, "beta", "beta"),
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"test-order-v1",
		meaning,
		members,
	)
	if err != nil {
		t.Fatal(err)
	}
	return constraint
}

func presentOrderMember(
	t testing.TB,
	key string,
	loadIdentityValue string,
) hostrelation.RelationOrderMember {
	t.Helper()
	subject, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"test.extension",
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	loadIdentity, err := hostrelation.NewHostLoadIdentity(loadIdentityValue)
	if err != nil {
		t.Fatal(err)
	}
	member, err := hostrelation.NewRelationOrderMember(subject, loadIdentity)
	if err != nil {
		t.Fatal(err)
	}
	return member
}

func presentOrderRow(
	t testing.TB,
	member hostrelation.RelationOrderMember,
) observerelation.ObservedRelationRow {
	t.Helper()
	row, err := observerelation.NewCorrelatedObservedRelationRow(
		member.HostLoadIdentity(),
		member.Subject(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return row
}
