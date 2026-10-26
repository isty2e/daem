package build

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/target"
)

type cancelAfterNativeKeyValidation struct {
	validator aggregate.ContributionValidator
	cancel    context.CancelFunc
	validated *bool
}

func (validator cancelAfterNativeKeyValidation) ContractID() aggregate.CodecContractID {
	return validator.validator.ContractID()
}

func (validator cancelAfterNativeKeyValidation) ValidateContributions(set aggregate.ContributionSet) error {
	if err := validator.validator.ValidateContributions(set); err != nil {
		return err
	}
	*validator.validated = true
	validator.cancel()
	return nil
}

func TestNativeKeyCodecCancellationDuringValueValidationReturnsNoResult(t *testing.T) {
	dir := t.TempDir()
	config := nativeDocumentConfig(t, "native", nativeLocalSource(t, dir, "source", []byte(`{"a":"literal-value"}`), 0o600), target.TargetPi, target.ScopeProject, "custom.json", hostconfig.OwnershipKeys, hostconfig.SyntaxJSON)
	file, err := BuildNativeDocumentsWithOptions(t.Context(), lockEnvironment(t, desired.Spec{HostConfigs: []hostconfig.Config{config}}), nativeDocumentResolver(t, dir), Options{}, nativeDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertNativeKeyRoundtrip(t, dir, file)

	var required []aggregate.CodecContractID
	for _, contract := range file.Locked.Subjects() {
		if contribution, present, err := contract.ManagedAggregateContribution(); err != nil {
			t.Fatal(err)
		} else if present {
			required = append(required, contribution.Contribution().CodecContractID())
		}
	}
	if len(required) != 1 {
		t.Fatal("fixture requires one real native key validator")
	}
	catalog, err := aggregatecodec.ForContributions(t.Context(), required, nativeDocumentLimits())
	if err != nil {
		t.Fatal(err)
	}
	validator, present := catalog.Lookup(required[0])
	if !present {
		t.Fatal("native key validator is unavailable")
	}

	for _, operation := range []string{"load", "marshal"} {
		t.Run(operation, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			validated := false
			cancelingCatalog, err := aggregate.NewContributionCatalog([]aggregate.ContributionValidator{
				cancelAfterNativeKeyValidation{validator: validator, cancel: cancel, validated: &validated},
			})
			if err != nil {
				t.Fatal(err)
			}
			codec, err := lockfile.NewNativeCodec(cancelingCatalog)
			if err != nil {
				t.Fatal(err)
			}

			switch operation {
			case "load":
				result, err := codec.Load(ctx, filepath.Join(dir, "native.lock.toml"))
				if !errors.Is(err, context.Canceled) || result.Version != 0 || result.Locked.Len() != 0 {
					t.Fatalf("cancellation during native validation exposed a lock: version=%d subjects=%d err=%v", result.Version, result.Locked.Len(), err)
				}
			case "marshal":
				content, err := codec.Marshal(ctx, file)
				if !errors.Is(err, context.Canceled) || content != nil {
					t.Fatal("cancellation during native validation exposed bytes", err)
				}
			}
			if !validated || !errors.Is(ctx.Err(), context.Canceled) {
				t.Fatal("cancellation did not occur inside actual native value validation")
			}
		})
	}
}
