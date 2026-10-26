package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/reconcile"
)

type nativeNarrowingActionJSON struct {
	Name            string `json:"name"`
	Target          string `json:"target"`
	Scope           string `json:"scope"`
	Destination     string `json:"destination"`
	DesiredKeyCount int    `json:"desired_key_count"`
	ValuesOmitted   bool   `json:"values_omitted"`
}

func nativeNarrowingJSONActions(actions []reconcile.NativeNarrowingAction) []nativeNarrowingActionJSON {
	var rows []nativeNarrowingActionJSON
	for _, action := range actions {
		placement, _ := action.SourceContract().HostConfigPlacement()
		rows = append(rows, nativeNarrowingActionJSON{
			Name: action.SourceContract().EntityID().Name(), Target: string(placement.Target()), Scope: string(placement.Scope()),
			Destination: placement.Destination().String(), DesiredKeyCount: len(action.DesiredContributions()), ValuesOmitted: true,
		})
	}
	return rows
}

func printNativeNarrowingActions(output io.Writer, actions []reconcile.NativeNarrowingAction) {
	if len(actions) == 0 {
		return
	}
	fmt.Fprintf(output, "configuration ownership transitions: %s\n", CountLabel(len(actions), "transition", "transitions"))
	for _, row := range nativeNarrowingJSONActions(actions) {
		fmt.Fprintf(output, "  host-config=%s target=%s scope=%s\n", Quote(row.Name), Escape(row.Target), Escape(row.Scope))
		fmt.Fprintf(output, "    %s: document -> keys (managed keys=%d; values omitted)\n", Quote(row.Destination), row.DesiredKeyCount)
	}
}
