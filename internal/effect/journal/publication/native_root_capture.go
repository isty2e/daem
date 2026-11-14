package publication

import (
	"context"
	"fmt"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/output"
)

func nativeCaptureRootFacts(ctx context.Context, batch NativeDocumentObservationBatch) (output.RootReference, []journalrecord.RootCapture, error) {
	qualified, err := batch.roots.Qualify(ctx, batch.budget)
	if err != nil {
		return output.RootReference{}, nil, err
	}
	manifest, present := qualified.Manifest()
	if !present {
		return output.RootReference{}, nil, fmt.Errorf("native17 capture requires its qualified manifest root")
	}
	references := map[output.RootReference]bool{manifest.RootReference(): true}
	for _, namespace := range batch.Evidence().Namespaces() {
		for _, alias := range namespace.Aliases() {
			references[alias.Root().RootReference()] = true
		}
		reference, bound := namespace.WriterPin().RootReference()
		if !bound {
			return output.RootReference{}, nil, fmt.Errorf("native17 capture cannot infer an unbound writer reference")
		}
		references[reference] = true
	}
	rows := make([]journalrecord.RootCapture, 0, len(references))
	for reference := range references {
		if err := ctx.Err(); err != nil {
			return output.RootReference{}, nil, err
		}
		grant, present := qualified.Selection(reference)
		if !present {
			return output.RootReference{}, nil, fmt.Errorf("native17 capture root is not independently qualified")
		}
		alternative, _ := grant.PointAlternative()
		row, err := journalrecord.NewRootCapture(reference, grant.Provenance(), alternative)
		if err != nil {
			return output.RootReference{}, nil, err
		}
		rows = append(rows, row)
	}
	return manifest.RootReference(), rows, nil
}
