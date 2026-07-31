package mutation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultWaitInterval = 10 * time.Millisecond
	defaultMaximumWait  = 5 * time.Second
)

// ContentionError reports that a bounded lease wait expired.
type ContentionError struct {
	domain string
}

func (err ContentionError) Error() string {
	return fmt.Sprintf("mutation domain %s is busy; retry after the other daem operation finishes", err.domain)
}

// CancellationError reports caller cancellation while acquiring a lease set.
type CancellationError struct {
	domain string
	cause  error
}

func (err CancellationError) Error() string {
	return fmt.Sprintf("mutation lease acquisition canceled for %s: %v", err.domain, err.cause)
}

// Unwrap exposes the caller cancellation or deadline cause.
func (err CancellationError) Unwrap() error {
	return err.cause
}

// Store owns the private OS-backed lock-record boundary for mutation domains.
type Store struct {
	dataDir     string
	root        string
	rootKey     string
	rootWitness pathSemanticsWitness
	maximum     time.Duration
	interval    time.Duration
}

// NewStore constructs a mutation lease store below one daem data directory.
func NewStore(dataDir string) (Store, error) {
	identity, err := canonicalPathIdentity(dataDir, PathEffectReferent)
	if err != nil {
		return Store{}, fmt.Errorf("resolve mutation lease data directory: %w", err)
	}
	root := filepath.Join(identity.accessPath, "locks", "mutation", "v1")
	rootIdentity, err := initialLeaseRootIdentity(identity.accessPath, root)
	if err != nil {
		return Store{}, fmt.Errorf("resolve mutation lease root: %w", err)
	}
	return Store{
		dataDir:     identity.accessPath,
		root:        rootIdentity.accessPath,
		rootKey:     rootIdentity.keyPath,
		rootWitness: rootIdentity.witness,
		maximum:     defaultMaximumWait,
		interval:    defaultWaitInterval,
	}, nil
}

// DataDir returns the physical data directory selected when this lease store
// was constructed. Callers may use it to keep later data-root effects on the
// same authority as the acquired leases.
func (store Store) DataDir() string {
	return store.dataDir
}

func (store Store) matchesRootIdentity(identity canonicalPath) bool {
	return identity.keyPath == store.rootKey && identity.witness == store.rootWitness
}

type normalizedDomain struct {
	key    string
	label  string
	access AccessMode
}

type heldLease struct {
	domain normalizedDomain
	record leaseRecord
}

type leaseRecord interface {
	Unlock() error
}

type preparedLeaseNamespace interface {
	Acquire(
		context.Context,
		string,
		AccessMode,
		time.Duration,
	) (leaseRecord, bool, error)
	ValidateCurrent() error
	Close() error
}

// LeaseSet owns one acquired mutation domain set.
type LeaseSet struct {
	domains   []Domain
	held      []heldLease
	namespace preparedLeaseNamespace
	mu        sync.RWMutex
	once      sync.Once
	released  bool
	err       error
}

// Acquire normalizes and acquires one complete mutation domain set.
func (store Store) Acquire(ctx context.Context, domains ...Domain) (*LeaseSet, error) {
	if ctx == nil {
		return nil, fmt.Errorf("mutation lease context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, CancellationError{domain: "requested set", cause: err}
	}
	if len(domains) == 0 {
		return nil, fmt.Errorf("mutation lease domain set is required")
	}
	normalized, err := store.normalize(domains)
	if err != nil {
		return nil, err
	}
	namespace, err := store.openLeaseNamespace()
	if err != nil {
		return nil, err
	}

	waitContext, cancel := context.WithTimeout(ctx, store.maximum)
	defer cancel()
	set := &LeaseSet{
		domains:   append([]Domain(nil), domains...),
		held:      make([]heldLease, 0, len(normalized)),
		namespace: namespace,
	}
	for _, domain := range normalized {
		record, locked, lockErr := namespace.Acquire(
			waitContext,
			lockRecordName(domain.key),
			domain.access,
			store.interval,
		)
		if lockErr != nil {
			releaseErr := set.Release()
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, errors.Join(CancellationError{domain: domain.label, cause: ctxErr}, releaseErr)
			}
			if errors.Is(lockErr, context.DeadlineExceeded) {
				return nil, errors.Join(ContentionError{domain: domain.label}, releaseErr)
			}
			return nil, errors.Join(fmt.Errorf("acquire mutation domain %s: %w", domain.label, lockErr), releaseErr)
		}
		if !locked {
			return nil, errors.Join(ContentionError{domain: domain.label}, set.Release())
		}
		set.held = append(set.held, heldLease{domain: domain, record: record})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, errors.Join(CancellationError{domain: domain.label, cause: ctxErr}, set.Release())
		}
	}
	return set, nil
}

