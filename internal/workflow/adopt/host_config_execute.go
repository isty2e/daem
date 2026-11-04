package adopt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"

	declarationtransaction "github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

type HostConfigCaptureResult struct {
	SourcePath   string
	ManifestPath string
	Retired      bool
	Committed    bool
}

// ExecuteHostConfigCapture consumes a caller-approved named-file edit. The UI
// must disclose its referent, known references and unenumerated outside readers.
func ExecuteHostConfigCapture(ctx context.Context, plan *HostConfigCapturePlan, approvedFile string) (result HostConfigCaptureResult, resultErr error) {
	if ctx == nil || plan == nil || plan.closed {
		return result, fmt.Errorf("capture execution requires a live plan and context")
	}
	defer func() { resultErr = errors.Join(resultErr, plan.Close()) }()
	if approvedFile != plan.ApprovalPath() {
		return result, ErrHostConfigCaptureApproval
	}
	domains, err := plan.captureDomains()
	if err != nil {
		return result, err
	}
	store, err := mutation.NewStore(plan.input.Paths.DataDir)
	if err != nil {
		return result, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, leases.Release()) }()
	if err := plan.qualifyCapturePaths(ctx); err != nil {
		return result, err
	}
	current, err := buildHostConfigCapturePlan(ctx, plan.input, plan.initial)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, current.Close()) }()
	if !sameHostConfigCapture(plan, current) {
		return result, mutation.StalePlanError{}
	}
	oldFacts, _ := plan.observation.NativeInput()
	newFacts, _ := current.observation.NativeInput()
	scope, err := oldFacts.Documents.Evidence().RegistryScope()
	if err != nil {
		return result, err
	}
	if same, err := scope.Matches(oldFacts.Registry, newFacts.Registry); err != nil {
		return result, err
	} else if !same {
		return result, mutation.StalePlanError{}
	}
	for _, namespace := range oldFacts.Documents.Evidence().Namespaces() {
		if _, _, err := namespace.WriterBinding(ctx, oldFacts.Budget); err != nil {
			return result, err
		}
	}
	if same, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return result, err
	} else if !same {
		return result, mutation.StalePlanError{}
	}
	if err := current.qualifyCapturePaths(ctx); err != nil {
		return result, err
	}
	if current.retire {
		err = declarationtransaction.CommitSourceRetirement(ctx, declarationtransaction.SourceRetirementInput{
			ManifestTarget: current.manifest, StateDir: current.input.Paths.StateDir, ManifestBytes: current.manifestAfter,
		})
	} else {
		err = declarationtransaction.CommitSourceTransaction(ctx, declarationtransaction.SourceTransactionInput{
			ManifestTarget: current.manifest, SourceTarget: current.source, StateDir: current.input.Paths.StateDir,
			ManifestBytes: current.manifestAfter, SourceBytes: current.sourceAfter,
		})
	}
	if err != nil {
		return result, err
	}
	return HostConfigCaptureResult{SourcePath: current.SourcePath(), ManifestPath: current.input.Paths.ManifestPath, Retired: current.retire, Committed: true}, nil
}

func sameHostConfigCapture(left, right *HostConfigCapturePlan) bool {
	if left.source.Path() != right.source.Path() || left.originalSource.Path() != right.originalSource.Path() ||
		left.retire != right.retire || left.narrowing != right.narrowing || !left.native.Equal(right.native) ||
		!bytes.Equal(left.manifestBefore, right.manifestBefore) || !bytes.Equal(left.manifestAfter, right.manifestAfter) ||
		!bytes.Equal(left.sourceBefore, right.sourceBefore) || !bytes.Equal(left.sourceAfter, right.sourceAfter) ||
		!bytes.Equal(left.desiredBefore, right.desiredBefore) ||
		!slices.Equal(left.selected, right.selected) || !slices.Equal(left.references, right.references) {
		return false
	}
	leftFacts, _ := left.observation.NativeInput()
	rightFacts, _ := right.observation.NativeInput()
	leftNamespaces, rightNamespaces := leftFacts.Documents.Evidence().Namespaces(), rightFacts.Documents.Evidence().Namespaces()
	return len(leftNamespaces) == 1 && len(rightNamespaces) == 1 && leftNamespaces[0].Observation().Equal(rightNamespaces[0].Observation())
}

func (plan *HostConfigCapturePlan) captureDomains() ([]mutation.Domain, error) {
	marker, err := fileset.FileSetAuthorityPath(plan.input.Paths.StateDir)
	if err != nil {
		return nil, err
	}
	paths := []string{plan.input.Paths.ManifestPath, plan.manifest.Path()}
	if !plan.retire {
		paths = append(paths, plan.SourcePath(), plan.source.Path())
	}
	originalPath, err := localCaptureSourcePath(plan.input.Paths.ManifestRoot, plan.config.Source())
	if err != nil {
		return nil, err
	}
	paths = append(paths, originalPath, plan.originalSource.Path())
	for _, reference := range plan.references {
		paths = append(paths, reference.Path)
	}
	steps := operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{
		TargetPaths: paths, MarkerPath: marker, TrailingDomains: plan.barrier.Domains(),
	})
	var domains []mutation.Domain
	for _, step := range steps {
		if domain, compiled := step.Compiled(); compiled {
			domains = append(domains, domain)
			continue
		}
		request, present := step.Path()
		if !present {
			return nil, fmt.Errorf("capture metadata domain is invalid")
		}
		logical, present := request.Logical()
		if !present {
			return nil, fmt.Errorf("capture metadata domain is not logical")
		}
		domain, err := mutation.NewLogicalPathDomain(logical)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	facts, _ := plan.observation.NativeInput()
	for _, namespace := range facts.Documents.Evidence().Namespaces() {
		path := ""
		if exact, present := namespace.Observation().Exact(); present {
			path = exact.Key()
		} else {
			missing, _ := namespace.Observation().Provisional()
			path = missing.CandidateKey()
		}
		alias := namespace.Addresses()[0]
		for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
			domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{Path: path, Access: mutation.AccessExclusive, Effect: effect, Target: string(alias.Target()), Scope: string(alias.Scope())})
			if err != nil {
				return nil, err
			}
			domains = append(domains, domain)
		}
	}
	return domains, nil
}
