// agency/errors.go (or at the top of agency.go)
package agency

import (
	"errors"
	"strings"
)

// ErrKeyNotFound is returned when a requested key does not exist in the agency
var ErrKeyNotFound = errors.New("key not found")

func IsKeyNotFound(err error) bool {
	return err == ErrKeyNotFound || (err != nil && strings.Contains(err.Error(), "key not found"))
}
