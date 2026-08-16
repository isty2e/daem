package clipresent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/contractversion"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

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
	wantIdentity := identityDisclosureFor(unsafeIdentity).Value()
	if changes := decision.PrecedenceChanges(); len(changes) != 2 ||
		string(changes[0].ForeignIdentity()) != unsafeIdentity {
		t.Fatalf("canonical decision identity changed before projection: %#v", changes)
	}

	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
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

func TestRelationOrderRiskDisclosureRequiresTargetGrammarProof(t *testing.T) {
	const privateIdentity = "plugins/local.ts"
	decision := presentRelationOrderDecisionWithForeignIdentities(
		t,
		target.TargetOpenCode,
		hostrelation.ConfigOrderOnly,
		[]string{privateIdentity},
	)

	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
	if len(rows) != 1 || len(rows[0].Risks) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	for _, risk := range rows[0].Risks {
		if !risk.ForeignIdentityRedacted ||
			!strings.HasPrefix(risk.ForeignIdentity, "redacted:sha256:") ||
			strings.Contains(risk.ForeignIdentity, privateIdentity) {
			t.Fatalf("risk = %#v", risk)
		}
	}

	for _, options := range []HumanOptions{{}, {Verbose: true}} {
		var output bytes.Buffer
		PrintRelationOrderActionsWithOptions(
			&output,
			[]reconcile.RelationOrderDecision{decision},
			options,
		)
		if text := output.String(); strings.Contains(text, privateIdentity) ||
			!strings.Contains(text, "redacted:sha256:") {
			t.Fatalf("human output = %q", text)
		}
	}
}

