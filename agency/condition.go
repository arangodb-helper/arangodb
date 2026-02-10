// agency/conditions.go
package agency

import "time"

type WriteCondition interface {
	GetName() string
	GetValue() any
	GetKey() []string // Get the full key path for the condition
}

type writeCondition struct {
	Key   []string
	Value any
}

func (w writeCondition) GetName() string {
	if len(w.Key) == 0 {
		return ""
	}
	return w.Key[0]
}
func (w writeCondition) GetValue() any    { return w.Value }
func (w writeCondition) GetKey() []string { return w.Key }

func IfEqualTo(key []string, value any) WriteCondition {
	return writeCondition{Key: key, Value: value}
}

// KeyEquals checks a field equals a value
func KeyEquals(key []string, field string, value any) WriteCondition {
	return writeCondition{
		Key:   append(key, field),
		Value: value,
	}
}

// KeyMissingOrExpired checks key missing or expired
func KeyMissingOrExpired(key []string, now time.Time) WriteCondition {
	return writeCondition{
		Key:   append(key, "expires"),
		Value: now,
	}
}
