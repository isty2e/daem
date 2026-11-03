package executioncontext

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/executionidentity"
)

func TestHostIdentityNormalizationAndApplicationSpecificDigests(t *testing.T) {
	want := "12345678123412341234123456789abc"
	for _, raw := range []string{want, strings.ToUpper(want), "12345678-1234-1234-1234-123456789ABC\n"} {
		got, err := canonicalExecutionHostID(raw)
		if err != nil || got != want {
			t.Fatal("platform identity failed canonical normalization", err)
		}
	}
	for _, raw := range []string{"", strings.Repeat("0", 32), strings.Repeat("z", 32), "123456781234-1234-1234-123456789abc", "private-invalid-identity"} {
		_, err := canonicalExecutionHostID(raw)
		if err == nil || strings.Contains(err.Error(), raw) && raw != "" {
			t.Fatal("invalid host identity was accepted or exposed")
		}
	}
	host := executionContextDigest("host", "linux", want)
	if len(host) != 64 || host == executionContextDigest("visibility", "linux", want) ||
		host == executionContextDigest("host", "darwin", want) || host != executionContextDigest("host", "linux", want) {
		t.Fatal("execution identity derivation lost purpose, platform or stability")
	}
}

func TestZeroQuiescenceAndInvalidBindingsCannotQualify(t *testing.T) {
	invocation, _ := executionidentity.NewInvocation(strings.Repeat("a", 32))
	legacy, _ := executionidentity.NewContext(executionidentity.Linux, strings.Repeat("b", 64), "12345678-1234-1234-1234-123456789abc")
	binding, err := executionidentity.NewBinding(invocation, legacy, 123)
	if err != nil {
		t.Fatal(err)
	}
	if (ExecutionQuiescence{}).Require(binding) == nil {
		t.Fatal("zero observation acquired quiescence evidence")
	}
	observation, err := ObserveExecutionQuiescence(t.Context(), binding)
	if err == nil || observation.Require(binding) == nil {
		t.Fatal("legacy record acquired process evidence")
	}
}
