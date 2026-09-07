package operationplan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestApplyFingerprintSerializersRejectMalformedOwnerProjections(t *testing.T) {
	t.Parallel()

	malformed := json.RawMessage(`{`)
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "apply",
			run: func() error {
				_, err := ApplyOperationFingerprint(ApplyIdentityInput{ManagedPaths: malformed})
				return err
			},
			want: "fingerprint apply plan:",
		},
		{
			name: "provider stable",
			run: func() error {
				_, err := ProviderStableOperationFingerprint(ProviderStableIdentityInput{
					ManagedPaths: malformed,
				})
				return err
			},
			want: "fingerprint post-provider apply plan:",
		},
		{
			name: "remaining execution",
			run: func() error {
				_, err := RemainingApplyOperationFingerprint(RemainingApplyIdentityInput{
					RelationOrders: malformed,
				})
				return err
			},
			want: "fingerprint remaining apply execution:",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want prefix %q", err, test.want)
			}
		})
	}
}
