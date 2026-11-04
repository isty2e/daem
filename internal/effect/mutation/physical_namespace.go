package mutation

// Namespaces returns the qualified path comparisons retained by physical
// authority. It grants no capability beyond the containing authority set.
func (set PhysicalAuthoritySet) Namespaces() ([]DirectoryEntryAuthorityObservation, error) {
	result := make([]DirectoryEntryAuthorityObservation, 0, len(set.domains))
	for _, domain := range set.domains {
		observation, err := directoryEntryAuthorityObservation(canonicalPath{
			keyPath: domain.canonicalPath, witness: domain.pathWitness, provisional: domain.provisional,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, observation)
	}
	return result, nil
}
