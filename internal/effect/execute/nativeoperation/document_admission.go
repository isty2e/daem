package nativeoperation

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// DocumentAdmissionError names the logical outputs involved in a refusal.
type DocumentAdmissionError struct {
	writer documentMember
	peer   documentMember
	cause  error
}

func (failure *DocumentAdmissionError) Error() string {
	if errors.Is(failure.cause, mutation.ErrRemovalSpellingMismatch) {
		return fmt.Sprintf("cannot remove configuration file %q: its path spelling cannot be matched after deletion", failure.writer.route.path)
	}
	if errors.Is(failure.cause, journalnamespace.ErrRemovalProtection) {
		return fmt.Sprintf("cannot remove configuration file %q: its path cannot be shown to stay outside reserved data after deletion", failure.writer.route.path)
	}
	if failure.cause != nil {
		if failure.writer.location() == failure.peer.location() {
			return fmt.Sprintf("cannot verify the selected output's route at %q", failure.writer.route.path)
		}
		return fmt.Sprintf("cannot check whether managed output %q (%s) overlaps %q", failure.peer.route.path, failure.peer.description(), failure.writer.route.path)
	}
	return fmt.Sprintf("changes at %q overlap managed output %q (%s)", failure.writer.route.path, failure.peer.route.path, failure.peer.description())
}

func (failure *DocumentAdmissionError) Unwrap() error { return failure.cause }

func (failure *DocumentAdmissionError) UnsafeRemoval() bool {
	return errors.Is(failure.cause, mutation.ErrRemovalSpellingMismatch) || errors.Is(failure.cause, journalnamespace.ErrRemovalProtection)
}

func (failure *DocumentAdmissionError) Unverified() bool {
	return failure.cause != nil && !failure.UnsafeRemoval()
}

func (failure *DocumentAdmissionError) RemovedAgentConfig() (string, bool) {
	if !failure.writer.removing {
		return "", false
	}
	if id, _, key := topologyprojection.AgentConfigKeyIdentity(failure.writer.subject); key {
		return id.Name(), true
	}
	if aggregate.IsAgentConfigDocumentSubject(failure.writer.subject) {
		if id, ok := topologyprojection.EntityID(failure.writer.subject); ok {
			return id.Name(), true
		}
	}
	return "", false
}

type documentRoute struct {
	path   output.Destination
	alias  aggregate.DocumentAddress
	native bool
}

func aggregateRoute(alias aggregate.DocumentAddress) documentRoute {
	return documentRoute{path: alias.AggregateRoot(), alias: alias}
}

func nativeAggregateRoute(alias aggregate.DocumentAddress) documentRoute {
	return documentRoute{path: alias.AggregateRoot(), alias: alias, native: true}
}

func projectionRoute(contract aggregate.ProjectionContract) documentRoute {
	address := contract.Address().Document()
	if _, native := aggregate.AgentConfigSyntaxForCodec(contract.CodecContractID()); native {
		return nativeAggregateRoute(address)
	}
	return aggregateRoute(address)
}

func managedRoute(subject topology.SubjectID, path output.Destination, scope target.Scope, consumers []target.Target) (documentRoute, error) {
	if !aggregate.IsAgentConfigDocumentSubject(subject) {
		return documentRoute{path: path}, nil
	}
	if len(consumers) != 1 {
		return documentRoute{}, fmt.Errorf("whole configuration membership requires one consumer")
	}
	alias, err := aggregate.NewDocumentAddress(consumers[0], scope, path)
	return nativeAggregateRoute(alias), err
}

type documentMember struct {
	route     documentRoute
	subject   topology.SubjectID
	owners    []target.Target
	scope     target.Scope
	footprint output.Footprint
	directory bool
	removing  bool
	retained  bool
	contract  aggregate.ProjectionContract
}

func (member documentMember) description() string {
	return fmt.Sprintf("%s, %s %v", member.subject.Key(), member.scope, member.owners)
}

type documentIntent struct {
	member   documentMember
	consumes map[topology.SubjectID]struct{}
	write    bool
}

