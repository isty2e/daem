package claudeplugin_test

import (
	"fmt"
	"strings"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

func TestCorrelateExactManagedRequiresSubjectAndManagedKey(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	row := mustRow(t, observeclaudeplugin.RowSpec{
		SubjectKey:            "context7",
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(subject.ExpectedRelation().ManagedInstanceKey()),
		Scope:                 observeclaudeplugin.HostScopeProject,
	})
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         []observeclaudeplugin.Row{row},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateExactCorrelation, observerelation.ReasonNone)
	if len(result.SameSubjectRows()) != 1 || len(result.ManagedKeyRows()) != 1 {
		t.Fatalf("expected exact row indexes, got same-name=%d managed-key=%d", len(result.SameSubjectRows()), len(result.ManagedKeyRows()))
	}
}

func TestRowNormalizesClaudeHostInventoryScope(t *testing.T) {
	tests := []struct {
		name  string
		scope observeclaudeplugin.HostScope
		want  observeclaudeplugin.HostScope
	}{
		{name: "missing scope becomes unknown", want: observeclaudeplugin.HostScopeUnknown},
		{name: "explicit unknown", scope: observeclaudeplugin.HostScopeUnknown, want: observeclaudeplugin.HostScopeUnknown},
		{name: "project", scope: observeclaudeplugin.HostScopeProject, want: observeclaudeplugin.HostScopeProject},
		{name: "user", scope: observeclaudeplugin.HostScopeUser, want: observeclaudeplugin.HostScopeUser},
		{name: "local", scope: observeclaudeplugin.HostScopeLocal, want: observeclaudeplugin.HostScopeLocal},
		{name: "managed", scope: observeclaudeplugin.HostScopeManaged, want: observeclaudeplugin.HostScopeManaged},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			row := mustRow(t, observeclaudeplugin.RowSpec{
				SubjectKey: "context7",
				Scope:      test.scope,
			})
			if row.HostScope() != test.want {
				t.Fatalf("HostScope() = %q, want %q", row.HostScope(), test.want)
			}
		})
	}
}

func TestInventoryDistinguishesUnknownRowScopeFromUnavailableBatch(t *testing.T) {
	unknownScopeInventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustRow(t, observeclaudeplugin.RowSpec{SubjectKey: "context7"}),
		},
	})
	if unknownScopeInventory.Availability() != observerelation.InventorySupported {
		t.Fatalf("unknown-scope inventory availability = %q, want supported", unknownScopeInventory.Availability())
	}
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	unknownScopeResult := observeclaudeplugin.Correlate(subject, unknownScopeInventory)
	assertState(t, unknownScopeResult, observerelation.StateMissing, observerelation.ReasonMissing)

	unavailableInventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	})
	if unavailableInventory.Availability() != observerelation.InventoryUnavailable {
		t.Fatalf("unavailable inventory availability = %q, want unavailable", unavailableInventory.Availability())
	}
	unavailableResult := observeclaudeplugin.Correlate(subject, unavailableInventory)
	assertState(t, unavailableResult, observerelation.StateUnavailableEvidence, observerelation.ReasonUnavailableEvidence)
}

func TestCorrelateUnkeyedSameSubjectProducesNoAdoptionWatchpoint(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustUnmanagedRow(t, "context7"),
		},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateUnkeyedSameSubject, observerelation.ReasonUnkeyedSameSubject)
	assertWatchpoints(t, result, nil)
}

func TestCorrelateMapsDaemProjectAndGlobalToClaudeHostScopes(t *testing.T) {
	projectSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeProject)
	globalSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeGlobal)
	tests := []struct {
		name    string
		subject realization.DelegatedRelation
		row     observeclaudeplugin.Row
	}{
		{
			name:    "project desired matches host project",
			subject: projectSubject,
			row:     mustManagedRowWithScope(t, "context7", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
		},
		{
			name:    "global desired matches host user",
			subject: globalSubject,
			row:     mustManagedRowWithScope(t, "context7", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         []observeclaudeplugin.Row{test.row},
			})

			result := observeclaudeplugin.Correlate(test.subject, inventory)
			assertState(t, result, observerelation.StateExactCorrelation, observerelation.ReasonNone)
		})
	}
}

