package rootedpath

import "errors"

var errMountIdentityUnsupported = errors.New("native mount identity is unavailable")

// recoveryMountEvidence preserves whether durable mount identity was captured
// and, when it was not, the boundary cause that prevented capture.
type recoveryMountEvidence struct {
	token identityToken
	cause error
}

func availableRecoveryMountEvidence(token identityToken) recoveryMountEvidence {
	return recoveryMountEvidence{token: token}
}

func unavailableRecoveryMountEvidence(cause error) recoveryMountEvidence {
	if cause == nil {
		cause = errors.New("durable recovery mount identity is unavailable")
	}
	return recoveryMountEvidence{cause: cause}
}

func (evidence recoveryMountEvidence) tokenOrFailure(physicalRoot string) (identityToken, error) {
	if evidence.token != (identityToken{}) && evidence.cause == nil {
		return evidence.token, nil
	}
	kind := FailureRecoveryEvidenceUnavailable
	detail := "capture durable recovery mount identity"
	if errors.Is(evidence.cause, errMountIdentityUnsupported) {
		kind = FailureUnsupportedPlatform
		detail = "durable recovery mount identity is unsupported"
	}
	return identityToken{}, newFailure(kind, physicalRoot, detail, evidence.cause)
}