type documentMembership struct {
	before map[topology.SubjectID]documentMember
	after  map[topology.SubjectID]documentMember
	rows   []documentMember
}

type documentLocation struct {
	route documentRoute
	scope target.Scope
}

func (member documentMember) location() documentLocation {
	return documentLocation{member.route, member.scope}
}

type documentMembersAtRoute struct {
	location  documentLocation
	whole     []documentMember
	keys      []documentMember
	directory bool
	retained  bool
}

func groupDocumentMembers(rows []documentMember) []documentMembersAtRoute {
	indices := make(map[documentLocation]int)
	var groups []documentMembersAtRoute
	for _, row := range rows {
		location := row.location()
		index, exists := indices[location]
		if !exists {
			index = len(groups)
			indices[location] = index
			groups = append(groups, documentMembersAtRoute{location: location})
		}
		group := &groups[index]
		group.retained = group.retained || row.retained
		if row.footprint.Kind() == output.FootprintPath {
			group.whole = append(group.whole, row)
			group.directory = group.directory || row.directory
		} else {
			group.keys = append(group.keys, row)
		}
	}
	for i := range groups {
		slices.SortStableFunc(groups[i].keys, func(left, right documentMember) int {
			return cmp.Compare(left.keyPointer(), right.keyPointer())
		})
	}
	return groups
}

func (member documentMember) keyPointer() string {
	k, _ := member.footprint.Key()
	return k.String()
}

func (group documentMembersAtRoute) representative() documentMember {
	if len(group.whole) != 0 {
		return group.whole[0]
	}
	return group.keys[0]
}

func (group documentMembersAtRoute) conflict(intent documentIntent) (documentMember, bool) {
	conflicting := func(member documentMember) bool { return !preservesMember(intent, member) }
	if index := slices.IndexFunc(group.whole, conflicting); index >= 0 {
		return group.whole[index], true
	}
	if intent.member.footprint.Kind() == output.FootprintPath {
		if index := slices.IndexFunc(group.keys, conflicting); index >= 0 {
			return group.keys[index], true
		}
		return documentMember{}, false
	}
	start := intent.member.keyPointer()
	first := func(pointer string) int {
		index, _ := slices.BinarySearchFunc(group.keys, pointer, func(member documentMember, pointer string) int {
			return cmp.Compare(member.keyPointer(), pointer)
		})
		return index
	}
	for pointer := start; pointer != ""; pointer = pointer[:strings.LastIndexByte(pointer, '/')] {
		for index := first(pointer); index < len(group.keys) && group.keys[index].keyPointer() == pointer; index++ {
			if conflicting(group.keys[index]) {
				return group.keys[index], true
			}
		}
	}
	prefix := start + "/"
	for index := first(prefix); index < len(group.keys) && strings.HasPrefix(group.keys[index].keyPointer(), prefix); index++ {
		if conflicting(group.keys[index]) {
			return group.keys[index], true
		}
	}
	return documentMember{}, false
}

func indexDocumentMembership(before, after durable.Snapshot) (documentMembership, error) {
	index := documentMembership{}
	indexSnapshot := func(snapshot durable.Snapshot) (map[topology.SubjectID]documentMember, error) {
		members := make(map[topology.SubjectID]documentMember)
		for _, row := range snapshot.ManagedPaths() {
			route, err := managedRoute(row.Subject(), row.Destination(), row.Scope(), row.ConsumerTargets())
			if err != nil {
				return nil, err
			}
			members[row.Subject()] = documentMember{route: route, subject: row.Subject(), owners: row.ConsumerTargets(), scope: row.Scope(), footprint: output.WholePathFootprint(), directory: row.ContentKind() == realization.PathProjectionDirectory}
		}
		for _, row := range snapshot.ManagedAggregates() {
			contract := row.Contribution().Contract()
			address := contract.Address().Document()
			members[row.Subject()] = documentMember{route: projectionRoute(contract), subject: row.Subject(), owners: []target.Target{address.Target()}, scope: address.Scope(), footprint: contract.Address().Footprint(), contract: contract}
		}
		return members, nil
	}
	var err error
	if index.before, err = indexSnapshot(before); err != nil {
		return documentMembership{}, err
	}
	if index.after, err = indexSnapshot(after); err != nil {
		return documentMembership{}, err
	}
	for _, member := range index.before {
		member.retained = member.sameClaim(index.after[member.subject])
		index.rows = append(index.rows, member)
	}
	for _, member := range index.after {
		if !member.sameClaim(index.before[member.subject]) {
			member.retained = true
			index.rows = append(index.rows, member)
		}
	}
	slices.SortFunc(index.rows, compareDocumentMembers)
	return index, nil
}

