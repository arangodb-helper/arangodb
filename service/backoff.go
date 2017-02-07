package service

import (
	"time"

	"github.com/cenkalti/backoff"
	"github.com/juju/errgo"
)

type PermanentError struct {
	Err error
}

func (e *PermanentError) Error() string {
	return e.Err.Error()
}

func retry(op func() error, timeout time.Duration) error {
	var failure error
	wrappedOp := func() error {
		if err := op(); err == nil {
			return nil
		} else {
			if pe, ok := errgo.Cause(err).(*PermanentError); ok {
				// Detected permanent error
				failure = pe.Err
				return nil
			} else {
				return err
			}
		}
	}
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = timeout
	b.MaxInterval = timeout / 3
	if err := backoff.Retry(wrappedOp, b); err != nil {
		return maskAny(err)
	}
	if failure != nil {
		return maskAny(failure)
	}
	return nil
}
