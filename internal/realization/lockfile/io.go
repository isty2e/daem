package lockfile

import (
	"context"
	"errors"
	"fmt"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
)

// Current lockfiles are collection-dense and can legitimately approach the
// selected-skill ceiling. Capacity follows observed structure rather than raw
// bytes so comments, formatting, and primitive payload length cannot expand
// the amount of container or path material admitted before decoding.
const (
	minimumRegenerableVersion                int64 = 3
	currentLockfileVersion                   int64 = lock.CurrentVersion
	lockfileStructuralUnitsPerExtraContainer       = 24
	lockfilePathWorkPerStructuralUnit              = 3
	lockfileContextCheckInterval                   = 4096
)

func currentLockfileStructureLimits(structuralUnits int) tomlstrict.Limits {
	limits := tomlstrict.StandardLimits()
	limits.MaximumContainers += structuralUnits / lockfileStructuralUnitsPerExtraContainer
	if structuralUnits%lockfileStructuralUnitsPerExtraContainer != 0 {
		limits.MaximumContainers++
	}
	limits.MaximumWork += structuralUnits
	limits.MaximumPathWork += lockfilePathWorkPerStructuralUnit * structuralUnits
	return limits
}

func admitCurrentLockfileStructure(ctx context.Context, content []byte) error {
	maximumLimits := currentLockfileStructureLimits(int(declarationartifact.MaximumBytes))
	usage, err := tomlstrict.AdmitWithUsage(ctx, content, maximumLimits)
	if err != nil {
		return err
	}
	return usage.Validate(currentLockfileStructureLimits(usage.StructuralUnits()))
}

// UnsupportedVersionError reports a lock schema that this reader cannot use.
type UnsupportedVersionError struct {
	Found     int64
	Supported int64
}

func (err UnsupportedVersionError) Error() string {
	if err.RelockSupported() {
		return fmt.Sprintf(
			"unsupported lockfile version %d; run daem lock to regenerate schema version %d",
			err.Found,
			err.Supported,
		)
	}
	if err.Found > err.Supported {
		return fmt.Sprintf(
			"unsupported lockfile version %d; lockfile was written by a newer daem (supported schema version %d)",
			err.Found,
			err.Supported,
		)
	}
	return fmt.Sprintf("unsupported lockfile version %d", err.Found)
}

// BoundedErrorEvidence returns the version-policy diagnostic without reading
// lockfile content.
func (err UnsupportedVersionError) BoundedErrorEvidence(maximumRunes int) (string, bool) {
	if maximumRunes <= 0 {
		return "", true
	}
	evidence := err.Error()
	if len(evidence) <= maximumRunes {
		return evidence, false
	}
	return evidence[:maximumRunes], true
}

// RelockSupported reports whether the lock workflow may replace this exact
// prior schema without interpreting its contents.
func (err UnsupportedVersionError) RelockSupported() bool {
	return err.Supported == currentLockfileVersion &&
		err.Found >= minimumRegenerableVersion &&
		err.Found < err.Supported
}

// Load reads an daem.lock.toml file.
func Load(ctx context.Context, path string) (lock.File, error) {
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return lock.File{}, err
	}
	return loadContent(ctx, content)
}

// ReadReplacementContent reads exact lockfile bytes and verifies that the
// authoring workflow may replace them without interpreting unsupported state.
func ReadReplacementContent(ctx context.Context, path string) ([]byte, error) {
	content, err := declarationartifact.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := validateReplacementContent(ctx, content); err != nil {
		return nil, err
	}
	return content, nil
}

// validateReplacementContent verifies that existing lockfile bytes may be
// replaced by a newly generated current lockfile. Current content remains
// strict; explicitly supported legacy schemas are treated as opaque.
func validateReplacementContent(ctx context.Context, content []byte) error {
	version, err := lockfileVersion(ctx, content)
	if err != nil {
		return err
	}
	if version == lock.CurrentVersion {
		_, err := loadCurrentContent(ctx, content)
		return err
	}
	versionErr := UnsupportedVersionError{Found: version, Supported: currentLockfileVersion}
	if versionErr.RelockSupported() {
		return nil
	}
	return versionErr
}

func loadContent(ctx context.Context, content []byte) (lock.File, error) {
	version, err := lockfileVersion(ctx, content)
	if err != nil {
		return lock.File{}, err
	}
	if version != currentLockfileVersion {
		return lock.File{}, UnsupportedVersionError{
			Found:     version,
			Supported: currentLockfileVersion,
		}
	}
	return loadCurrentContent(ctx, content)
}

