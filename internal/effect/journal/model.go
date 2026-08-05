package journal

import (
	"fmt"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/topology"
)

const recoveryJournalFileName = "journal.json"

const recoveryOperationApply = "apply"

// Paths identifies the host paths recovery needs without depending on CLI command state.
type Paths struct {
	RecoveryDir   string
	StatefilePath string
	ManifestRoot  string
	DataDir       string
}

type recoveryJournal struct {
	Version               int                                `json:"version"`
	OperationID           string                             `json:"operation_id"`
	Operation             string                             `json:"operation"`
	CreatedAt             string                             `json:"created_at"`
	ProjectRootProvenance *recoveryRootProvenance            `json:"project_root_provenance,omitempty"`
	Entries               []recoveryEntry                    `json:"entries"`
	StatefileBefore       durable.Snapshot                   `json:"-"`
	StatefileAfter        durable.Snapshot                   `json:"-"`
	ClaimTransitions      []recoveryClaimTransition          `json:"claim_transitions,omitempty"`
	ProvisionalAcquires   []recoveryProvisionalAcquireIntent `json:"provisional_acquire_intents,omitempty"`
}

type recoveryRootProvenance struct {
	PhysicalRoot      string `json:"physical_root"`
	ObjectFingerprint string `json:"object_fingerprint"`
	MountFingerprint  string `json:"mount_fingerprint"`
}

type recoveryGlobalPathBinding struct {
	ResolvedPath   string                 `json:"resolved_path"`
	RootProvenance recoveryRootProvenance `json:"root_provenance"`
}

// recoveryEntry is the exact journal-v11 persistence DTO. Subject is the sole
// semantic identity carried across the recovery boundary. GlobalPathBinding
// binds a global logical destination to its capture-time physical root
// incarnation; OwnershipPathAuthority is a separate foreign key to one exact
// claim transition.
type recoveryEntry struct {
	Subject                persistedSubjectRef        `json:"subject"`
	Target                 string                     `json:"target,omitempty"`
	Targets                []string                   `json:"targets,omitempty"`
	Scope                  string                     `json:"scope"`
	Path                   string                     `json:"path"`
	GlobalPathBinding      *recoveryGlobalPathBinding `json:"global_path_binding,omitempty"`
	ContentPath            string                     `json:"content_path,omitempty"`
	ContentKind            string                     `json:"content_kind,omitempty"`
	OwnershipPathAuthority *pathAuthorityDTO          `json:"ownership_path_authority,omitempty"`
	Before                 recoveryBeforePathDTO      `json:"before"`
	ExpectedAfter          recoveryExpectedPathDTO    `json:"expected_after"`
	StateBeforeIdentity    *recoveryStateIdentity     `json:"state_before_identity,omitempty"`
	StateBefore            recoveryManagedMembership  `json:"state_before"`
	StateExpectedAfter     recoveryManagedMembership  `json:"state_expected_after"`
	Aggregate              *recoveryAggregateContract `json:"aggregate,omitempty"`
	StateIndependent       bool                       `json:"state_independent,omitempty"`
}

type recoveryManagedMembership struct {
	Managed     bool   `json:"managed"`
	ContentHash string `json:"content_hash,omitempty"`
}

// recoveryStateIdentity is the exact nested journal-v11 persistence DTO used to
// correlate statefile rows.
type recoveryStateIdentity struct {
	Subject     persistedSubjectRef `json:"subject"`
	Target      string              `json:"target,omitempty"`
	Targets     []string            `json:"targets,omitempty"`
	Scope       string              `json:"scope"`
	Path        string              `json:"path"`
	ContentPath string              `json:"content_path,omitempty"`
	ContentKind string              `json:"content_kind,omitempty"`
	Aggregate   bool                `json:"aggregate,omitempty"`
}

type persistedSubjectRef struct {
	Kind      string `json:"kind,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

func (subject persistedSubjectRef) canonical() (topology.SubjectID, error) {
	hasKind := subject.Kind != ""
	hasNamespace := subject.Namespace != ""
	hasName := subject.Name != ""
	if !hasKind && !hasNamespace && !hasName {
		return topology.SubjectID{}, fmt.Errorf("subject is required")
	}
	if !hasKind || !hasNamespace || !hasName {
		return topology.SubjectID{}, fmt.Errorf("kind, namespace, and name are required together")
	}
	id, err := topology.NewSubjectID(topology.SubjectKind(subject.Kind), subject.Namespace, subject.Name)
	if err != nil {
		return topology.SubjectID{}, err
	}
	return id, nil
}

// CaptureResult identifies the committed recovery journal for one interrupted operation.
// Stable persistence remains governed by the storage durability contract.
type CaptureResult struct {
	OperationID       string
	Directory         string
	JournalPath       string
	RecordFingerprint string
}

// OperationID returns a stable apply recovery operation identifier for a timestamp.
func OperationID(createdAt time.Time) string {
	return createdAt.UTC().Format("20060102T150405.000000000Z") + "-apply"
}
