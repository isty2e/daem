package extension

import desiredextension "github.com/isty2e/daem/internal/desired/extension"

// IsPiGitCommitPinChangeTo admits a ref-only change, not a repository alias or
// target/scope change, even when native Pi would reuse the same checkout.
func (carrier Carrier) IsPiGitCommitPinChangeTo(next Carrier) bool {
	return carrier.Family() == desiredextension.CarrierPiPackage &&
		next.Family() == desiredextension.CarrierPiPackage &&
		carrier.key.Target() == next.key.Target() && carrier.key.Scope() == next.key.Scope() &&
		carrier.Source().Kind() == next.Source().Kind() &&
		desiredextension.GitCommitPinChange(carrier.Source().Ref(), next.Source().Ref())
}

// PiGitIdentity is a ref-independent conflict key, not exact relation authority.
func (carrier Carrier) PiGitIdentity() (string, bool) {
	if carrier.Family() != desiredextension.CarrierPiPackage {
		return "", false
	}
	source, err := InterpretCarrierSource(carrier.key)
	if err != nil || source.Class() != CarrierSourceGit {
		return "", false
	}
	return source.Identity(), true
}
