package operationplan

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// AdoptScanKind is the closed freshness-evidence grammar accepted by the
// import operation compiler.
type AdoptScanKind string

const (
	// AdoptScanBoundedFile observes one absent or bounded regular file.
	AdoptScanBoundedFile AdoptScanKind = "bounded_file"
	// AdoptScanDirectoryListing observes one immediate directory inventory.
	AdoptScanDirectoryListing AdoptScanKind = "directory_listing"
)

// AdoptSource carries one staged declaration source and its observed live path.
type AdoptSource struct {
	SourcePath string
	LivePath   string
	Target     string
	Scope      string
}

// AdoptSkillRoute carries one target-specific Skill live/read route.
type AdoptSkillRoute struct {
	LivePath string
	ReadPath string
	Target   string
	Scope    string
}

// AdoptPhysicalPath carries one observed target-visible path.
type AdoptPhysicalPath struct {
	Path   string
	Target string
	Scope  string
}

// AdoptMCPSource carries one physical MCP document authority and the alternate
// paths required to remain absent.
type AdoptMCPSource struct {
	PrimaryPath         string
	Target              string
	Scope               string
	RequiredAbsentPaths []string
}

// AdoptScan carries one scan-derived physical freshness observation.
type AdoptScan struct {
	Path         string
	Target       string
	Scope        string
	Kind         AdoptScanKind
	MaximumBytes int64
}

// AdoptInput contains normalized import paths plus owner-compiled barrier
// domains and revision requests. It performs no filesystem observation.
type AdoptInput struct {
	BarrierDomains          []mutation.Domain
	BarrierRevisions        []mutation.RevisionRequest
	OutputPath              string
	OutputMaximumBytes      int64
	SelectorLockfilePath    string
	MetadataTransactionPath string
	Sources                 []AdoptSource
	SkillSourcePaths        []string
	SkillRoutes             []AdoptSkillRoute
	Hooks                   []AdoptPhysicalPath
	MCPSources              []AdoptMCPSource
	Scans                   []AdoptScan
}

type adoptDomainKind uint8

const (
	adoptDomainLogical adoptDomainKind = iota + 1
	adoptDomainPhysical
)

// AdoptDomainRequest is one pure logical-or-physical mutation-domain request.
// Workflow code lowers it through mutation so path canonicalization and
// filesystem authority remain outside operationplan.
type AdoptDomainRequest struct {
	kind     adoptDomainKind
	logical  mutation.LogicalPathRequest
	physical mutation.PhysicalPathRequest
}

// Logical returns the logical request when this value represents one.
func (request AdoptDomainRequest) Logical() (mutation.LogicalPathRequest, bool) {
	if request.kind != adoptDomainLogical {
		return mutation.LogicalPathRequest{}, false
	}
	return request.logical, true
}

// Physical returns the physical request when this value represents one.
func (request AdoptDomainRequest) Physical() (mutation.PhysicalPathRequest, bool) {
	if request.kind != adoptDomainPhysical {
		return mutation.PhysicalPathRequest{}, false
	}
	return request.physical, true
}

type adoptStepAction uint8

const (
	adoptStepNoAction adoptStepAction = iota
	adoptStepRevision
	adoptStepExternalValidation
)

// AdoptStep is one ordered compiler transition. Callers must check Preflight,
// lower Domain when present, then consume the step through AdoptRevisionCompiler.
type AdoptStep struct {
	index          int
	domain         AdoptDomainRequest
	hasDomain      bool
	preflightErr   error
	action         adoptStepAction
	revision       mutation.RevisionRequest
	stable         bool
	authoritative  bool
	externalPath   string
	externalEffect mutation.PathEffect
}

// Preflight returns a pure input error that historically precedes this step's
// domain lowering.
func (step AdoptStep) Preflight() error {
	return step.preflightErr
}

// Domain returns the domain request lowered before this step is consumed.
func (step AdoptStep) Domain() (AdoptDomainRequest, bool) {
	return step.domain, step.hasDomain
}

// AdoptProgram is the immutable ordered import operation-safety program.
type AdoptProgram struct {
	barrierDomains   []mutation.Domain
	barrierRevisions []mutation.RevisionRequest
	steps            []AdoptStep
}

// BarrierDomains returns the owner-compiled State Barrier domain prefix.
func (program AdoptProgram) BarrierDomains() []mutation.Domain {
	return append([]mutation.Domain(nil), program.barrierDomains...)
}