func compareDocumentMembers(left, right documentMember) int {
	if left.route.path != right.route.path {
		return cmp.Compare(left.route.path.String(), right.route.path.String())
	}
	return topology.CompareSubjectID(left.subject, right.subject)
}

func (member documentMember) sameClaim(other documentMember) bool {
	if member.subject != other.subject || member.route != other.route || member.scope != other.scope || member.footprint != other.footprint || member.directory != other.directory {
		return false
	}
	return member.footprint.Kind() == output.FootprintPath || member.contract.Equal(other.contract)
}

func documentIntents(preparation effectpreparation.NativeApplyStatePreparation, whole wholeDocumentWork, members documentMembership) ([]documentIntent, error) {
	var intents []documentIntent
	for _, action := range preparation.Narrowings() {
		plan, selected := action.KeyPlan()
		if !selected {
			continue
		}
		consumes := map[topology.SubjectID]struct{}{action.Baseline().Subject(): {}}
		for _, intent := range plan.Intents() {
			before := intent.Before()
			desired, _ := intent.Desired()
			for _, contribution := range desired.Contributions() {
				member := members.after[contribution.SubjectID()]
				if !before.Present() || before.CanonicalProjection() != contribution.Contribution().CanonicalContribution() {
					intents = append(intents, documentIntent{member: member, consumes: consumes, write: true})
				}
			}
		}
	}
	for _, member := range members.rows {
		if after, remains := members.after[member.subject]; remains && member.sameClaim(after) && !member.sameClaim(members.before[member.subject]) {
			intents = append(intents, documentIntent{member: member})
		}
	}
	ordinary, err := effectpreparation.ManagedPathJournalMutations(whole.ordinary)
	if err != nil {
		return nil, err
	}
	for _, mutation := range slices.Concat(ordinary, whole.mutations) {
		if mutation.Kind() == journalrecord.CaptureRecord {
			continue
		}
		member := members.after[mutation.Subject()]
		if mutation.Kind() == journalrecord.CaptureRemove {
			member = members.before[mutation.Subject()]
			member.removing = true
		}
		intents = append(intents, documentIntent{member: member, write: true})
	}
	for _, effect := range preparation.AggregateEffects() {
		if effect.Kind() == effectpreparation.AggregateEffectRecord {
			continue
		}
		removesDocument := effect.BeforeDocument().Exists() && !effect.Rendered().Document().Exists()
		route := aggregateRoute(effect.DocumentAddress())
		if _, native := aggregate.AgentConfigSyntaxForCodec(effect.CodecContractID()); native {
			route.native = true
		}
		var removal documentIntent
		if removesDocument {
			removal = documentIntent{member: documentMember{route: route, owners: []target.Target{effect.Target()}, scope: effect.Scope(), footprint: output.WholePathFootprint(), removing: true}, consumes: make(map[topology.SubjectID]struct{}), write: true}
		}
		for _, projection := range effect.ProjectionEffects() {
			if !projection.IsJournaled() || !projection.MutatesHost() {
				continue
			}
			if removesDocument {
				for _, subject := range projection.Subjects() {
					if subject.Kind() == effectpreparation.AggregateEffectRemove {
						if removal.member.subject == (topology.SubjectID{}) {
							removal.member.subject = subject.Subject()
						}
						removal.consumes[subject.Subject()] = struct{}{}
					}
				}
				continue
			}
			contract := projection.Contract()
			consumes := make(map[topology.SubjectID]struct{})
			if contract.Cardinality() == aggregate.ContributionSharedSet {
				for _, previous := range projection.PreviousStates() {
					consumes[previous.Subject()] = struct{}{}
				}
			}
			for _, subject := range projection.Subjects() {
				consumes[subject.Subject()] = struct{}{}
			}
			member := documentMember{route: route, owners: []target.Target{effect.Target()}, scope: effect.Scope(), footprint: contract.Address().Footprint(), contract: contract, removing: projection.Kind() == effectpreparation.AggregateEffectRemove}
			if subjects := projection.Subjects(); len(subjects) == 1 {
				member.subject = subjects[0].Subject()
			}
			intents = append(intents, documentIntent{member: member, consumes: consumes, write: true})
		}
		if removesDocument {
			intents = append(intents, removal)
		}
	}
	for _, edit := range preparation.SequenceEdits() {
		address := edit.DocumentAddress()
		footprint, err := output.NewKeyFootprint(edit.SelectedSequence().Projection().ArrayPath())
		if err != nil {
			return nil, err
		}
		member := documentMember{route: nativeAggregateRoute(address), owners: []target.Target{address.Target()}, scope: address.Scope(), footprint: footprint}
		intents = append(intents, documentIntent{member: member, write: true})
	}
	return intents, nil
}