func TestCorrelateIgnoresWrongScopeSamePluginKey(t *testing.T) {
	projectSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeProject)
	globalSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeGlobal)
	tests := []struct {
		name    string
		subject realization.DelegatedRelation
		row     observeclaudeplugin.Row
	}{
		{
			name:    "host project row with matching managed key does not satisfy daem global",
			subject: globalSubject,
			row:     mustManagedRowWithScope(t, "context7", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
		},
		{
			name:    "host user row with matching managed key does not satisfy daem project",
			subject: projectSubject,
			row:     mustManagedRowWithScope(t, "context7", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
		},
		{
			name:    "host project row does not satisfy daem global",
			subject: globalSubject,
			row:     mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeProject),
		},
		{
			name:    "host user row does not satisfy daem project",
			subject: projectSubject,
			row:     mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeUser),
		},
		{
			name:    "host local row does not satisfy daem project",
			subject: projectSubject,
			row:     mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeLocal),
		},
		{
			name:    "host managed row does not satisfy daem global",
			subject: globalSubject,
			row:     mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeManaged),
		},
		{
			name:    "unknown row scope does not satisfy daem project",
			subject: projectSubject,
			row:     mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeUnknown),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         []observeclaudeplugin.Row{test.row},
			})

			result := observeclaudeplugin.Correlate(test.subject, inventory)
			assertState(t, result, observerelation.StateMissing, observerelation.ReasonMissing)
			if len(result.Rows()) != 0 || len(result.SameSubjectRows()) != 0 || len(result.ManagedKeyRows()) != 0 {
				t.Fatalf(
					"wrong-scope rows leaked into correlation: rows=%d same-name=%d managed-key=%d",
					len(result.Rows()),
					len(result.SameSubjectRows()),
					len(result.ManagedKeyRows()),
				)
			}
			if len(result.Watchpoints()) != 0 {
				t.Fatalf("wrong-scope row produced blockers/watchpoints: %v", result.Watchpoints())
			}
		})
	}
}

func TestCorrelateIgnoresWrongScopeNoiseWhenExactScopedRowExists(t *testing.T) {
	projectSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeProject)
	globalSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeGlobal)
	tests := []struct {
		name    string
		subject realization.DelegatedRelation
		rows    []observeclaudeplugin.Row
	}{
		{
			name:    "project exact row ignores host user local managed and unknown noise",
			subject: projectSubject,
			rows: []observeclaudeplugin.Row{
				mustManagedRowWithScope(t, "context7", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				mustManagedRowWithScope(t, "context7", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeLocal),
				mustManagedRowWithScope(t, "context7-copy", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeManaged),
				mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeUnknown),
			},
		},
		{
			name:    "global exact host user row ignores project local managed and unknown noise",
			subject: globalSubject,
			rows: []observeclaudeplugin.Row{
				mustManagedRowWithScope(t, "context7", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				mustManagedRowWithScope(t, "context7", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeLocal),
				mustManagedRowWithScope(t, "context7-copy", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeManaged),
				mustUnmanagedRowWithScope(t, "context7", observeclaudeplugin.HostScopeUnknown),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         test.rows,
			})

			result := observeclaudeplugin.Correlate(test.subject, inventory)
			assertState(t, result, observerelation.StateExactCorrelation, observerelation.ReasonNone)
			if len(result.Rows()) != 1 || len(result.SameSubjectRows()) != 1 || len(result.ManagedKeyRows()) != 1 {
				t.Fatalf(
					"scoped rows = %d same-name=%d managed-key=%d, want exactly the scoped exact row",
					len(result.Rows()),
					len(result.SameSubjectRows()),
					len(result.ManagedKeyRows()),
				)
			}
			if len(result.Watchpoints()) != 0 {
				t.Fatalf("wrong-scope noise produced watchpoints: %v", result.Watchpoints())
			}
		})
	}
}