// Steps returns compiler transitions in exact historical admission order.
func (program AdoptProgram) Steps() []AdoptStep {
	return append([]AdoptStep(nil), program.steps...)
}

// AdoptRevisionPlan contains canonical full and publication-stable revision roles.
type AdoptRevisionPlan struct {
	revisions       []mutation.RevisionRequest
	stableRevisions []mutation.RevisionRequest
}

// Revisions returns full revision requests sorted by the historical
// decimal-effect, colon, path key.
func (plan AdoptRevisionPlan) Revisions() []mutation.RevisionRequest {
	return append([]mutation.RevisionRequest(nil), plan.revisions...)
}

// StableRevisions returns publication-stable revision requests in the same
// canonical order.
func (plan AdoptRevisionPlan) StableRevisions() []mutation.RevisionRequest {
	return append([]mutation.RevisionRequest(nil), plan.stableRevisions...)
}

type adoptObservedRevision struct {
	request       mutation.RevisionRequest
	authoritative bool
}

// AdoptRevisionCompiler incrementally consumes one AdoptProgram after each
// domain has been lowered, preserving historical error precedence.
type AdoptRevisionCompiler struct {
	observed            map[string]adoptObservedRevision
	stableObserved      map[string]adoptObservedRevision
	externallyValidated map[string]struct{}
	nextStep            int
	totalSteps          int
	finished            bool
}

// NewRevisionCompiler begins ordered revision-role compilation. Barrier
// revision conflicts are reported before the first import-specific domain.
func (program AdoptProgram) NewRevisionCompiler() (*AdoptRevisionCompiler, error) {
	compiler := &AdoptRevisionCompiler{
		observed:            make(map[string]adoptObservedRevision),
		stableObserved:      make(map[string]adoptObservedRevision),
		externallyValidated: make(map[string]struct{}),
		totalSteps:          len(program.steps),
	}
	for _, request := range program.barrierRevisions {
		if err := compiler.addRevision(request, true, false); err != nil {
			return nil, err
		}
	}
	return compiler, nil
}

// ApplyAfterDomain consumes exactly the next compiler step after its domain, if
// any, has been lowered successfully.
func (compiler *AdoptRevisionCompiler) ApplyAfterDomain(step AdoptStep) error {
	if compiler == nil || compiler.observed == nil || compiler.stableObserved == nil ||
		compiler.externallyValidated == nil {
		return fmt.Errorf("adopt revision compiler is not initialized")
	}
	if compiler.finished {
		return fmt.Errorf("adopt revision compiler is already finished")
	}
	if step.index != compiler.nextStep {
		return fmt.Errorf(
			"adopt revision step index %d is out of order; want %d",
			step.index,
			compiler.nextStep,
		)
	}
	if err := step.Preflight(); err != nil {
		return err
	}
	switch step.action {
	case adoptStepNoAction:
	case adoptStepRevision:
		if err := compiler.addRevision(
			step.revision,
			step.stable,
			step.authoritative,
		); err != nil {
			return err
		}
	case adoptStepExternalValidation:
		compiler.externallyValidated[adoptRevisionKey(
			step.externalPath,
			step.externalEffect,
		)] = struct{}{}
	default:
		return fmt.Errorf("adopt revision step action %d is invalid", step.action)
	}
	compiler.nextStep++
	return nil
}

// Compile finishes revision roles after every ordered step has been consumed.
func (compiler *AdoptRevisionCompiler) Compile() (AdoptRevisionPlan, error) {
	if compiler == nil || compiler.observed == nil || compiler.stableObserved == nil ||
		compiler.externallyValidated == nil {
		return AdoptRevisionPlan{}, fmt.Errorf("adopt revision compiler is not initialized")
	}
	if compiler.finished {
		return AdoptRevisionPlan{}, fmt.Errorf("adopt revision compiler is already finished")
	}
	if compiler.nextStep != compiler.totalSteps {
		return AdoptRevisionPlan{}, fmt.Errorf(
			"adopt revision compiler consumed %d of %d steps",
			compiler.nextStep,
			compiler.totalSteps,
		)
	}
	compiler.finished = true
	for key := range compiler.externallyValidated {
		delete(compiler.observed, key)
		delete(compiler.stableObserved, key)
	}
	return AdoptRevisionPlan{
		revisions:       sortedAdoptRevisions(compiler.observed),
		stableRevisions: sortedAdoptRevisions(compiler.stableObserved),
	}, nil
}

