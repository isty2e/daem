package diagnose

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/findings"
)

type ConfigFormat string

const (
	ConfigFormatJSON ConfigFormat = "json"
	ConfigFormatTOML ConfigFormat = "toml"
)

var errMultipleJSONValues = errors.New("multiple JSON values")

type doctorConfigFile struct {
	Path                string
	Format              ConfigFormat
	SyntaxErrorSeverity findings.Severity
}

func configFileCheck(name string, configFile doctorConfigFile) findings.Check {
	if _, err := os.Stat(configFile.Path); err != nil {
		if os.IsNotExist(err) {
			return warnCheck(name, fmt.Sprintf("%s is missing", configFile.Path))
		}

		return errorCheck(name, fmt.Sprintf("stat %s: %v", configFile.Path, err))
	}

	if err := parseConfigFile(configFile); err != nil {
		if configFile.SyntaxErrorSeverity == findings.SeverityWarn && isConfigSyntaxError(err) {
			return warnCheck(name, fmt.Sprintf("strict parse of %s failed: %v", configFile.Path, err))
		}

		return errorCheck(name, fmt.Sprintf("parse %s: %v", configFile.Path, err))
	}

	return okCheck(name, fmt.Sprintf("%s is parseable", configFile.Path))
}

func parseConfigFile(configFile doctorConfigFile) error {
	switch configFile.Format {
	case ConfigFormatTOML:
		var decoded map[string]toml.Primitive
		if _, err := toml.DecodeFile(configFile.Path, &decoded); err != nil {
			return err
		}
		return nil
	case ConfigFormatJSON:
		file, err := os.Open(configFile.Path)
		if err != nil {
			return err
		}
		defer file.Close()

		decoder := json.NewDecoder(file)
		var decoded map[string]json.RawMessage
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}

		var extra json.RawMessage
		err = decoder.Decode(&extra)
		if err == nil {
			return errMultipleJSONValues
		}
		if !errors.Is(err, io.EOF) {
			return err
		}

		return nil
	default:
		return fmt.Errorf("unsupported config format %q", configFile.Format)
	}
}

func isConfigSyntaxError(err error) bool {
	var jsonSyntaxError *json.SyntaxError
	return errors.As(err, &jsonSyntaxError) || errors.Is(err, errMultipleJSONValues)
}
