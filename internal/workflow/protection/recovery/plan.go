package recovery

import "github.com/isty2e/daem/internal/effect/protection"

type Plan struct {
	records protection.Result
	blocker error
}

func (plan Plan) Records() []protection.Record {
	return plan.records.Records()
}

func (plan Plan) Clone() Plan    { return plan }
func (plan Plan) Blocker() error { return plan.blocker }
func (plan Plan) HasErrors() bool {
	if plan.blocker != nil {
		return true
	}
	return plan.records.Failure() != nil
}
