package diagnoseworkflow

import (
	"fmt"
	"io/fs"
	"os"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/diagnose"
	"github.com/isty2e/daem/internal/findings"
)

type manifestLoadStage uint8

const (
	manifestLoadStageUnknown manifestLoadStage = iota
	manifestLoadStageReady
	manifestLoadStageReadFailure
	manifestLoadStageParseFailure
)

type manifestLoad struct {
	stage manifestLoadStage
	facts diagnose.ManifestFacts
	err   error
}

type manifestLoader struct {
	stat       func(string) (fs.FileInfo, error)
	readFile   func(string) ([]byte, error)
	normalize  func([]byte) (desired.Environment, error)
	buildFacts func(desired.Environment) (diagnose.ManifestFacts, error)
}

var doctorManifestLoader = defaultDoctorManifestLoader()

func defaultDoctorManifestLoader() manifestLoader {
	return manifestLoader{
		stat:       os.Stat,
		readFile:   os.ReadFile,
		normalize:  declarationmanifest.Decode,
		buildFacts: diagnose.NewManifestFacts,
	}
}

func (loader manifestLoader) load(path string) manifestLoad {
	if _, err := loader.stat(path); err != nil {
		return manifestLoad{stage: manifestLoadStageReadFailure, err: err}
	}

	content, err := loader.readFile(path)
	if err != nil {
		return manifestLoad{stage: manifestLoadStageReadFailure, err: err}
	}
	normalized, err := loader.normalize(content)
	if err != nil {
		return manifestLoad{stage: manifestLoadStageParseFailure, err: err}
	}
	facts, err := loader.buildFacts(normalized)
	if err != nil {
		return manifestLoad{stage: manifestLoadStageParseFailure, err: err}
	}

	return manifestLoad{stage: manifestLoadStageReady, facts: facts}
}

func (loaded manifestLoad) ready() bool {
	return loaded.stage == manifestLoadStageReady
}

func manifestCheck(path string, explicit bool, loaded manifestLoad) findings.Check {
	switch loaded.stage {
	case manifestLoadStageReady:
		return findings.OKCheck("manifest", fmt.Sprintf("%s is parseable", path))
	case manifestLoadStageReadFailure:
		if os.IsNotExist(loaded.err) && !explicit {
			return findings.WarnCheck("manifest", fmt.Sprintf("%s not found; running general diagnostics", path))
		}
		return findings.ErrorCheck("manifest", fmt.Sprintf("read %s: %v", path, loaded.err))
	case manifestLoadStageParseFailure:
		return findings.ErrorCheck("manifest", fmt.Sprintf("parse %s: %v", path, loaded.err))
	default:
		return findings.ErrorCheck("manifest", "manifest load produced an invalid internal stage")
	}
}

func allResourceKinds() map[entity.Kind]struct{} {
	return map[entity.Kind]struct{}{
		entity.KindInstructions: {},
		entity.KindSkill:        {},
		entity.KindHook:         {},
	}
}
