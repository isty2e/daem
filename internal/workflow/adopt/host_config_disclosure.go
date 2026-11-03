package adopt

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

type HostConfigCaptureValue struct {
	Key                         output.KeyPath
	Before, After               string
	BeforePresent, AfterPresent bool
}

// Fingerprint binds approval to actual observations, not their masked display.
func (plan *HostConfigCapturePlan) Fingerprint() string {
	fields := [][]byte{
		[]byte("source"), []byte(plan.input.Name), []byte(plan.SourcePath()), []byte(plan.SourceReferent()), []byte(plan.input.Paths.ManifestPath),
		[]byte(fmt.Sprintf("%t:%t:%t:%t", plan.Initial(), plan.retire, plan.narrowing, plan.native.Exists())),
		plan.sourceBefore, plan.sourceAfter, plan.desiredBefore, plan.native.Content(), plan.manifestBefore, plan.manifestAfter,
		[]byte(fmt.Sprint(len(plan.selected))),
	}
	for _, key := range plan.selected {
		fields = append(fields, []byte(key.String()))
	}
	fields = append(fields, []byte(fmt.Sprint(len(plan.references))))
	for _, reference := range plan.references {
		fields = append(fields, []byte(reference.Resource), []byte(reference.Path))
	}
	return captureFingerprint(fields...)
}

// Length-prefix raw bytes so arbitrary Unix path spellings cannot collapse
// through a display or JSON string's UTF-8 replacement rules.
func captureFingerprint(fields ...[]byte) string {
	digest := sha256.New()
	digest.Write([]byte("daem-host-config-capture-v1:"))
	for _, field := range fields {
		fmt.Fprintf(digest, "%d:", len(field))
		digest.Write(field)
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}

// SelectedValues excludes unselected source and host siblings. Whole-document
// bytes have no value projection and must not be printed as an arbitrary diff.
func (plan *HostConfigCapturePlan) SelectedValues(ctx context.Context) ([]HostConfigCaptureValue, error) {
	if plan.config.Ownership() == hostconfig.OwnershipDocument {
		return nil, nil
	}
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: plan.config.Target(), Scope: plan.config.Scope(), Path: plan.config.Path(), Ownership: plan.config.Ownership(), Syntax: plan.config.Syntax()})
	if err != nil {
		return nil, err
	}
	syntax, err := configdocument.ParseSyntax(string(placement.Grammar()))
	if err != nil {
		return nil, err
	}
	before, err := configdocument.Absent(syntax, nativeconfig.DocumentLimits())
	if !plan.Initial() {
		before, err = configdocument.Parse(ctx, plan.desiredBefore, syntax, nativeconfig.DocumentLimits())
	}
	if err != nil {
		return nil, err
	}
	after, err := configdocument.Parse(ctx, plan.sourceAfter, syntax, nativeconfig.DocumentLimits())
	if err != nil {
		return nil, err
	}
	values := make([]HostConfigCaptureValue, 0, len(plan.selected))
	for _, key := range plan.selected {
		row := HostConfigCaptureValue{Key: key}
		for index, document := range []configdocument.Document{before, after} {
			value, present, err := document.Lookup(key)
			if err != nil {
				return nil, err
			}
			text := ""
			if present {
				canonical, err := value.CanonicalContext(ctx, nativeconfig.DocumentLimits())
				if err != nil {
					return nil, err
				}
				text, err = configdocument.DisplayValue(ctx, syntax, canonical, nativeconfig.DocumentLimits())
				if err != nil {
					return nil, err
				}
			}
			if index == 0 {
				row.Before, row.BeforePresent = text, present
			} else {
				row.After, row.AfterPresent = text, present
			}
		}
		values = append(values, row)
	}
	return values, nil
}