func TestCorrelateIgnoresWrongScopeAmbiguity(t *testing.T) {
	projectSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeProject)
	globalSubject := mustDelegatedRelationWithScope(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7", target.ScopeGlobal)
	tests := []struct {
		name    string
		subject realization.DelegatedRelation
		rows    []observeclaudeplugin.Row
	}{
		{
			name:    "project ignores ambiguous host user rows",
			subject: projectSubject,
			rows: []observeclaudeplugin.Row{
				mustManagedRowWithScope(t, "context7", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
				mustManagedRowWithScope(t, "context7-copy", string(projectSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeUser),
			},
		},
		{
			name:    "global ignores ambiguous host project rows",
			subject: globalSubject,
			rows: []observeclaudeplugin.Row{
				mustManagedRowWithScope(t, "context7", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
				mustManagedRowWithScope(t, "context7-copy", string(globalSubject.ExpectedRelation().ManagedInstanceKey()), observeclaudeplugin.HostScopeProject),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
				Availability: observerelation.InventorySupported,
				Freshness:    observerelation.EvidenceFresh,
				Rows:         test.rows,
			})

			result := observeclaudeplugin.Correlate(test.subject, inventory)
			assertState(t, result, observerelation.StateMissing, observerelation.ReasonMissing)
			if len(result.Rows()) != 0 || len(result.SameSubjectRows()) != 0 || len(result.ManagedKeyRows()) != 0 {
				t.Fatalf(
					"wrong-scope ambiguity leaked into scoped correlation: rows=%d same-name=%d managed-key=%d",
					len(result.Rows()),
					len(result.SameSubjectRows()),
					len(result.ManagedKeyRows()),
				)
			}
			if len(result.Watchpoints()) != 0 {
				t.Fatalf("wrong-scope ambiguity produced watchpoints: %v", result.Watchpoints())
			}
		})
	}
}

func TestCorrelateDetectsSameNameShadowAndMarketplaceSourceCollision(t *testing.T) {
	marketplace := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	hostSource := mustDelegatedRelation(t, desiredextension.SourceKindHostSource, "https://github.com/acme/context7.git", "context7", "context7")
	row := mustRow(t, observeclaudeplugin.RowSpec{
		SubjectKey:            "context7",
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    string(hostSource.ExpectedRelation().ManagedInstanceKey()),
		Scope:                 observeclaudeplugin.HostScopeProject,
	})
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			row,
		},
	})

	result := observeclaudeplugin.Correlate(marketplace, inventory)
	assertState(t, result, observerelation.StateSameSubjectShadow, observerelation.ReasonSameSubjectShadow)
	assertWatchpoints(t, result, []observerelation.Watchpoint{observerelation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsSamePluginKeyDifferentDeclarationID(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	otherDeclaration := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7-managed", "context7")
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustManagedRow(t, "context7", string(otherDeclaration.ExpectedRelation().ManagedInstanceKey())),
		},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateSameSubjectShadow, observerelation.ReasonSameSubjectShadow)
	assertWatchpoints(t, result, []observerelation.Watchpoint{observerelation.WatchpointReplacementAuthorityRequired})
	if len(result.SameSubjectRows()) != 1 || len(result.ManagedKeyRows()) != 0 {
		t.Fatalf(
			"rows = same-name %d managed-key %d, want visible-key shadow without managed-key match",
			len(result.SameSubjectRows()),
			len(result.ManagedKeyRows()),
		)
	}
}

func TestCorrelateDetectsExactManagedRowShadowedBySameNameRow(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustManagedRow(t, "context7", string(subject.ExpectedRelation().ManagedInstanceKey())),
			mustUnmanagedRow(t, "context7"),
		},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateSameSubjectShadow, observerelation.ReasonSameSubjectShadow)
}

