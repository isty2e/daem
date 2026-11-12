package metadataconversion

import "slices"

// Disclosure describes the selected cohort without granting access to its paths.
type Disclosure struct {
	ManifestPath string
	DataDir      string
	Statefiles   []string
	Targets      []string
}

func (plan Plan) Disclosure() Disclosure {
	targets := plan.intent.Targets()
	paths := make([]string, len(targets))
	for index, target := range targets {
		paths[index] = target.Path()
	}
	return Disclosure{
		ManifestPath: plan.paths.ManifestPath,
		DataDir:      plan.paths.DataDir,
		Statefiles:   slices.Clone(plan.participants),
		Targets:      paths,
	}
}

func (recovery Recovery) Disclosure() Disclosure {
	return recovery.plan.Disclosure()
}
