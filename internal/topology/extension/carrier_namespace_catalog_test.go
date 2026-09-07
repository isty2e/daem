package extension

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
)

func TestCarrierNamespaceCoversClosedCarrierVocabulary(t *testing.T) {
	t.Parallel()

	seen := make(map[string]desiredextension.Carrier)
	for _, carrier := range desiredextension.SupportedCarriers() {
		namespace, ok := CarrierNamespace(carrier)
		if !ok || namespace == "" {
			t.Fatalf("carrier %q has no namespace", carrier)
		}
		if previous, duplicate := seen[namespace]; duplicate {
			t.Fatalf("namespace %q is shared by %q and %q", namespace, previous, carrier)
		}
		seen[namespace] = carrier
	}
	if _, ok := CarrierNamespace(desiredextension.Carrier("forged")); ok {
		t.Fatal("forged carrier has a namespace")
	}
}
