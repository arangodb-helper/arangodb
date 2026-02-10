// agency/transaction.go
package agency

type Transaction struct {
	Ops   []KeyChanger
	Conds []WriteCondition
}

// Operations converts ops into the map structure expected by agency
func (tx *Transaction) Operations() map[string]map[string]any {
	ops := map[string]map[string]any{}

	for _, op := range tx.Ops {
		op.Apply(ops)
	}
	return ops
}
