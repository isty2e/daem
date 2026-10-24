// Package compose assembles pure semantic projection candidates into one native
// document. Physical binding, approval, ownership and publication stay outside.
package compose

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type projectionArea struct {
	codec     aggregate.Codec
	selection aggregate.Selection
	syntax    configdocument.Syntax
	areas     []output.KeyPath
}

// ProjectionEdit is a semantic plan with its owner's native rewrite envelope.
// The envelope describes candidate projection, not an ownership claim.
type ProjectionEdit struct {
	projection projectionArea
	plan       aggregate.Plan
}

func NewProjectionEdit(codec aggregate.Codec, plan aggregate.Plan, syntax configdocument.Syntax, areas []output.KeyPath) (ProjectionEdit, error) {
	canonical, err := aggregate.NewPlan(plan.Before(), plan.Intents())
	if err != nil {
		return ProjectionEdit{}, err
	}
	projection, err := newProjectionArea(codec, canonical.Before(), syntax, areas)
	if err != nil {
		return ProjectionEdit{}, err
	}
	return ProjectionEdit{projection, canonical}, nil
}

// ProjectionBaseline retains a complete recovery selection, including blockers.
type ProjectionBaseline struct {
	projection projectionArea
	baseline   aggregate.Snapshot
}

func NewProjectionBaseline(codec aggregate.Codec, baseline aggregate.Snapshot, syntax configdocument.Syntax, areas []output.KeyPath) (ProjectionBaseline, error) {
	projection, err := newProjectionArea(codec, baseline, syntax, areas)
	if err != nil {
		return ProjectionBaseline{}, err
	}
	canonical, err := aggregate.NewSnapshot(baseline.DocumentExisted(), projection.selection, baseline.States())
	if err != nil {
		return ProjectionBaseline{}, err
	}
	return ProjectionBaseline{projection, canonical}, nil
}

func newProjectionArea(codec aggregate.Codec, snapshot aggregate.Snapshot, syntax configdocument.Syntax, areas []output.KeyPath) (projectionArea, error) {
	selection, err := snapshot.Selection()
	if err != nil {
		return projectionArea{}, err
	}
	catalog, err := aggregate.NewCodecCatalog([]aggregate.Codec{codec})
	if err != nil {
		return projectionArea{}, err
	}
	if _, present := catalog.Lookup(selection.CodecContractID()); !present {
		return projectionArea{}, fmt.Errorf("projection codec does not match its selection")
	}
	if _, err := configdocument.ParseSyntax(syntax.String()); err != nil {
		return projectionArea{}, err
	}
	if len(areas) == 0 {
		return projectionArea{}, fmt.Errorf("projection native rewrite areas are required")
	}
	for _, area := range areas {
		if area.IsRoot() {
			return projectionArea{}, fmt.Errorf("native rewrite area cannot select the document root")
		}
	}
	areas = output.TopmostKeyPaths(areas)
	contracts := selection.Contracts()
	keys := make([]output.KeyPath, 0, len(contracts))
	for _, contract := range contracts {
		keys = append(keys, contract.Address().KeyPath())
	}
	slices.SortFunc(keys, func(left, right output.KeyPath) int {
		return strings.Compare(left.String(), right.String())
	})
	covered := 0
	for _, area := range areas {
		members := 0
		exact := sort.Search(len(keys), func(index int) bool { return keys[index].String() >= area.String() })
		if exact < len(keys) && keys[exact] == area {
			members++
		}
		prefix, upper := area.String()+"/", area.String()+"0"
		first := sort.Search(len(keys), func(index int) bool { return keys[index].String() >= prefix })
		last := sort.Search(len(keys), func(index int) bool { return keys[index].String() >= upper })
		members += last - first
		if members == 0 {
			return projectionArea{}, fmt.Errorf("native rewrite area covers no selected projection")
		}
		covered += members
	}
	if covered != len(keys) {
		return projectionArea{}, fmt.Errorf("native rewrite areas do not cover the complete selection")
	}
	return projectionArea{codec, selection, syntax, areas}, nil
}
