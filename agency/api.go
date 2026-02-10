// agency/api.go
package agency

import (
	"context"
	"time"
)

// Starter-scoped API (NOT v1 compatible)
type Agency interface {
	Read(ctx context.Context, key []string, out any) error
	ReadKey(ctx context.Context, key []string, out any) error

	Write(ctx context.Context, tx *Transaction) error
	WriteKey(ctx context.Context, key []string, value any, ttl time.Duration, conditions ...WriteCondition) error
	RemoveKey(ctx context.Context, key []string, conditions ...WriteCondition) error
	RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error
}