func TestCorrelateDetectsManagedKeyDrift(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustManagedRow(t, "renamed-context7", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateManagedKeyDrift, observerelation.ReasonManagedKeyDrift)
	assertWatchpoints(t, result, []observerelation.Watchpoint{observerelation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateDetectsAmbiguousManagedKeyRows(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	inventory := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustManagedRow(t, "context7", string(subject.ExpectedRelation().ManagedInstanceKey())),
			mustManagedRow(t, "context7-copy", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})

	result := observeclaudeplugin.Correlate(subject, inventory)
	assertState(t, result, observerelation.StateAmbiguous, observerelation.ReasonAmbiguous)
	assertWatchpoints(t, result, []observerelation.Watchpoint{observerelation.WatchpointReplacementAuthorityRequired})
}

func TestCorrelateBlocksStaleAndUnsupportedEvidenceBeforeRows(t *testing.T) {
	subject := mustDelegatedRelation(t, desiredextension.SourceKindMarketplace, "context7", "context7", "context7")
	stale := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceStale,
		Rows: []observeclaudeplugin.Row{
			mustManagedRow(t, "context7", string(subject.ExpectedRelation().ManagedInstanceKey())),
		},
	})
	unsupported := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	unavailable := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
	})
	unavailableAndStale := mustInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceStale,
	})

	staleResult := observeclaudeplugin.Correlate(subject, stale)
	assertState(t, staleResult, observerelation.StateStaleEvidence, observerelation.ReasonStaleEvidence)
	assertWatchpoints(t, staleResult, []observerelation.Watchpoint{observerelation.WatchpointFreshInventoryRequired})

	unsupportedResult := observeclaudeplugin.Correlate(subject, unsupported)
	assertState(t, unsupportedResult, observerelation.StateUnsupported, observerelation.ReasonUnsupportedInventory)
	assertWatchpoints(t, unsupportedResult, []observerelation.Watchpoint{observerelation.WatchpointPassiveInventoryRequired})

	unavailableResult := observeclaudeplugin.Correlate(subject, unavailable)
	assertState(t, unavailableResult, observerelation.StateUnavailableEvidence, observerelation.ReasonUnavailableEvidence)
	assertWatchpoints(t, unavailableResult, []observerelation.Watchpoint{observerelation.WatchpointRelationEvidenceRequired})

	unavailableStaleResult := observeclaudeplugin.Correlate(subject, unavailableAndStale)
	assertState(t, unavailableStaleResult, observerelation.StateStaleEvidence, observerelation.ReasonStaleEvidence)
	assertWatchpoints(t, unavailableStaleResult, []observerelation.Watchpoint{observerelation.WatchpointFreshInventoryRequired})
}

func TestInventoryAndRowsRejectInvalidFactShapes(t *testing.T) {
	if _, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey:         "context7",
		ManagedInstanceKey: "managed",
	}); err == nil {
		t.Fatalf("NewRow accepted managed key without presence flag")
	}
	if _, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
		SubjectKey: "context\n7",
	}); err == nil {
		t.Fatalf("NewRow accepted control character in subject key")
	}
	for _, invalidScope := range []observeclaudeplugin.HostScope{
		"global",
		"User",
		" project ",
	} {
		if _, err := observeclaudeplugin.NewRow(observeclaudeplugin.RowSpec{
			SubjectKey: "context7",
			Scope:      invalidScope,
		}); err == nil {
			t.Fatalf("NewRow accepted unsupported Claude host inventory scope %q", invalidScope)
		}
	}
	if _, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryAvailability("maybe"),
		Freshness:    observerelation.EvidenceFresh,
	}); err == nil {
		t.Fatalf("NewInventory accepted unsupported availability")
	}
	if _, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnsupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustUnmanagedRow(t, "context7"),
		},
	}); err == nil {
		t.Fatalf("NewInventory accepted rows for unsupported inventory")
	}
	if _, err := observeclaudeplugin.NewInventory(observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventoryUnavailable,
		Freshness:    observerelation.EvidenceFresh,
		Rows: []observeclaudeplugin.Row{
			mustUnmanagedRow(t, "context7"),
		},
	}); err == nil {
		t.Fatalf("NewInventory accepted rows for unavailable inventory")
	}
}

