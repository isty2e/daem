package diagnose

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/findings"
)

type ConfigFormat string

const (
	ConfigFormatJSON ConfigFormat = "json"
	ConfigFormatTOML ConfigFormat = "toml"
)

const doctorHostConfigMaximumBytes int64 = 4 << 20

var errMultipleJSONValues = errors.New("multiple JSON values")

type doctorConfigFile struct {
	Path                string
	Format              ConfigFormat
	SyntaxErrorSeverity findings.Severity
}

func configFileCheck(ctx context.Context, name string, configFile doctorConfigFile) findings.Check {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return errorCheck(name, fmt.Sprintf("read %s: %v", configFile.Path, err))
	}

	content, exists, err := filesnapshot.ReadRegularFileReferentContext(
		ctx,
		configFile.Path,
		doctorHostConfigMaximumBytes,
	)
	if err != nil {
		return configFileReadError(name, configFile.Path, err)
	}
	if !exists {
		return warnCheck(name, fmt.Sprintf("%s is missing", configFile.Path))
	}

	if err := parseConfigFileBytes(ctx, configFile, content); err != nil {
		if configFile.SyntaxErrorSeverity == findings.SeverityWarn && isConfigSyntaxError(err) {
			return warnCheck(name, fmt.Sprintf("strict parse of %s failed: %v", configFile.Path, err))
		}

		return errorCheck(name, fmt.Sprintf("parse %s: %v", configFile.Path, err))
	}

	return okCheck(name, fmt.Sprintf("%s is parseable", configFile.Path))
}

func configFileReadError(name string, path string, err error) findings.Check {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return warnCheck(name, fmt.Sprintf("%s is missing", path))
	case errors.Is(err, filesnapshot.ErrNotRegular):
		return errorCheck(name, fmt.Sprintf("%s is not a regular file", path))
	case errors.Is(err, filesnapshot.ErrLimitExceeded):
		return errorCheck(name, fmt.Sprintf("read %s: file exceeds %d bytes", path, doctorHostConfigMaximumBytes))
	default:
		return errorCheck(name, fmt.Sprintf("read %s: %v", path, err))
	}
}

func parseConfigFileBytes(ctx context.Context, configFile doctorConfigFile, content []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch configFile.Format {
	case ConfigFormatTOML:
		if err := tomlstrict.Admit(ctx, content, tomlstrict.StandardLimits()); err != nil {
			return err
		}
		var decoded map[string]toml.Primitive
		if _, err := tomlstrict.DecodeAdmitted(ctx, content, &decoded); err != nil {
			return err
		}
		return nil
	case ConfigFormatJSON:
		if err := jsonstrict.Validate(content, "host config", tomlstrict.MaximumDepth); err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(content))
		var decoded map[string]json.RawMessage
		if err := decoder.Decode(&decoded); err != nil {
			return err
		}

		var extra json.RawMessage
		err := decoder.Decode(&extra)
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
	return errors.As(err, &jsonSyntaxError) ||
		errors.Is(err, errMultipleJSONValues) ||
		errors.Is(err, jsonstrict.ErrMultipleValues) ||
		errors.Is(err, jsonstrict.ErrDuplicateObjectKey)
}
