package relationhost

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

func openCodePluginObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierOpenCodePlugin,
		observe: observeOpenCodePlugins,
	}
}

func observeOpenCodePlugins(input Input, records []carrierRecord) (relationobserve.BatchSpec, error) {
	recordsByScope := map[target.Scope][]carrierRecord{
		target.ScopeProject: nil,
		target.ScopeGlobal:  nil,
	}
	for _, record := range records {
		switch record.scope {
		case target.ScopeProject, target.ScopeGlobal:
			recordsByScope[record.scope] = append(recordsByScope[record.scope], record)
		default:
			return relationobserve.BatchSpec{}, fmt.Errorf(
				"OpenCode plugin relation scope %q is not observable",
				record.scope,
			)
		}
	}

	spec := relationobserve.BatchSpec{}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		scopedRecords := recordsByScope[scope]
		if len(scopedRecords) == 0 {
			continue
		}
		directory, err := openCodeConfigDirectory(input, scope)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		documents, err := observeOpenCodeConfigDocuments(directory)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		for _, document := range documents {
			authorityPath, err := relationobserve.NewAuthorityPath(
				document.path,
				target.TargetOpenCode,
				scope,
			)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.AuthorityPaths = append(spec.AuthorityPaths, authorityPath)
		}
		for _, record := range scopedRecords {
			result, err := correlateOpenCodeRelation(record, documents)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
				Key:    record.key,
				Result: result,
			})
		}
	}
	return spec, nil
}

type openCodeConfigObservation struct {
	path     string
	exists   bool
	document opencodeconfig.Document
}

func observeOpenCodeConfigDocuments(directory string) ([]openCodeConfigObservation, error) {
	documents := make([]openCodeConfigObservation, 0, 2)
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		path, err := selectOpenCodeConfigPath(directory, kind)
		if err != nil {
			return nil, err
		}
		observation := openCodeConfigObservation{path: path}
		content, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				documents = append(documents, observation)
				continue
			}
			return nil, fmt.Errorf("read OpenCode %s config %q: %w", kind, path, err)
		}
		observation.exists = true
		if strings.TrimSpace(string(content)) == "" {
			content = []byte("{}")
		}
		document, err := opencodeconfig.Parse(content)
		if err != nil {
			return nil, fmt.Errorf("observe OpenCode %s config %q: %w", kind, path, err)
		}
		observation.document = document
		documents = append(documents, observation)
	}
	return documents, nil
}

func selectOpenCodeConfigPath(
	directory string,
	kind opencodeconfig.ConfigKind,
) (string, error) {
	name, err := opencodeconfig.SelectName(kind, func(name string) (bool, error) {
		_, err := os.Stat(filepath.Join(directory, name))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
			return false, nil
		default:
			return false, err
		}
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, name), nil
}

func openCodeConfigDirectory(input Input, scope target.Scope) (string, error) {
	switch scope {
	case target.ScopeProject:
		root := filepath.Clean(input.Paths.ManifestRoot)
		if input.Paths.ManifestRoot == "" || !filepath.IsAbs(root) || root != input.Paths.ManifestRoot {
			return "", fmt.Errorf("OpenCode project config requires an absolute clean manifest root")
		}
		return filepath.Join(root, ".opencode"), nil
	case target.ScopeGlobal:
		root := os.Getenv("XDG_CONFIG_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("resolve OpenCode user home: %w", err)
			}
			root = filepath.Join(home, ".config")
		}
		clean := filepath.Clean(root)
		if !filepath.IsAbs(clean) || clean != root {
			return "", fmt.Errorf("OpenCode config root %q must be absolute and clean", root)
		}
		return filepath.Join(clean, "opencode"), nil
	default:
		return "", fmt.Errorf("OpenCode plugin relation scope %q is not observable", scope)
	}
}

func correlateOpenCodeRelation(
	record carrierRecord,
	documents []openCodeConfigObservation,
) (relationobserve.CorrelationResult, error) {
	if err := record.key.Validate(); err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"OpenCode relation correlation key: %w",
			err,
		)
	}
	expected := record.key.ExpectedRelation()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		string(expected.SubjectKey()),
	)
	if err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf("OpenCode relation source: %w", err)
	}
	if _, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		record.scope,
		source,
	); err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf("OpenCode relation carrier: %w", err)
	}

	present := false
	for _, document := range documents {
		if !document.exists {
			continue
		}
		switch count := document.document.ExactSourceCount(source.Ref()); count {
		case 0:
		case 1:
			present = true
		default:
			return relationobserve.CorrelationResult{}, fmt.Errorf(
				"OpenCode config %q contains %d exact plugin rows for source %q",
				document.path,
				count,
				source.Ref(),
			)
		}
	}

	rows := make([]relationobserve.Row, 0, 1)
	if present {
		row, err := relationobserve.NewRow(relationobserve.RowSpec{
			SubjectKey:            source.Ref(),
			HasManagedInstanceKey: true,
			ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
		})
		if err != nil {
			return relationobserve.CorrelationResult{}, err
		}
		rows = append(rows, row)
	}
	inventory, err := relationobserve.NewInventory(relationobserve.InventorySpec{
		Availability: relationobserve.InventorySupported,
		Freshness:    relationobserve.EvidenceFresh,
		Rows:         rows,
	})
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	return relationobserve.Correlate(expected, inventory), nil
}