func lockfileVersion(ctx context.Context, content []byte) (int64, error) {
	if err := declarationartifact.Admit(content); err != nil {
		return 0, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := tomlstrict.ValidateUTF8(ctx, content); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, err
		}
		return 0, fmt.Errorf("lockfile is not valid UTF-8")
	}
	envelope, err := firstSignificantTOMLLine(ctx, content)
	if err != nil {
		return 0, err
	}
	if len(envelope) == 0 {
		return 0, fmt.Errorf("lockfile version envelope is required")
	}
	if err := tomlstrict.Admit(ctx, envelope, tomlstrict.StandardLimits()); err != nil {
		return 0, err
	}
	var header versionEnvelopeDTO
	metadata, err := tomlstrict.DecodeAdmitted(ctx, envelope, &header)
	if err != nil {
		return 0, err
	}
	if len(metadata.Undecoded()) != 0 || header.Version <= 0 {
		return 0, fmt.Errorf("lockfile must begin with a version = N envelope")
	}
	return header.Version, nil
}

func firstSignificantTOMLLine(ctx context.Context, content []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	index := 0
	if len(content) >= 3 && content[0] == 0xef && content[1] == 0xbb && content[2] == 0xbf {
		index = 3
	}
	cancelAt := index
	checkContext := func(position int) error {
		if position-cancelAt < lockfileContextCheckInterval {
			return nil
		}
		cancelAt = position
		return ctx.Err()
	}

	for index < len(content) {
		first := -1
		last := -1
		lineEnd := index
		for lineEnd < len(content) && content[lineEnd] != '\n' {
			if !isTOMLLineWhitespace(content[lineEnd]) {
				if first < 0 {
					first = lineEnd
				}
				last = lineEnd + 1
			}
			lineEnd++
			if err := checkContext(lineEnd); err != nil {
				return nil, err
			}
		}
		if first >= 0 && content[first] != '#' {
			return content[first:last], nil
		}
		index = lineEnd
		if index < len(content) {
			index++
			if err := checkContext(index); err != nil {
				return nil, err
			}
		}
	}
	return nil, ctx.Err()
}

func isTOMLLineWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r'
}

func loadCurrentContent(ctx context.Context, content []byte) (lock.File, error) {
	if err := admitCurrentLockfileStructure(ctx, content); err != nil {
		return lock.File{}, err
	}

	var header versionEnvelopeDTO
	headerMetadata, err := tomlstrict.DecodeAdmitted(ctx, content, &header)
	if err != nil {
		return lock.File{}, err
	}
	if err := validateLockedShapes(&headerMetadata); err != nil {
		return lock.File{}, err
	}

	var dto fileDTO
	metadata, err := tomlstrict.DecodeAdmitted(ctx, content, &dto)
	if err != nil {
		return lock.File{}, err
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return lock.File{}, fmt.Errorf("unknown lockfile key %q", undecoded[0].String())
	}
	file, err := snapshotFromDTO(dto)
	if err != nil {
		return lock.File{}, err
	}
	if err := lock.Validate(file); err != nil {
		return lock.File{}, err
	}
	if err := validateConcreteAggregateContributions(file); err != nil {
		return lock.File{}, err
	}
	if err := validateCanonicalDTO(dto, file); err != nil {
		return lock.File{}, err
	}
	return file, nil
}

// Marshal renders the lockfile as TOML.
func Marshal(file lock.File) ([]byte, error) {
	if err := lock.Validate(file); err != nil {
		return nil, err
	}
	if err := validateConcreteAggregateContributions(file); err != nil {
		return nil, err
	}

	output := declarationartifact.NewOutputBuffer()
	dto, err := dtoFromSnapshot(file)
	if err != nil {
		return nil, err
	}
	if err := toml.NewEncoder(&output).Encode(dto); err != nil {
		return nil, err
	}
	content := output.Bytes()
	if err := admitCurrentLockfileStructure(context.Background(), content); err != nil {
		return nil, fmt.Errorf("lockfile TOML structure: %w", err)
	}
	return content, nil
}

func validateConcreteAggregateContributions(file lock.File) error {
	codecs := aggregatecodec.Catalog()
	items := make([]aggregate.SubjectContribution, 0)
	firstSubjectIndexByAddress := make(map[aggregate.ProjectionAddress]int)
	for index, contract := range file.Locked.Subjects() {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return fmt.Errorf("locked subject[%d] aggregate contribution: %w", index, err)
		}
		if !present {
			continue
		}
		address := contribution.Contribution().Address()
		if _, exists := firstSubjectIndexByAddress[address]; !exists {
			firstSubjectIndexByAddress[address] = index
		}
		items = append(items, contribution)
	}
	sets, err := aggregate.PartitionContributionSets(items)
	if err != nil {
		return fmt.Errorf("locked aggregate contributions: %w", err)
	}
	for _, set := range sets {
		firstSubjectIndex := firstSubjectIndexByAddress[set.Address()]
		if err := codecs.ValidateContributionSet(set); err != nil {
			return fmt.Errorf(
				"locked subject[%d] aggregate contribution set: %w",
				firstSubjectIndex,
				err,
			)
		}
	}
	return nil
}