// DomainsMatchCurrent reports whether path identities still match the domains actually acquired.
func (set *LeaseSet) DomainsMatchCurrent(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("mutation lease context is required")
	}
	if set == nil {
		return false, fmt.Errorf("mutation lease set is not initialized")
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return false, fmt.Errorf("mutation lease set is not active")
	}
	if set.namespace == nil {
		return false, fmt.Errorf("mutation lease namespace is not initialized")
	}
	if err := set.namespace.ValidateCurrent(); err != nil {
		return false, fmt.Errorf("validate mutation lease namespace: %w", err)
	}
	for _, domain := range set.domains {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		matches, err := domain.matchesCurrentPath()
		if err != nil {
			return false, err
		}
		if !matches {
			return false, nil
		}
	}
	return true, nil
}

// Release releases the acquired set in reverse order. It is safe to call repeatedly.
func (set *LeaseSet) Release() error {
	if set == nil {
		return nil
	}
	set.once.Do(func() {
		set.mu.Lock()
		defer set.mu.Unlock()
		failures := make([]error, 0)
		for index := len(set.held) - 1; index >= 0; index-- {
			if err := set.held[index].record.Unlock(); err != nil {
				failures = append(failures, fmt.Errorf("release mutation domain %s: %w", set.held[index].domain.label, err))
			}
		}
		if set.namespace != nil {
			if err := set.namespace.Close(); err != nil {
				failures = append(failures, fmt.Errorf("release mutation lease namespace: %w", err))
			}
		}
		set.released = true
		set.err = errors.Join(failures...)
	})
	return set.err
}

func lockRecordName(key string) string {
	digest := sha256.Sum256([]byte(key))
	return hex.EncodeToString(digest[:]) + ".lock"
}

type pathDomainFact struct {
	path    string
	witness pathSemanticsWitness
	access  AccessMode
}

// hasExclusivePathAncestor memoizes whether each visited prefix is at or below
// an explicit exclusive fact. The facts map stays immutable during this pass.
func hasExclusivePathAncestor(
	path string,
	facts map[string]*pathDomainFact,
	exclusiveCoverage map[string]bool,
) bool {
	ancestor := filepath.Dir(path)
	if ancestor == path {
		return false
	}

	var unresolved []string
	covered := false
	for {
		if fact := facts[ancestor]; fact != nil && fact.access == AccessExclusive {
			exclusiveCoverage[ancestor] = true
			covered = true
			break
		}
		if cached, present := exclusiveCoverage[ancestor]; present {
			covered = cached
			break
		}
		unresolved = append(unresolved, ancestor)
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			break
		}
		ancestor = parent
	}
	for _, path := range unresolved {
		exclusiveCoverage[path] = covered
	}
	return covered
}

