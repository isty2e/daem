package journal

type refusedWriterPathBudget struct {
	err   error
	calls int
}

func (budget *refusedWriterPathBudget) AdmitPathComponents(int) error {
	budget.calls++
	return budget.err
}
