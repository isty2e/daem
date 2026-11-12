package migrate

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sync"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

type MetadataInput struct {
	ManifestPath string
	Recover      bool
	References   []string
}

type MetadataDisclosure struct {
	metadataconversion.Disclosure
	Action              string
	ReferenceAssertions []MetadataReferenceAssertion
}

type MetadataReferenceAssertion struct {
	InputPath    string
	IdentityKey  string
	Witness      string
	ObservedPath string
}

type metadataExecution func(context.Context) error

type PreparedMetadata struct {
	mu         sync.Mutex
	execution  metadataExecution
	disclosure MetadataDisclosure
}

func PlanMetadata(ctx context.Context, input MetadataInput) (*PreparedMetadata, error) {
	if ctx == nil {
		return nil, fmt.Errorf("metadata migration context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil {
		return nil, err
	}
	budget, err := physicalwork.NewBudget(0)
	if err != nil {
		return nil, err
	}
	if input.Recover {
		if len(input.References) != 0 {
			return nil, fmt.Errorf("metadata recovery uses its recorded references; provide lookup candidates when planning a new conversion")
		}
		clearErr := recoverygate.RequireGlobalMetadataClear(ctx, paths)
		if clearErr == nil {
			return &PreparedMetadata{
				execution: func(ctx context.Context) error { return ctx.Err() },
				disclosure: MetadataDisclosure{
					Disclosure: metadataconversion.Disclosure{ManifestPath: paths.ManifestPath, DataDir: paths.DataDir},
					Action:     "none",
				},
			}, nil
		}
		if !recoverygate.IsGlobalMetadataRefusal(clearErr) {
			return nil, clearErr
		}
		recovery, err := metadataconversion.InspectRecovery(ctx, paths, budget)
		if err != nil {
			return nil, err
		}
		return &PreparedMetadata{
			execution:  recovery.Execute,
			disclosure: MetadataDisclosure{Disclosure: recovery.Disclosure(), Action: string(recovery.Action())},
		}, nil
	}

	authority, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return nil, err
	}
	references := make([]pathauthority.Reference, 0, len(input.References))
	assertions := make([]MetadataReferenceAssertion, 0, len(input.References))
	for _, path := range input.References {
		if path == "" {
			return nil, fmt.Errorf("metadata lookup candidate path is required")
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		observed, err := mutation.ObservePersistedDirectoryEntryAuthorityBounded(absolute, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, fmt.Errorf("observe metadata lookup candidate %q: %w", path, err)
		}
		references = append(references, observed.Reference())
		assertions = append(assertions, MetadataReferenceAssertion{
			InputPath: path, IdentityKey: observed.Exact().Key(),
			Witness: observed.Exact().Witness(), ObservedPath: observed.Reference().Locator(),
		})
	}
	plan, err := metadataconversion.Prepare(ctx, authority, budget, references)
	if err != nil {
		return nil, err
	}
	action := "convert"
	if !plan.Changed() {
		action = "already_converted"
	}
	return &PreparedMetadata{
		execution: plan.Execute,
		disclosure: MetadataDisclosure{
			Disclosure: plan.Disclosure(), Action: action, ReferenceAssertions: assertions,
		},
	}, nil
}

func (prepared *PreparedMetadata) Disclosure() MetadataDisclosure {
	disclosure := prepared.disclosure
	disclosure.Statefiles = slices.Clone(disclosure.Statefiles)
	disclosure.Targets = slices.Clone(disclosure.Targets)
	disclosure.ReferenceAssertions = slices.Clone(disclosure.ReferenceAssertions)
	return disclosure
}

func (prepared *PreparedMetadata) Close() {
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	prepared.execution = nil
}

func (prepared *PreparedMetadata) Execute(ctx context.Context) (MetadataDisclosure, error) {
	if ctx == nil {
		return MetadataDisclosure{}, fmt.Errorf("metadata migration context is required")
	}
	prepared.mu.Lock()
	execution := prepared.execution
	prepared.execution = nil
	prepared.mu.Unlock()
	if execution == nil {
		return MetadataDisclosure{}, fmt.Errorf("metadata migration preparation is not available")
	}
	if err := execution(ctx); err != nil {
		return MetadataDisclosure{}, err
	}
	disclosure := prepared.Disclosure()
	if disclosure.Action == "convert" {
		disclosure.Action = "converted"
	}
	return disclosure, nil
}
