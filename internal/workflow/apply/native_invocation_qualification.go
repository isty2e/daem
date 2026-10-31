package apply

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

var (
	errNativeInvocationKeyChanged     = errors.New("selected settings changed during host invocation")
	errNativeInvocationOutsideChanged = errors.New("host invocation changed settings outside its rewrite area")
)

type nativeInvocationKey struct {
	document aggregate.DocumentAddress
	path     output.KeyPath
	value    configdocument.Value
}

type nativeInvocationRewrite struct {
	aliases  []aggregate.DocumentAddress
	preserve func(context.Context, configdocument.Document, configdocument.Document) (bool, error)
}

func nativeInvocationKeys(ctx context.Context, sources []lockobserve.NativeSourceObservation) ([]nativeInvocationKey, error) {
	var keys []nativeInvocationKey
	for _, source := range sources {
		for _, projection := range source.Projections() {
			item, present, err := projection.ManagedAggregateContribution()
			if err != nil || !present {
				return nil, errors.Join(fmt.Errorf("native invocation requires selected key contributions"), err)
			}
			contribution := item.Contribution()
			syntaxName, native := aggregate.HostConfigSyntaxForCodec(contribution.CodecContractID())
			if !native {
				return nil, fmt.Errorf("invocation key is not a native contribution")
			}
			syntax, err := configdocument.ParseSyntax(syntaxName)
			if err != nil {
				return nil, err
			}
			value, err := configdocument.DecodeValue(ctx, syntax, contribution.CanonicalContribution(), nativeconfig.DocumentLimits())
			if err != nil {
				return nil, err
			}
			keys = append(keys, nativeInvocationKey{document: contribution.Contract().Address().Document(), path: contribution.Address().KeyPath(), value: value})
		}
	}
	return keys, nil
}

func qualifyNativeInvocationDocuments(ctx context.Context, keys []nativeInvocationKey, before, after map[aggregate.DocumentAddress]configdocument.Document, rewrite nativeInvocationRewrite) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, key := range keys {
		for _, documents := range []map[aggregate.DocumentAddress]configdocument.Document{before, after} {
			document, selected := documents[key.document]
			if !selected {
				return fmt.Errorf("%w: document %q was not observed", errNativeInvocationKeyChanged, key.document.AggregateRoot())
			}
			value, present, err := document.Lookup(key.path)
			if err != nil || !present || !value.Equal(key.value) {
				return errors.Join(fmt.Errorf("%w: %s at %q", errNativeInvocationKeyChanged, key.path.String(), key.document.AggregateRoot()), err)
			}
		}
	}

	addresses := make([]aggregate.DocumentAddress, 0, len(before))
	for address := range before {
		addresses = append(addresses, address)
	}
	slices.SortFunc(addresses, func(left, right aggregate.DocumentAddress) int {
		return strings.Compare(string(left.Target())+"\x00"+string(left.Scope())+"\x00"+left.AggregateRoot().String(), string(right.Target())+"\x00"+string(right.Scope())+"\x00"+right.AggregateRoot().String())
	})
	if len(before) != len(after) {
		return fmt.Errorf("%w: observed document membership changed", errNativeInvocationOutsideChanged)
	}
	for _, address := range addresses {
		current, present := after[address]
		if !present {
			return fmt.Errorf("%w: document %q was not observed", errNativeInvocationOutsideChanged, address.AggregateRoot())
		}
		var same bool
		var err error
		if rewrite.preserve != nil && slices.Contains(rewrite.aliases, address) {
			same, err = rewrite.preserve(ctx, before[address], current)
		} else {
			same, err = before[address].PreservesOutside(ctx, current, nil, nil)
		}
		if err != nil || !same {
			return errors.Join(fmt.Errorf("%w: %q", errNativeInvocationOutsideChanged, address.AggregateRoot()), err)
		}
	}
	return nil
}