func TestRelationOrderRiskDisclosureEscapesSafeIdentityAcrossFormats(
	t *testing.T,
) {
	const safeIdentity = "npm:@acme/quote-and-slash"
	decision := presentRelationOrderDecisionWithForeignIdentities(
		t,
		target.TargetPi,
		hostrelation.RuntimePrecedence,
		[]string{safeIdentity},
	)
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
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
			`foreign="npm:@acme/quote-and-slash"`,
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
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
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

func TestPlanAndApplyJSONRedactLocalRelationAndLoadIdentities(t *testing.T) {
	const (
		openCodeSource = "plugins/local.ts"
		piSource       = "packages/tools"
		piLoadIdentity = "/Users/alice/private/pi-extension"
	)
	actions := []reconcile.RelationAction{
		localHostSourceRelationAction(
			t,
			"opencode-local",
			desiredextension.CarrierOpenCodePlugin,
			target.TargetOpenCode,
			target.ScopeProject,
			openCodeSource,
		),
		localHostSourceRelationAction(
			t,
			"pi-local",
			desiredextension.CarrierPiPackage,
			target.TargetPi,
			target.ScopeGlobal,
			piSource,
		),
	}
	orders := []reconcile.RelationOrderDecision{
		presentBlockedOrderDecisionWithLoadIdentity(
			t,
			target.TargetOpenCode,
			hostrelation.ConfigOrderOnly,
			openCodeSource,
		),
		presentBlockedOrderDecisionWithLoadIdentity(
			t,
			target.TargetPi,
			hostrelation.RuntimePrecedence,
			piLoadIdentity,
		),
	}
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:        reconcile.ContextDryRun,
		Relations:      actions,
		RelationOrders: orders,
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
	assertLocalRelationIdentitiesRedacted(
		t,
		planOutput.Bytes(),
		planPayload.RelationActions,
		planPayload.RelationOrders,
		[]string{openCodeSource, piSource, piLoadIdentity},
	)

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
	assertLocalRelationIdentitiesRedacted(
		t,
		applyOutput.Bytes(),
		applyPayload.RelationActions,
		applyPayload.RelationOrders,
		[]string{openCodeSource, piSource, piLoadIdentity},
	)
}

func TestRelationOrderJSONPreservesProvenPublicCarrierIdentity(t *testing.T) {
	action := localHostSourceRelationAction(
		t,
		"opencode-plugin",
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		target.ScopeProject,
		"npm:@acme/plugin@^1.2.3",
	)
	loadIdentity, err := hostrelation.NewHostLoadIdentity("@acme/plugin")
	if err != nil {
		t.Fatal(err)
	}
	member, err := hostrelation.NewRelationOrderMember(action.Subject(), loadIdentity)
	if err != nil {
		t.Fatal(err)
	}

	rows := relationOrderJSONMembers(
		target.TargetOpenCode,
		mustPresentOrderClassID(t, "extension:opencode:project:plugins"),
		[]hostrelation.RelationOrderMember{member},
		publicRelationIdentitySubjects([]reconcile.RelationAction{action}),
	)
	if len(rows) != 1 || rows[0].HostLoadIdentity != "@acme/plugin" ||
		rows[0].HostLoadIdentityRedacted {
		t.Fatalf("relation order member = %#v", rows)
	}
}

func TestCarrierIdentityDisclosurePropagatesPrivateHostSource(t *testing.T) {
	for _, source := range []string{
		"./plugins/local.ts",
		"plugins/local.ts",
		`plugins\local.ts`,
		"/Users/alice/private/plugin.ts",
		`C:\Users\alice\private\plugin.ts`,
		"npm:foo@../../private/plugin",
		"npm:foo@file:../private/plugin.ts",
		"foo@/Users/alice/private/plugin.ts",
	} {
		t.Run(source, func(t *testing.T) {
			action := localHostSourceRelationAction(
				t,
				"local-opencode",
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				source,
			)
			disclosure := carrierIdentityDisclosureFor(action.CarrierIdentity())
			if !disclosure.sourceRef.Redacted() ||
				!disclosure.sourceNamespace.Redacted() ||
				!disclosure.relationSubjectKey.Redacted() ||
				disclosure.carrierSubject == nil ||
				!disclosure.carrierSubject.NameRedacted {
				t.Fatalf("carrier disclosure = %#v", disclosure)
			}

			result, err := reconcile.NewResult(reconcile.ResultInput{
				Context:   reconcile.ContextDryRun,
				Relations: []reconcile.RelationAction{action},
			})
			if err != nil {
				t.Fatal(err)
			}
			for name, print := range map[string]func(*bytes.Buffer) error{
				"plan": func(output *bytes.Buffer) error {
					return PrintPlanJSON(output, PlanJSONInput{
						Command:        "status",
						Mode:           "status",
						Reconciliation: result,
					})
				},
				"apply": func(output *bytes.Buffer) error {
					return PrintApplyResultJSON(output, ApplyResultJSONInput{
						Reconciliation: result,
					})
				},
			} {
				var output bytes.Buffer
				if err := print(&output); err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(output.Bytes(), []byte(source)) {
					t.Fatalf("%s JSON discloses host source %q: %s", name, source, output.Bytes())
				}
			}
		})
	}
}

func TestCarrierIdentityDisclosureTreatsPiPathSelectorAsPrivate(t *testing.T) {
	action := localHostSourceRelationAction(
		t,
		"private-pi-package",
		desiredextension.CarrierPiPackage,
		target.TargetPi,
		target.ScopeGlobal,
		"npm:../../escape",
	)
	disclosure := carrierIdentityDisclosureFor(action.CarrierIdentity())
	if !disclosure.sourceRef.Redacted() ||
		!disclosure.sourceNamespace.Redacted() ||
		!disclosure.relationSubjectKey.Redacted() ||
		disclosure.carrierSubject == nil ||
		!disclosure.carrierSubject.NameRedacted {
		t.Fatalf("carrier disclosure = %#v", disclosure)
	}
}

func TestCarrierIdentityDisclosurePreservesOpenCodePackageSources(t *testing.T) {
	for _, source := range []string{
		"opencode-wakatime",
		"@acme/opencode-plugin@1.2.3",
		"npm:@acme/opencode-plugin@1.2.3",
	} {
		t.Run(source, func(t *testing.T) {
			action := localHostSourceRelationAction(
				t,
				"package-opencode",
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				target.ScopeProject,
				source,
			)
			disclosure := carrierIdentityDisclosureFor(action.CarrierIdentity())
			if disclosure.sourceRef.Redacted() ||
				disclosure.sourceNamespace.Redacted() ||
				disclosure.relationSubjectKey.Redacted() ||
				disclosure.carrierSubject == nil ||
				disclosure.carrierSubject.NameRedacted {
				t.Fatalf("carrier disclosure = %#v", disclosure)
			}
		})
	}
}

func assertLocalRelationIdentitiesRedacted(
	t testing.TB,
	encoded []byte,
	actions []relationActionJSON,
	orders []relationOrderJSON,
	privateValues []string,
) {
	t.Helper()
	if len(actions) != 2 || len(orders) != 2 {
		t.Fatalf("relation actions/orders = %d/%d, want 2/2", len(actions), len(orders))
	}
	for _, action := range actions {
		if !action.SourceNamespaceRedacted ||
			!action.SourceRefRedacted ||
			!action.RelationSubjectKeyRedacted ||
			!strings.HasPrefix(action.SourceNamespace, "redacted:sha256:") ||
			!strings.HasPrefix(action.SourceRef, "redacted:sha256:") ||
			!strings.HasPrefix(action.RelationSubjectKey, "redacted:sha256:") {
			t.Fatalf("relation action disclosure = %#v", action)
		}
	}
	for _, order := range orders {
		if len(order.DesiredMembers) != 1 ||
			!order.DesiredMembers[0].HostLoadIdentityRedacted ||
			!strings.HasPrefix(
				order.DesiredMembers[0].HostLoadIdentity,
				"redacted:sha256:",
			) {
			t.Fatalf("relation order disclosure = %#v", order)
		}
	}
	for _, privateValue := range privateValues {
		if bytes.Contains(encoded, []byte(privateValue)) {
			t.Fatalf("JSON discloses local relation identity %q: %s", privateValue, encoded)
		}
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
		`managed="host_relation/test.extension/beta" foreign="npm:foreign" managed_position=before -> after`,
		`managed="host_relation/test.extension/alpha" foreign="npm:foreign" managed_position=after -> before`,
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
		[]string{"npm:foreign-one", "npm:foreign-two"},
	)
	var output bytes.Buffer
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{},
	)
	text := output.String()
	wantInOrder := []string{
		`managed="host_relation/test.extension/beta" foreign="npm:foreign-one" managed_position=before -> after`,
		`managed="host_relation/test.extension/beta" foreign="npm:foreign-two" managed_position=before -> after`,
		`managed="host_relation/test.extension/alpha" foreign="npm:foreign-one" managed_position=after -> before`,
		`managed="host_relation/test.extension/alpha" foreign="npm:foreign-two" managed_position=after -> before`,
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
			Detail:     "settings file is malformed\n\x1b[2J\u202e",
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
		!strings.Contains(text, "extension order could not be observed") ||
		strings.Contains(text, "settings file is malformed") ||
		strings.Contains(text, "\x1b") || strings.Contains(text, "\u202e") {
		t.Fatalf("output = %q", text)
	}

	output.Reset()
	PrintRelationOrderActionsWithOptions(
		&output,
		[]reconcile.RelationOrderDecision{decision},
		HumanOptions{Verbose: true},
	)
	if text := output.String(); !strings.Contains(text, `settings file is malformed\n\x1b[2J\u202e`) ||
		strings.Contains(text, "\x1b") || strings.Contains(text, "\u202e") {
		t.Fatalf("verbose output = %q", text)
	}
}

func TestRelationOrderMemberIdentitiesRedactPrivateHostIdentity(t *testing.T) {
	member := presentOrderMember(t, "private", "/Users/alice/.pi/extensions/private.ts")

	identities := relationOrderMemberIdentities(
		target.TargetPi,
		mustPresentOrderClassID(t, "extension:pi:project:packages"),
		[]hostrelation.RelationOrderMember{member},
	)
	if len(identities) != 1 ||
		!strings.Contains(identities[0], "redacted:sha256:") ||
		strings.Contains(identities[0], "/Users/alice") {
		t.Fatalf("member identities = %#v", identities)
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
	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
	if len(rows) != 1 ||
		rows[0].Kind != string(reconcile.OrderBlocked) ||
		rows[0].Reason != string(reconcile.OrderReasonResourceLimitExceeded) ||
		rows[0].Detail != "extension order observation exceeded its resource limit" {
		t.Fatalf("resource-limit JSON row = %#v", rows)
	}
}

func TestRelationOrderJSONDoesNotExposePathBearingObservationDetail(t *testing.T) {
	constraint := presentOrderConstraint(
		t,
		target.TargetOpenCode,
		hostrelation.ConfigOrderOnly,
	)
	sequenceID, err := hostrelation.NewPhysicalSequenceID("opencode:project:server.plugins")
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     target.TargetOpenCode,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonObservationUnavailable,
			Detail:     "open /Users/alice/.config/opencode/opencode.json: permission denied",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	rows := relationOrderJSONActions([]reconcile.RelationOrderDecision{decision}, nil)
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Detail != "extension order could not be observed" ||
		bytes.Contains(encoded, []byte("/Users/alice")) {
		t.Fatalf("relation-order JSON = %s", encoded)
	}
}

func presentRelationOrderDecision(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
) reconcile.RelationOrderDecision {
	foreignIdentity := "foreign"
	if selectedTarget == target.TargetPi {
		foreignIdentity = "npm:foreign"
	}
	return presentRelationOrderDecisionWithForeignIdentities(
		t,
		selectedTarget,
		meaning,
		[]string{foreignIdentity},
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

func presentBlockedOrderDecisionWithLoadIdentity(
	t testing.TB,
	selectedTarget target.Target,
	meaning hostrelation.RuntimeMeaning,
	loadIdentity string,
) reconcile.RelationOrderDecision {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID(
		"extension:" + string(selectedTarget) + ":local:test",
	)
	if err != nil {
		t.Fatal(err)
	}
	constraint, err := hostrelation.NewRelationOrderConstraint(
		classID,
		"test-order-v1",
		meaning,
		[]hostrelation.RelationOrderMember{
			presentOrderMember(t, string(selectedTarget)+"-local", loadIdentity),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	sequenceID, err := hostrelation.NewPhysicalSequenceID(
		string(selectedTarget) + ":local:test",
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := reconcile.NewBlockedRelationOrderDecision(
		reconcile.BlockedRelationOrderDecisionInput{
			Target:     selectedTarget,
			Scope:      target.ScopeProject,
			Constraint: constraint,
			SequenceID: sequenceID,
			Reason:     reconcile.OrderReasonObservationUnavailable,
			Detail:     "local extension order could not be observed",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return decision
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
	className := "extension:" + string(selectedTarget) + ":project:packages"
	if selectedTarget == target.TargetOpenCode {
		className = "extension:opencode:project:plugins"
	}
	classID := mustPresentOrderClassID(t, className)
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

func mustPresentOrderClassID(
	t testing.TB,
	value string,
) hostrelation.OrderClassID {
	t.Helper()
	classID, err := hostrelation.NewOrderClassID(value)
	if err != nil {
		t.Fatal(err)
	}
	return classID
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