// ForwardDocumentAdmission keeps only the route comparisons on which forward
// work depends. It supplies no writer authority and is not recorded in journals.
type ForwardDocumentAdmission struct {
	groups       map[output.Destination][]documentRouteCheck
	resolver     mutationnamespace.Resolver
	manifestRoot string
	readPaths    []string
}

// ReadDomains holds stable comparison-only routes without adding a content
// revision or claiming target-wide host authority.
func (admission ForwardDocumentAdmission) ReadDomains(ctx context.Context, budget *physicalwork.Budget) ([]mutation.Domain, error) {
	var domains []mutation.Domain
	for _, path := range admission.readPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// The lexical spelling can include parent traversal through a
		// configured root link. Only its resolved path is a lock candidate.
		if path != filepath.Clean(path) {
			continue
		}
		domain, stable, err := mutation.NewStableRouteReadDomain(path, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return nil, err
		}
		if stable {
			domains = append(domains, domain)
		}
	}
	return domains, ctx.Err()
}

type documentRouteCheck struct {
	writer     documentMember
	peer       documentMember
	peerBefore mutation.DirectoryEntryAuthorityObservation
}

type documentRouteFact struct {
	observed    mutation.DirectoryEntryAuthorityObservation
	locator     string
	referent    string
	unreachable bool
	cause       error
}

type trackedDocumentRouteBudget struct {
	*physicalwork.Budget
	limit error
}

func (budget *trackedDocumentRouteBudget) AdmitPathComponents(count int) error {
	err := budget.Budget.AdmitPathComponents(count)
	if err != nil {
		budget.limit = err
	}
	return err
}

func (budget *trackedDocumentRouteBudget) AdmitPhysicalWork(count, entries int, bytes int64) error {
	err := budget.Budget.AdmitPhysicalWork(count, entries, bytes)
	if err != nil {
		budget.limit = err
	}
	return err
}

type documentRouteObservation struct {
	budget        *trackedDocumentRouteBudget
	resolver      mutationnamespace.Resolver
	observe       func(string) (string, mutation.DirectoryEntryAuthorityObservation, error)
	facts         map[documentRoute]documentRouteFact
	manifestRoot  string
	locations     nativehostpath.Locations
	locationsErr  error
	locationsRead bool
}

func newDocumentRouteObservation(manifestRoot string, resolver mutationnamespace.Resolver, budget *physicalwork.Budget) *documentRouteObservation {
	shared := &trackedDocumentRouteBudget{Budget: budget}
	return &documentRouteObservation{manifestRoot: manifestRoot, budget: shared, resolver: resolver, observe: mutation.NewFutureReferentAuthorityObserverBounded(physicalwork.MaximumPhysicalPathDepth, shared), facts: make(map[documentRoute]documentRouteFact)}
}

