package cli_test

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"
)

type doctorJSONTestPayload struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Manifest      struct {
		Path     string `json:"path"`
		Explicit bool   `json:"explicit"`
	} `json:"manifest"`
	Targets    []string `json:"targets"`
	CheckCount int      `json:"check_count"`
	HasErrors  bool     `json:"has_errors"`
	Checks     []struct {
		Status        string   `json:"status"`
		Name          string   `json:"name"`
		Detail        string   `json:"detail"`
		Repairability string   `json:"repairability"`
		RepairActions []string `json:"repair_actions"`
		ManualReasons []string `json:"manual_reasons"`
		NextStep      string   `json:"next_step"`
	} `json:"checks"`
}

func decodeDoctorJSONTestPayload(t *testing.T, content []byte) doctorJSONTestPayload {
	t.Helper()

	var payload doctorJSONTestPayload
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

func assertDoctorJSONCheck(t *testing.T, payload doctorJSONTestPayload, status string, name string) {
	t.Helper()

	_ = findDoctorJSONCheck(t, payload, status, name)
}

func findDoctorJSONCheck(t *testing.T, payload doctorJSONTestPayload, status string, name string) struct {
	Status        string   `json:"status"`
	Name          string   `json:"name"`
	Detail        string   `json:"detail"`
	Repairability string   `json:"repairability"`
	RepairActions []string `json:"repair_actions"`
	ManualReasons []string `json:"manual_reasons"`
	NextStep      string   `json:"next_step"`
} {
	t.Helper()

	for _, check := range payload.Checks {
		if check.Status == status && check.Name == name {
			return check
		}
	}
	t.Fatalf("checks = %#v, want %s %s", payload.Checks, status, name)
	return struct {
		Status        string   `json:"status"`
		Name          string   `json:"name"`
		Detail        string   `json:"detail"`
		Repairability string   `json:"repairability"`
		RepairActions []string `json:"repair_actions"`
		ManualReasons []string `json:"manual_reasons"`
		NextStep      string   `json:"next_step"`
	}{}
}

func assertDoctorJSONCheckDetail(
	t *testing.T,
	payload doctorJSONTestPayload,
	status string,
	name string,
	detail string,
) {
	t.Helper()

	for _, check := range payload.Checks {
		if check.Status == status && check.Name == name && check.Detail == detail {
			return
		}
	}
	t.Fatalf("checks = %#v, want %s %s detail %q", payload.Checks, status, name, detail)
}

func assertDoctorJSONManifestPath(t *testing.T, got string, want string) {
	t.Helper()

	resolvedGot, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) returned error: %v", got, err)
	}
	resolvedWant, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) returned error: %v", want, err)
	}
	if resolvedGot != resolvedWant {
		t.Fatalf("manifest path = %q, want %q", got, want)
	}
}
