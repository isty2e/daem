package store

type documentSettlementCountingBudget struct{ calls int }

func (budget *documentSettlementCountingBudget) AdmitPathComponents(int) error {
	budget.calls++
	return nil
}
