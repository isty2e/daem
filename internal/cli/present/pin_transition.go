package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/reconcile"
)

type pinChangeJSON struct {
	FromSource         string   `json:"from_source"`
	FromSourceRedacted bool     `json:"from_source_redacted,omitempty"`
	ToSource           string   `json:"to_source"`
	ToSourceRedacted   bool     `json:"to_source_redacted,omitempty"`
	Resume             bool     `json:"resume"`
	PreviousProvenance string   `json:"previous_provenance"`
	EffectClasses      []string `json:"effect_classes"`
	NonClaims          []string `json:"non_claims"`
}

func pinChangeForAction(action reconcile.RelationAction) *pinChangeJSON {
	pair, present := action.PinTransition()
	if !present {
		return nil
	}
	before := carrierIdentityDisclosureFor(pair.Before().Identity())
	after := carrierIdentityDisclosureFor(pair.Identity())
	return &pinChangeJSON{
		FromSource: before.sourceRef.Value(), FromSourceRedacted: before.sourceRef.Redacted(),
		ToSource: after.sourceRef.Value(), ToSourceRedacted: after.sourceRef.Redacted(),
		Resume: action.ResumesPinTransition(), PreviousProvenance: string(pair.Before().Provenance()),
		EffectClasses: []string{"git_checkout_reset_and_clean", "dependency_install", "package_settings_replacement"},
		NonClaims:     []string{"automatic_host_rollback", "runtime_readiness", "complete_artifact_attestation"},
	}
}

func printPinChange(output io.Writer, action reconcile.RelationAction) {
	change := pinChangeForAction(action)
	if change == nil {
		return
	}
	fmt.Fprintf(output, "    pin change from=%q to=%q resume=%t\n", change.FromSource, change.ToSource, change.Resume)
	fmt.Fprintln(output, "    native install may reset/clean the checkout and run dependency scripts; no automatic rollback")
	if change.Resume {
		fmt.Fprintln(output, "    retained intent requires a new authorized native attempt; settings alone do not complete it")
	}
}
