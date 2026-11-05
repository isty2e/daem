package publication

import journalobservation "github.com/isty2e/daem/internal/effect/journal/observation"

func reviseCaptureInput(input journalobservation.CaptureInput, revise func(*journalobservation.CaptureInputSpec)) journalobservation.CaptureInput {
	spec := journalobservation.CaptureInputSpec{
		Owner: input.Owner(), OperationID: input.OperationID(), Claims: input.Claims(),
		Provisional: input.Provisional(), Bindings: input.Bindings(), GroupIndex: input.GroupIndex(),
		Limits: input.Limits(), Budget: input.Budget(),
	}
	revise(&spec)
	return journalobservation.NewCaptureInput(spec)
}
