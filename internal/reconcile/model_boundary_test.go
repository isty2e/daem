package reconcile

import (
	"reflect"
	"strings"
	"testing"
)

func TestResultModelsDoNotCarryExecutionOrJournalEvidence(t *testing.T) {
	for _, model := range []struct {
		name      string
		modelType reflect.Type
	}{
		{name: "Result", modelType: reflect.TypeFor[Result]()},
		{name: "ManagedPathDecision", modelType: reflect.TypeFor[ManagedPathDecision]()},
		{name: "AggregateDecision", modelType: reflect.TypeFor[AggregateDecision]()},
	} {
		t.Run(model.name, func(t *testing.T) {
			assertNoForbiddenPlanFields(t, model.modelType)
			assertNoExecutionOrJournalFieldTypes(t, model.modelType)
		})
	}
}

func TestResultDoesNotExposeMutableDecisionCollections(t *testing.T) {
	modelType := reflect.TypeFor[Result]()
	for index := 0; index < modelType.NumField(); index++ {
		field := modelType.Field(index)
		if field.IsExported() {
			t.Fatalf("Result.%s exposes mutable collection authority", field.Name)
		}
	}
}

func assertNoForbiddenPlanFields(t *testing.T, modelType reflect.Type) {
	t.Helper()

	forbiddenParts := []string{
		"expectedpath",
		"exitcode",
		"stdout",
		"stderr",
		"command",
		"argv",
		"responsebody",
		"observedpostcondition",
		"observedafter",
		"physicalpath",
		"resolvedpath",
		"backuppath",
		"journalpath",
		"recoveryclassification",
		"observedat",
		"readiness",
		"secret",
		"authsession",
		"trustdecision",
		"recoveryevidence",
		"payload",
		"processresult",
		"pid",
	}

	for index := 0; index < modelType.NumField(); index++ {
		field := modelType.Field(index)
		normalized := strings.ToLower(field.Name)
		for _, forbidden := range forbiddenParts {
			if strings.Contains(normalized, forbidden) {
				t.Fatalf("%s.%s carries forbidden execution/journal evidence marker %q", modelType.Name(), field.Name, forbidden)
			}
		}
	}
}

func assertNoExecutionOrJournalFieldTypes(t *testing.T, modelType reflect.Type) {
	t.Helper()

	for index := 0; index < modelType.NumField(); index++ {
		fieldType := planBoundaryBaseType(modelType.Field(index).Type)
		packagePath := fieldType.PkgPath()
		if strings.Contains(packagePath, "/internal/effect/execute") || strings.Contains(packagePath, "/internal/effect/journal") {
			t.Fatalf("%s field %s depends on execution/journal type %s", modelType.Name(), modelType.Field(index).Name, fieldType)
		}
	}
}

func planBoundaryBaseType(fieldType reflect.Type) reflect.Type {
	for fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
		fieldType = fieldType.Elem()
	}
	return fieldType
}
