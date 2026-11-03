package clijson

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

type Recover struct {
	SchemaVersion          int    `json:"schema_version"`
	Command                string `json:"command"`
	Mode                   string `json:"mode"`
	Phase                  string `json:"phase"`
	AuthorityKind          string `json:"authority_kind"`
	OperationID            string `json:"operation_id"`
	OperationDir           string `json:"operation_dir"`
	Classification         string `json:"classification"`
	ActionCount            int    `json:"action_count"`
	CleanupObligationCount int    `json:"cleanup_obligation_count"`
	HasErrors              bool   `json:"has_errors"`
	ContinuingFileSetFence string `json:"continuing_file_set_fence"`
	Actions                []struct {
		Kind    string `json:"kind"`
		Reason  string `json:"reason"`
		Subject struct {
			Kind      string `json:"kind"`
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
		} `json:"subject"`
		ResourceID string `json:"resource_id"`
		Resource   struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"resource"`
		Target      string   `json:"target"`
		Targets     []string `json:"targets"`
		Scope       string   `json:"scope"`
		Destination string   `json:"destination"`
		ContentKind string   `json:"content_kind"`
		BackupPath  string   `json:"backup_path"`
		BackupHash  string   `json:"backup_hash"`
		BackupKind  string   `json:"backup_kind"`
		Detail      string   `json:"detail"`
	} `json:"actions"`
	CleanupObligations []struct {
		Action      string `json:"action"`
		Readiness   string `json:"readiness"`
		Reason      string `json:"reason"`
		Scope       string `json:"scope"`
		Destination string `json:"destination"`
		Detail      string `json:"detail"`
	} `json:"cleanup_obligations"`
}

func DecodeRecover(t *testing.T, content []byte) Recover {
	t.Helper()

	var payload Recover
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("Decode returned error: %v\ncontent=%s", err, content)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("unexpected trailing JSON content: %s", content)
	}

	return payload
}