func (observation *documentRouteObservation) fact(route documentRoute) documentRouteFact {
	if fact, ok := observation.facts[route]; ok {
		return fact
	}
	var fact documentRouteFact
	resolved, err := observation.resolve(route)
	if err == nil {
		fact.locator = resolved
		fact.referent, fact.observed, err = observation.observe(resolved)
	}
	if journalnamespace.ProvesUnreachableReferent(err) {
		fact.unreachable = true
	} else {
		fact.cause = err
	}
	observation.facts[route] = fact
	return fact
}

func (observation *documentRouteObservation) resolve(route documentRoute) (string, error) {
	if !route.native {
		return observation.resolver(route.path, observation.budget)
	}
	alias := route.alias
	binding, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: alias.Target(), Scope: alias.Scope(), Destination: alias.AggregateRoot()})
	if err != nil {
		return "", err
	}
	child, directory := binding.Coordinate().DirectoryChild()
	if !directory {
		return "", fmt.Errorf("native configuration comparison requires a directory child")
	}
	manifest, err := configuration.ProjectRootReference()
	if err != nil {
		return "", err
	}
	if binding.RootReference() == manifest {
		return filepath.Join(observation.manifestRoot, filepath.FromSlash(child)), nil
	}
	if !observation.locationsRead {
		observation.locations, observation.locationsErr = nativehostpath.CaptureLocations(observation.manifestRoot)
		observation.locationsRead = true
	}
	if observation.locationsErr != nil {
		return "", observation.locationsErr
	}
	location, err := observation.locations.Resolve(binding.RootReference())
	if err != nil {
		return "", err
	}
	return location.Path() + string(filepath.Separator) + filepath.FromSlash(child), nil
}

func (observation *documentRouteObservation) terminal(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return observation.budget.limit
}

// ObserveForwardDocumentAdmission checks complete prepared work. Unchanged or
// unselected state rows remain members, but do not become actors or writers.
func ObserveForwardDocumentAdmission(ctx context.Context, manifestRoot string, preparation effectpreparation.NativeApplyStatePreparation, evidence []journalobservation.ApprovalEvidence, resolver mutationnamespace.Resolver, budget *physicalwork.Budget) (ForwardDocumentAdmission, error) {
	return ObserveForwardDocumentPhases(ctx, manifestRoot, evidence, resolver, budget, preparation)
}

func ObserveForwardDocumentPhases(ctx context.Context, manifestRoot string, evidence []journalobservation.ApprovalEvidence, resolver mutationnamespace.Resolver, budget *physicalwork.Budget, phases ...effectpreparation.NativeApplyStatePreparation) (ForwardDocumentAdmission, error) {
	if ctx == nil || resolver == nil || budget == nil {
		return ForwardDocumentAdmission{}, fmt.Errorf("forward document admission requires context, resolver and allocation")
	}
	aliases := make([][]aggregate.DocumentAddress, 0, len(evidence))
	for _, group := range evidence {
		aliases = append(aliases, group.Aliases)
	}
	observation := newDocumentRouteObservation(manifestRoot, resolver, budget)
	result := ForwardDocumentAdmission{resolver: resolver, manifestRoot: manifestRoot, groups: make(map[output.Destination][]documentRouteCheck)}
	readPaths := make(map[string]struct{})
	for _, preparation := range phases {
		if err := preparation.Validate(); err != nil {
			return ForwardDocumentAdmission{}, err
		}
		whole, err := wholeDocumentWorkFor(preparation, aliases)
		if err != nil {
			return ForwardDocumentAdmission{}, err
		}
		if err := observeForwardDocumentAdmission(ctx, preparation, whole, evidence, observation, &result, readPaths); err != nil {
			return ForwardDocumentAdmission{}, err
		}
	}
	for path := range readPaths {
		result.readPaths = append(result.readPaths, path)
	}
	slices.Sort(result.readPaths)
	return result, nil
}

