package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
)

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
	if _, err := toml.Decode(string(content), &header); err != nil {
		return lock.File{}, err
	}
	if header.Version != lock.CurrentVersion {
		return lock.File{}, fmt.Errorf("unsupported lockfile version %d", header.Version)
	}

	var dto fileDTO
	metadata, err := toml.Decode(string(content), &dto)
	if err != nil {
		return lock.File{}, err
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		return lock.File{}, fmt.Errorf("unknown lockfile key %q", undecoded[0].String())
	}
	if err := validateLockedShapes(&metadata); err != nil {
		return lock.File{}, err
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
	for index, contract := range file.Locked.Subjects() {
		contribution, present, err := contract.ManagedAggregateContribution()
		if err != nil {
			return fmt.Errorf("locked subject[%d] aggregate contribution: %w", index, err)
		}
		if !present {
			continue
		}
		if err := codecs.ValidateSubjectContribution(
			contract.SubjectID(),
			contribution.Contribution(),
		); err != nil {
			return fmt.Errorf("locked subject[%d] aggregate contribution: %w", index, err)
		}
	}
	return nil
}
