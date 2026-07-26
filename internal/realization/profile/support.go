package profile

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

// UnsupportedReason is a closed structural reason for an unavailable resource.
type UnsupportedReason string

const (
	UnsupportedReasonNotImplemented            UnsupportedReason = "not-implemented"
	UnsupportedReasonBridgeRequired            UnsupportedReason = "bridge-required"
	UnsupportedReasonDirectCLIRouteNotAdmitted UnsupportedReason = "direct-cli-route-not-admitted"
)

// Support is one target/resource support fact. It carries no human wording or
// authoring admission policy.
type Support struct {
	selectedTarget target.Target
	resourceKind   entity.Kind
	supported      bool
	reason         UnsupportedReason
}

// NewSupported constructs one supported target/resource fact.
func NewSupported(selectedTarget target.Target, resourceKind entity.Kind) (Support, error) {
	result := Support{selectedTarget: selectedTarget, resourceKind: resourceKind, supported: true}
	if err := result.Validate(); err != nil {
		return Support{}, err
	}
	return result, nil
}

// NewUnsupported constructs one unavailable target/resource fact.
func NewUnsupported(
	selectedTarget target.Target,
	resourceKind entity.Kind,
	reason UnsupportedReason,
) (Support, error) {
	result := Support{selectedTarget: selectedTarget, resourceKind: resourceKind, reason: reason}
	if err := result.Validate(); err != nil {
		return Support{}, err
	}
	return result, nil
}

// Validate rejects unsupported-without-reason and supported-with-reason states.
func (support Support) Validate() error {
	if _, err := target.ParseTarget(string(support.selectedTarget)); err != nil {
		return err
	}
	if _, err := entity.ParseKind(string(support.resourceKind)); err != nil {
		return err
	}
	if support.supported {
		if support.reason != "" {
			return fmt.Errorf("supported %s/%s must not carry unsupported reason %q", support.selectedTarget, support.resourceKind, support.reason)
		}
		return nil
	}
	switch support.reason {
	case UnsupportedReasonNotImplemented,
		UnsupportedReasonBridgeRequired,
		UnsupportedReasonDirectCLIRouteNotAdmitted:
		return nil
	default:
		return fmt.Errorf("unsupported %s/%s requires a closed reason", support.selectedTarget, support.resourceKind)
	}
}

func (support Support) Target() target.Target     { return support.selectedTarget }
func (support Support) ResourceKind() entity.Kind { return support.resourceKind }
func (support Support) Supported() bool           { return support.supported }
func (support Support) Reason() UnsupportedReason { return support.reason }
