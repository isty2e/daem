package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
)

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
	return fmt.Sprintf("unsupported lockfile version %d", err.Found)
}

// RelockSupported reports whether the lock workflow may replace this exact
// prior schema without interpreting its contents.
func (err UnsupportedVersionError) RelockSupported() bool {
	return err.Found == 5 && err.Supported == 6
}

// Load reads an daem.lock.toml file.
func Load(path string) (lock.File, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return lock.File{}, err
	}
	if !utf8.Valid(content) {
		return lock.File{}, fmt.Errorf("lockfile is not valid UTF-8")
	}

	var header struct {
		Version int `toml:"version"`
	}
	headerMetadata, err := toml.Decode(string(content), &header)
	if err != nil {
		return lock.File{}, err
	}
	if header.Version != lock.CurrentVersion {
		return lock.File{}, UnsupportedVersionError{
			Found:     header.Version,
			Supported: lock.CurrentVersion,
		}
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

	var output bytes.Buffer
	dto, err := dtoFromSnapshot(file)
	if err != nil {
		return nil, err
	}
	if err := toml.NewEncoder(&output).Encode(dto); err != nil {
		return nil, err
	}

	return output.Bytes(), nil
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