func observeForwardDocumentAdmission(ctx context.Context, preparation effectpreparation.NativeApplyStatePreparation, whole wholeDocumentWork, evidence []journalobservation.ApprovalEvidence, observation *documentRouteObservation, admission *ForwardDocumentAdmission, readPaths map[string]struct{}) error {
	if preparation.ActionCount() == 0 {
		return ctx.Err()
	}
	members, err := indexDocumentMembership(preparation.BeforeState(), preparation.AfterState())
	if err != nil {
		return err
	}
	intents, err := documentIntents(preparation, whole, members)
	if err != nil || len(intents) == 0 {
		return err
	}
	groups := groupDocumentMembers(members.rows)
	var writerLocations []documentLocation
	writers := make(map[documentLocation][]documentIntent)
	for _, intent := range intents {
		location := intent.member.location()
		if _, exists := writers[location]; !exists {
			writerLocations = append(writerLocations, location)
		}
		writers[location] = append(writers[location], intent)
	}
	var refusal error
	for _, location := range writerLocations {
		if err := observation.terminal(ctx); err != nil {
			return err
		}
		selected := writers[location]
		writer := selected[0].member
		writerGroup, grouped := whole.indexByAlias[writer.route.alias]
		var writerFact documentRouteFact
		if grouped {
			writerFact.observed = evidence[writerGroup].Authority
		} else {
			writerFact = observation.fact(writer.route)
		}
		if err := observation.terminal(ctx); err != nil {
			return err
		}
		if writerFact.cause != nil || writerFact.unreachable {
			if refusal == nil {
				refusal = routeRefusal(writer, writer, compareSeparateRoutes(writerFact, documentRouteFact{unreachable: true}, false))
			}
		}
		for _, intent := range selected {
			writer.directory = writer.directory || intent.member.directory
		}
		for _, group := range groups {
			if !group.mattersTo(selected) {
				continue
			}
			peer := group.representative()
			peerGroup, peerGrouped := whole.indexByAlias[peer.route.alias]
			sameGroup := grouped && peerGrouped && writerGroup == peerGroup
			sameRoute := writer.location() == peer.location()
			if sameGroup || sameRoute {
				for _, intent := range selected {
					if refusal != nil {
						break
					}
					if blocking, found := group.conflict(intent); found {
						refusal = &DocumentAdmissionError{writer: intent.member, peer: blocking}
					}
				}
				continue
			}
			var peerFact documentRouteFact
			if peerGrouped {
				peerFact.observed = evidence[peerGroup].Authority
			} else {
				peerFact = observation.fact(peer.route)
			}
			if err := observation.terminal(ctx); err != nil {
				return err
			}
			problem := compareSeparateRoutes(writerFact, peerFact, writer.directory || group.directory)
			if problem != nil {
				if refusal == nil {
					refusal = routeRefusal(writer, peer, problem)
				}
				continue
			}
			// Rooted writers requalify their complete admitted namespace themselves.
			// Separate routes still need a fresh comparison at publication.
			peer.directory = group.directory
			peer.retained = group.retained
			admission.groups[writer.route.path] = append(admission.groups[writer.route.path], documentRouteCheck{writer, peer, peerFact.observed})
			if !peerGrouped && !peerFact.unreachable {
				readPaths[peerFact.locator] = struct{}{}
				readPaths[peerFact.referent] = struct{}{}
			}
		}
	}
	if err := observation.terminal(ctx); err != nil {
		return err
	}
	if refusal != nil {
		return refusal
	}
	return checkRemovalPaths(ctx, intents, whole.indexByAlias, evidence, observation.budget)
}

func checkRemovalPaths(ctx context.Context, intents []documentIntent, indexByAlias map[aggregate.DocumentAddress]int, evidence []journalobservation.ApprovalEvidence, budget *trackedDocumentRouteBudget) error {
	checked := make(map[int]struct{})
	for _, intent := range intents {
		member := intent.member
		if !intent.write || !member.removing || member.footprint.Kind() != output.FootprintPath {
			continue
		}
		index, grouped := indexByAlias[member.route.alias]
		if !grouped {
			continue
		}
		if _, done := checked[index]; done {
			continue
		}
		checked[index] = struct{}{}
		refusal := evidence[index].RequireRecoverableRemoval(ctx, budget)
		if err := ctx.Err(); err != nil {
			return err
		}
		if budget.limit != nil {
			return budget.limit
		}
		if refusal != nil {
			return routeRefusal(member, member, refusal)
		}
	}
	return ctx.Err()
}

