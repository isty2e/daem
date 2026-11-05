package qualification

import "testing"

func TestEmptyQualifiedDocumentsExposeNoNamespace(t *testing.T) {
	var documents Documents
	if documents.Len() != 0 || documents.Records().Len() != 0 {
		t.Fatal("empty qualification supplied document facts")
	}
	for _, index := range []int{-1, 0, 1} {
		namespace, present := documents.NamespaceAt(index)
		if present || namespace.Observation().Validate() == nil {
			t.Fatal("empty qualification supplied a namespace", index)
		}
	}
}
