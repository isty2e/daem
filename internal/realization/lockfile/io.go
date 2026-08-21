package lockfile

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
)

const minimumRegenerableVersion = 3

// UnsupportedVersionError reports a lock schema that this reader cannot use.
type UnsupportedVersionError struct {
	Found     int
	Supported int
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
	return err.Supported == lock.CurrentVersion &&
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
	versionErr := UnsupportedVersionError{Found: version, Supported: lock.CurrentVersion}
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
	if version != lock.CurrentVersion {
		return lock.File{}, UnsupportedVersionError{
			Found:     version,
			Supported: lock.CurrentVersion,
		}
	}
	return loadCurrentContent(ctx, content)
}

func lockfileVersion(ctx context.Context, content []byte) (int, error) {
	if err := declarationartifact.Admit(content); err != nil {
		return 0, err
	}
	if !utf8.Valid(content) {
		return 0, fmt.Errorf("lockfile is not valid UTF-8")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	text := strings.TrimPrefix(string(content), "\uFEFF")
	var envelope string
	for line := range strings.SplitSeq(text, "\n") {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		envelope = line
		break
	}
	if envelope == "" {
		return 0, fmt.Errorf("lockfile version envelope is required")
	}
	if err := tomlstrict.Admit(ctx, []byte(envelope), tomlstrict.StandardLimits()); err != nil {
		return 0, err
	}
	var header struct {
		Version int `toml:"version"`
	}
	metadata, err := toml.Decode(envelope, &header)
	if err != nil {
		return 0, err
	}
	if len(metadata.Undecoded()) != 0 || header.Version <= 0 {
		return 0, fmt.Errorf("lockfile must begin with a version = N envelope")
	}
	return header.Version, nil
}

func loadCurrentContent(ctx context.Context, content []byte) (lock.File, error) {
	if err := tomlstrict.Admit(ctx, content, tomlstrict.StandardLimits()); err != nil {
		return lock.File{}, err
	}

	var header struct {
		Version int `toml:"version"`
	}
	headerMetadata, err := toml.Decode(string(content), &header)
	if err != nil {
		return lock.File{}, err
	}
	if err := validateLockedShapes(&headerMetadata); err != nil {
		return lock.File{}, err
	}

	var dto fileDTO
	metadata, err := toml.Decode(string(content), &dto)
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
	if err := tomlstrict.Admit(context.Background(), content, tomlstrict.StandardLimits()); err != nil {
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
