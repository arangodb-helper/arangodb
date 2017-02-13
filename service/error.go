package service

import "github.com/pkg/errors"

var (
	maskAny = errors.WithStack
)

type ErrorResponse struct {
	Error string
}