// CompileAdopt compiles normalized import evidence into an immutable ordered
// program without filesystem I/O or authority acquisition.
func CompileAdopt(input AdoptInput) AdoptProgram {
	builder := adoptProgramBuilder{
		physicalDomainKeys: make(map[string]struct{}),
	}

	outputRequests, err := mutation.BoundedFileRevisionRequests(
		input.OutputMaximumBytes,
		input.OutputPath,
	)
	if err != nil {
		builder.addError(err)
		return builder.program(input)
	}
	for _, request := range outputRequests {
		access := mutation.AccessShared
		if request.Effect == mutation.PathEffectDirectoryEntry {
			access = mutation.AccessExclusive
		}
		builder.addLogical(
			input.OutputPath,
			access,
			request.Effect,
			adoptRevisionStep(request, true, false),
		)
	}

	if input.SelectorLockfilePath != "" {
		builder.addLogical(
			input.SelectorLockfilePath,
			mutation.AccessShared,
			mutation.PathEffectDirectoryEntry,
			AdoptStep{},
		)
		builder.addLogical(
			input.SelectorLockfilePath,
			mutation.AccessShared,
			mutation.PathEffectReferent,
			AdoptStep{},
		)
	}

	builder.addLogical(
		input.MetadataTransactionPath,
		mutation.AccessExclusive,
		mutation.PathEffectDirectoryEntry,
		adoptRevisionStep(
			mutation.NewBoundedContentRevisionRequest(
				input.MetadataTransactionPath,
				mutation.PathEffectDirectoryEntry,
			),
			true,
			false,
		),
	)

	for _, source := range input.Sources {
		builder.addLogical(
			source.SourcePath,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					source.SourcePath,
					mutation.PathEffectDirectoryEntry,
				),
				false,
				false,
			),
		)
		builder.addPhysical(
			source.LivePath,
			source.Target,
			source.Scope,
			mutation.PathEffectReferent,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					source.LivePath,
					mutation.PathEffectReferent,
				),
				true,
				false,
			),
		)
	}

	for _, path := range input.SkillSourcePaths {
		builder.addLogical(
			path,
			mutation.AccessExclusive,
			mutation.PathEffectDirectoryEntry,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					path,
					mutation.PathEffectDirectoryEntry,
				),
				false,
				false,
			),
		)
	}

	for _, route := range input.SkillRoutes {
		builder.addPhysical(
			route.LivePath,
			route.Target,
			route.Scope,
			mutation.PathEffectDirectoryEntry,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					route.LivePath,
					mutation.PathEffectDirectoryEntry,
				),
				true,
				false,
			),
		)
		builder.addPhysical(
			route.ReadPath,
			route.Target,
			route.Scope,
			mutation.PathEffectReferent,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					route.ReadPath,
					mutation.PathEffectReferent,
				),
				true,
				false,
			),
		)
	}

	for _, hook := range input.Hooks {
		builder.addPhysical(
			hook.Path,
			hook.Target,
			hook.Scope,
			mutation.PathEffectReferent,
			adoptRevisionStep(
				mutation.NewBoundedContentRevisionRequest(
					hook.Path,
					mutation.PathEffectReferent,
				),
				true,
				false,
			),
		)
	}

	for _, source := range input.MCPSources {
		builder.addPhysical(
			source.PrimaryPath,
			source.Target,
			source.Scope,
			mutation.PathEffectReferent,
			adoptExternalValidationStep(
				source.PrimaryPath,
				mutation.PathEffectReferent,
			),
		)
		for _, path := range source.RequiredAbsentPaths {
			builder.addPhysical(
				path,
				source.Target,
				source.Scope,
				mutation.PathEffectDirectoryEntry,
				adoptRevisionStep(
					mutation.NewRequiredAbsentRevisionRequest(path),
					true,
					true,
				),
			)
		}
	}

	for _, scan := range input.Scans {
		switch scan.Kind {
		case AdoptScanBoundedFile:
			requests, requestErr := mutation.BoundedFileRevisionRequests(
				scan.MaximumBytes,
				scan.Path,
			)
			if requestErr != nil {
				builder.addError(requestErr)
				return builder.program(input)
			}
			for _, request := range requests {
				builder.addPhysical(
					scan.Path,
					scan.Target,
					scan.Scope,
					request.Effect,
					adoptRevisionStep(request, true, true),
				)
			}
		case AdoptScanDirectoryListing:
			builder.addPhysical(
				scan.Path,
				scan.Target,
				scan.Scope,
				mutation.PathEffectReferent,
				adoptRevisionStep(
					mutation.NewBoundedDirectoryListingRevisionRequest(scan.Path),
					true,
					true,
				),
			)
		default:
			builder.addError(fmt.Errorf(
				"import scan %q has unsupported evidence kind %q",
				scan.Path,
				scan.Kind,
			))
			return builder.program(input)
		}
	}

	return builder.program(input)
}

