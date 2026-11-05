package qualification

type SelectedPhaseError struct{ cause error }

func (failure *SelectedPhaseError) Error() string {
	return "native selected document does not admit recovery"
}

func (failure *SelectedPhaseError) Unwrap() error { return failure.cause }