func (group documentMembersAtRoute) mattersTo(intents []documentIntent) bool {
	for _, intent := range intents {
		if intent.write {
			return true
		}
	}
	return group.retained
}

func preservesMember(intent documentIntent, peer documentMember) bool {
	writer := intent.member
	if !intent.write && !peer.retained {
		return true
	}
	if !writer.footprint.Overlaps(peer.footprint) || writer.subject == peer.subject && writer.subject != (topology.SubjectID{}) {
		return true
	}
	if _, consumed := intent.consumes[peer.subject]; consumed {
		return true
	}
	return !intent.write && writer.contract.Cardinality() == aggregate.ContributionSharedSet && writer.contract.Equal(peer.contract)
}

var overlappingDocumentRoutes = errors.New("document routes overlap")

func routeRefusal(writer, peer documentMember, problem error) error {
	if problem == overlappingDocumentRoutes {
		problem = nil
	}
	return &DocumentAdmissionError{writer: writer, peer: peer, cause: problem}
}

func compareSeparateRoutes(writer, peer documentRouteFact, directory bool) error {
	if err := errors.Join(writer.cause, peer.cause); err != nil {
		return err
	}
	if writer.unreachable {
		return fmt.Errorf("selected route has no reachable destination")
	}
	if peer.unreachable {
		return nil
	}
	left, leftExact := writer.observed.Exact()
	right, rightExact := peer.observed.Exact()
	leftMissing, _ := writer.observed.Provisional()
	rightMissing, _ := peer.observed.Provisional()
	var overlaps bool
	switch {
	case leftExact && rightExact:
		overlaps = left.MayOverlap(right)
	case leftExact:
		overlaps = rightMissing.CouldEqual(left) || directory && rightMissing.CouldOverlap(left)
	case rightExact:
		overlaps = leftMissing.CouldEqual(right) || directory && leftMissing.CouldOverlap(right)
	default:
		overlaps = leftMissing.CouldOverlapProvisional(rightMissing)
	}
	if overlaps {
		return overlappingDocumentRoutes
	}
	return nil
}

func (admission ForwardDocumentAdmission) recheck(ctx context.Context, budget *physicalwork.Budget, paths ...output.Destination) error {
	if len(admission.groups) == 0 {
		return ctx.Err()
	}
	observation := newDocumentRouteObservation(admission.manifestRoot, admission.resolver, budget)
	var refusal error
	check := func(checks []documentRouteCheck) error {
		for _, check := range checks {
			if err := observation.terminal(ctx); err != nil {
				return err
			}
			writer, peer := check.writer, check.peer
			current := observation.fact(peer.route)
			if missing, provisional := current.observed.Provisional(); provisional && !peer.retained {
				if previous, exact := check.peerBefore.Exact(); exact && missing.MatchesMissingExact(previous) {
					// Losing a file need not lose its known normalized name.
					// This is comparison only; it cannot qualify a writer.
					current.observed = check.peerBefore
				}
			}
			problem := compareSeparateRoutes(observation.fact(writer.route), current, writer.directory || peer.directory)
			if problem != nil && refusal == nil {
				refusal = routeRefusal(writer, peer, problem)
			}
		}
		return observation.terminal(ctx)
	}
	if len(paths) == 0 {
		for _, checks := range admission.groups {
			if err := check(checks); err != nil {
				return err
			}
		}
	} else {
		for _, path := range paths {
			if err := check(admission.groups[path]); err != nil {
				return err
			}
		}
	}
	return refusal
}

var _ rootedpath.DirectoryObservationBudget = (*trackedDocumentRouteBudget)(nil)