type adoptProgramBuilder struct {
	steps              []AdoptStep
	physicalDomainKeys map[string]struct{}
}

func (builder *adoptProgramBuilder) program(input AdoptInput) AdoptProgram {
	return AdoptProgram{
		barrierDomains:   append([]mutation.Domain(nil), input.BarrierDomains...),
		barrierRevisions: append([]mutation.RevisionRequest(nil), input.BarrierRevisions...),
		steps:            append([]AdoptStep(nil), builder.steps...),
	}
}

func (builder *adoptProgramBuilder) addLogical(
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
	step AdoptStep,
) {
	step.domain = AdoptDomainRequest{
		kind: adoptDomainLogical,
		logical: mutation.LogicalPathRequest{
			Path: path, Access: access, Effect: effect,
		},
	}
	step.hasDomain = true
	builder.addStep(step)
}

func (builder *adoptProgramBuilder) addPhysical(
	path string,
	target string,
	scope string,
	effect mutation.PathEffect,
	step AdoptStep,
) {
	key := target + "\x00" + scope + "\x00" + adoptRevisionKey(path, effect)
	if _, exists := builder.physicalDomainKeys[key]; !exists {
		builder.physicalDomainKeys[key] = struct{}{}
		step.domain = AdoptDomainRequest{
			kind: adoptDomainPhysical,
			physical: mutation.PhysicalPathRequest{
				Path: path, Access: mutation.AccessShared, Effect: effect,
				Target: target, Scope: scope,
			},
		}
		step.hasDomain = true
	}
	builder.addStep(step)
}

func (builder *adoptProgramBuilder) addError(err error) {
	builder.addStep(AdoptStep{preflightErr: err})
}

func (builder *adoptProgramBuilder) addStep(step AdoptStep) {
	step.index = len(builder.steps)
	builder.steps = append(builder.steps, step)
}

func adoptRevisionStep(
	request mutation.RevisionRequest,
	stable bool,
	authoritative bool,
) AdoptStep {
	return AdoptStep{
		action:        adoptStepRevision,
		revision:      request,
		stable:        stable,
		authoritative: authoritative,
	}
}

func adoptExternalValidationStep(path string, effect mutation.PathEffect) AdoptStep {
	return AdoptStep{
		action:         adoptStepExternalValidation,
		externalPath:   path,
		externalEffect: effect,
	}
}

func (compiler *AdoptRevisionCompiler) addRevision(
	request mutation.RevisionRequest,
	stable bool,
	authoritative bool,
) error {
	key := adoptRevisionKey(request.Path, request.Effect)
	if err := addAdoptObservedRevision(
		compiler.observed,
		key,
		request,
		authoritative,
	); err != nil {
		return err
	}
	if stable {
		if err := addAdoptObservedRevision(
			compiler.stableObserved,
			key,
			request,
			authoritative,
		); err != nil {
			return err
		}
	}
	return nil
}

func addAdoptObservedRevision(
	destination map[string]adoptObservedRevision,
	key string,
	request mutation.RevisionRequest,
	authoritative bool,
) error {
	if existing, exists := destination[key]; exists {
		switch {
		case existing.request.Equal(request):
			return nil
		case existing.authoritative && !authoritative:
			return nil
		case !existing.authoritative && authoritative:
			destination[key] = adoptObservedRevision{
				request:       request,
				authoritative: true,
			}
			return nil
		default:
			return fmt.Errorf(
				"import path %q carries conflicting revision semantics",
				request.Path,
			)
		}
	}
	destination[key] = adoptObservedRevision{
		request:       request,
		authoritative: authoritative,
	}
	return nil
}

func sortedAdoptRevisions(
	observed map[string]adoptObservedRevision,
) []mutation.RevisionRequest {
	keys := make([]string, 0, len(observed))
	for key := range observed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	requests := make([]mutation.RevisionRequest, 0, len(keys))
	for _, key := range keys {
		requests = append(requests, observed[key].request)
	}
	return requests
}

func adoptRevisionKey(path string, effect mutation.PathEffect) string {
	return strconv.Itoa(int(effect)) + ":" + path
}
