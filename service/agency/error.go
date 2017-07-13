package agency

import (
	"fmt"

	"github.com/pkg/errors"
)

var (
	maskAny              = errors.WithStack
	ConditionFailedError = errors.New("Condition failed")
	KeyNotFoundError     = errors.New("Key not found")
)

type StatusError struct {
	StatusCode int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("Status %d", e.StatusCode)
}

func IsStatusError(err error) (int, bool) {
	err = errors.Cause(err)
	if serr, ok := err.(StatusError); ok {
		return serr.StatusCode, true
	}
	return 0, false
}

func IsConditionFailed(err error) bool {
	return errors.Cause(err) == ConditionFailedError
}

func IsKeyNotFound(err error) bool {
	return errors.Cause(err) == KeyNotFoundError
}
