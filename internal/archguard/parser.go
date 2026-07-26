package archguard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ParseGoListJSON parses concatenated JSON package objects emitted by
// go list -json.
func ParseGoListJSON(data []byte) ([]PackageRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []PackageRecord
	for {
		var record PackageRecord
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse go list json: %w", err)
		}
		if record.ImportPath == "" {
			return nil, fmt.Errorf("parse go list json: package record missing ImportPath")
		}
		records = append(records, record)
	}
	return records, nil
}

// AnalyzeGoListReport parses go list -json output and returns classified topology findings.
func AnalyzeGoListReport(data []byte) (Report, error) {
	records, err := ParseGoListJSON(data)
	if err != nil {
		return Report{}, err
	}
	return AnalyzeReport(records), nil
}
