package cli

import (
	"io"
	"testing"
)

func TestProgressAdmissionRequiresTerminalTextStderr(t *testing.T) {
	tests := []struct {
		name       string
		jsonOutput bool
		stderr     io.Writer
		options    commandOptions
		want       bool
	}{
		{
			name:    "terminal text",
			stderr:  io.Discard,
			options: commandOptions{stderrIsTerminal: true},
			want:    true,
		},
		{
			name:       "json",
			jsonOutput: true,
			stderr:     io.Discard,
			options:    commandOptions{stderrIsTerminal: true},
		},
		{
			name:   "non-terminal",
			stderr: io.Discard,
		},
		{
			name:    "nil stderr",
			options: commandOptions{stderrIsTerminal: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lockAdmitted := newLockProgressRenderer(
				test.jsonOutput,
				test.stderr,
				test.options,
			) != nil
			applyAdmitted := newApplyProgressRenderer(
				test.jsonOutput,
				test.stderr,
				test.options,
			) != nil
			if lockAdmitted != test.want || applyAdmitted != test.want {
				t.Fatalf(
					"lock/apply admission = %t/%t, want %t",
					lockAdmitted,
					applyAdmitted,
					test.want,
				)
			}
		})
	}
}

func TestNilLockProgressRendererHasNoWorkflowSink(t *testing.T) {
	if sink := lockWorkflowProgressSink(nil); sink != nil {
		t.Fatal("nil renderer returned a workflow progress sink")
	}
}