func (store Store) normalize(domains []Domain) ([]normalizedDomain, error) {
	paths := make(map[string]*pathDomainFact)
	hosts := make(map[string][2]string)
	routes := make([]Domain, 0)
	for _, domain := range domains {
		if err := domain.access.validate(); err != nil {
			return nil, err
		}
		switch domain.kind {
		case domainLogicalPath, domainPhysicalPath:
			if domain.canonicalPath == "" || domain.pathWitness == "" {
				return nil, fmt.Errorf("mutation path domain is not initialized")
			}
			if pathContains(domain.canonicalPath, store.rootKey) {
				return nil, fmt.Errorf("mutation path %q contains the lease store", domain.canonicalPath)
			}
			fact := paths[domain.canonicalPath]
			if fact == nil {
				fact = &pathDomainFact{
					path: domain.canonicalPath, witness: domain.pathWitness, access: domain.access,
				}
				paths[domain.canonicalPath] = fact
			} else if fact.witness != domain.pathWitness {
				return nil, fmt.Errorf("mutation path %q has contradictory filesystem semantics", domain.canonicalPath)
			} else if domain.access == AccessExclusive {
				fact.access = AccessExclusive
			}
			if domain.kind == domainPhysicalPath {
				hostKey := encodedKey("host", domain.target, domain.scope)
				hosts[hostKey] = [2]string{domain.target, domain.scope}
			}
		case domainHostRoute:
			if err := domain.containment.validate(); err != nil {
				return nil, err
			}
			routes = append(routes, domain)
		default:
			return nil, fmt.Errorf("mutation domain is not initialized")
		}
	}

	exclusiveCoverage := make(map[string]bool)
	retainedPaths := make(map[string]*pathDomainFact, len(paths))
	for path, fact := range paths {
		covered := hasExclusivePathAncestor(path, paths, exclusiveCoverage)
		exclusiveCoverage[path] = covered || fact.access == AccessExclusive
		if !covered {
			retainedPaths[path] = fact
		}
	}
	paths = retainedPaths

	entries := make(map[string]normalizedDomain)
	add := func(key string, label string, access AccessMode) {
		_, ok := entries[key]
		if !ok || access == AccessExclusive {
			entries[key] = normalizedDomain{key: key, label: label, access: access}
		}
	}
	for _, fact := range paths {
		add(pathKey(fact.path), strconv.Quote(fact.path), fact.access)
	}
	expandedAncestors := make(map[string]struct{})
	for _, fact := range paths {
		ancestor := filepath.Dir(fact.path)
		for ancestor != fact.path {
			add(pathKey(ancestor), strconv.Quote(ancestor), AccessShared)
			if _, expanded := expandedAncestors[ancestor]; expanded {
				break
			}
			expandedAncestors[ancestor] = struct{}{}
			parent := filepath.Dir(ancestor)
			if parent == ancestor {
				break
			}
			ancestor = parent
		}
	}
	for _, host := range hosts {
		addHostIntents(add, host[0], host[1])
	}
	for _, route := range routes {
		addHostIntents(add, route.target, route.scope)
		switch route.containment {
		case RouteContainmentCompletePaths:
			key := encodedKey("host-route", route.target, route.scope, route.family)
			add(key, fmt.Sprintf("host route %q/%q/%q", route.target, route.scope, route.family), AccessExclusive)
		case RouteContainmentScope:
			key := encodedKey("host-scope", route.target, route.scope)
			add(key, fmt.Sprintf("host scope %q/%q", route.target, route.scope), AccessExclusive)
		case RouteContainmentUnknown:
			key := encodedKey("host-target", route.target)
			add(key, fmt.Sprintf("host target %q", route.target), AccessExclusive)
		}
	}

	normalized := make([]normalizedDomain, 0, len(entries))
	for _, entry := range entries {
		normalized = append(normalized, entry)
	}
	sort.Slice(normalized, func(left int, right int) bool {
		return normalized[left].key < normalized[right].key
	})
	return normalized, nil
}

func addHostIntents(add func(string, string, AccessMode), target string, scope string) {
	add(encodedKey("host-target", target), fmt.Sprintf("host target %q", target), AccessShared)
	add(encodedKey("host-scope", target, scope), fmt.Sprintf("host scope %q/%q", target, scope), AccessShared)
}

func pathKey(path string) string {
	return encodedKey("path", filepath.ToSlash(path))
}

func encodedKey(fields ...string) string {
	var builder strings.Builder
	for _, field := range fields {
		builder.WriteString(strconv.Itoa(len(field)))
		builder.WriteByte(':')
		builder.WriteString(field)
	}
	return builder.String()
}