func mustDelegatedRelation(
	t *testing.T,
	kind desiredextension.SourceKind,
	sourceRef string,
	declarationID string,
	pluginKey string,
) realization.DelegatedRelation {
	t.Helper()
	return mustDelegatedRelationWithScope(t, kind, sourceRef, declarationID, pluginKey, target.ScopeProject)
}

func mustDelegatedRelationWithScope(
	t *testing.T,
	kind desiredextension.SourceKind,
	sourceRef string,
	declarationID string,
	pluginKey string,
	scope target.Scope,
) realization.DelegatedRelation {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey(pluginKey)
	if err != nil {
		t.Fatalf("NewSubjectKey: %v", err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey(fmt.Sprintf(
		"test:%s:%s:%s:%s",
		kind,
		sourceRef,
		declarationID,
		scope,
	))
	if err != nil {
		t.Fatalf("NewManagedInstanceKey: %v", err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatalf("NewExpectedRelation: %v", err)
	}
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:          "test-carrier",
		Target:               target.TargetClaudeCode,
		Scope:                scope,
		SourceNamespace:      string(kind) + ":" + sourceRef,
		ExpectedRelation:     expected,
		RouteID:              "test.carrier.install",
		RouteContractVersion: "test-carrier-v1",
		CanonicalRequestHash: "sha256:" + strings.Repeat("a", 64),
		VerifiedRelationFields: []string{
			"managed_instance_key",
			"relation_subject_key",
			"scope",
			"source_kind",
			"source_ref",
			"target",
		},
	})
	if err != nil {
		t.Fatalf("NewDelegatedRelation: %v", err)
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatalf("NewDelegatedRelation returned %q realization", realization.Kind())
	}
	return relation
}

func mustManagedRow(t *testing.T, subjectKey string, managedKey string) observeclaudeplugin.Row {
	t.Helper()
	return mustManagedRowWithScope(t, subjectKey, managedKey, observeclaudeplugin.HostScopeProject)
}

func mustManagedRowWithScope(
	t *testing.T,
	subjectKey string,
	managedKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	return mustRow(t, observeclaudeplugin.RowSpec{
		SubjectKey:            subjectKey,
		HasManagedInstanceKey: true,
		ManagedInstanceKey:    managedKey,
		Scope:                 scope,
	})
}

func mustUnmanagedRow(t *testing.T, subjectKey string) observeclaudeplugin.Row {
	t.Helper()
	return mustUnmanagedRowWithScope(t, subjectKey, observeclaudeplugin.HostScopeProject)
}

func mustUnmanagedRowWithScope(
	t *testing.T,
	subjectKey string,
	scope observeclaudeplugin.HostScope,
) observeclaudeplugin.Row {
	t.Helper()
	return mustRow(t, observeclaudeplugin.RowSpec{SubjectKey: subjectKey, Scope: scope})
}

func mustRow(t *testing.T, spec observeclaudeplugin.RowSpec) observeclaudeplugin.Row {
	t.Helper()
	row, err := observeclaudeplugin.NewRow(spec)
	if err != nil {
		t.Fatalf("NewRow: %v", err)
	}
	return row
}

func mustInventory(t *testing.T, spec observeclaudeplugin.InventorySpec) observeclaudeplugin.Inventory {
	t.Helper()
	inventory, err := observeclaudeplugin.NewInventory(spec)
	if err != nil {
		t.Fatalf("NewInventory: %v", err)
	}
	return inventory
}

func assertState(
	t *testing.T,
	result observerelation.CorrelationResult,
	state observerelation.CorrelationState,
	reason observerelation.ReasonCode,
) {
	t.Helper()
	if result.State() != state || result.Reason() != reason {
		t.Fatalf("state = (%q, %q), want (%q, %q)", result.State(), result.Reason(), state, reason)
	}
}

func assertWatchpoints(
	t *testing.T,
	result observerelation.CorrelationResult,
	want []observerelation.Watchpoint,
) {
	t.Helper()
	got := result.Watchpoints()
	if len(got) != len(want) {
		t.Fatalf("watchpoints length = %d, want %d: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("watchpoints[%d] = %q, want %q: %v", index, got[index], want[index], got)
		}
	}
}
