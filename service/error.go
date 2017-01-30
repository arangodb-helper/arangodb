package service

import (
	"github.com/juju/errgo"
)

var (
	maskAny = errgo.MaskFunc(errgo.Any)
)

type ErrorResponse struct {
	Error string
}
