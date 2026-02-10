package agency

import (
	"context"
	"fmt"
	"net/http"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

type client struct {
	conn driver_http.Connection
	http *http.Client
}

var _ Agency = (*client)(nil)

// NewAgencyConnection creates a new HTTP connection for agency operations.
// It wraps driver_http.NewHttpConnection to provide a consistent interface
// that returns an error (for consistency with other connection creation patterns).
func NewAgencyConnection(config driver_http.HttpConfiguration) (driver_http.Connection, error) {
	if config.Endpoint == nil {
		return nil, fmt.Errorf("agency.NewAgencyConnection: endpoint cannot be nil")
	}
	conn := driver_http.NewHttpConnection(config)
	return conn, nil
}

func NewAgency(conn driver_http.Connection) (Agency, error) {
	if conn == nil {
		return nil, fmt.Errorf("agency.New: connection cannot be nil")
	}
	return &client{
		conn: conn,
		http: &http.Client{},
	}, nil
}

func (c *client) RemoveKeyIfEqualTo(ctx context.Context, key []string, oldValue interface{}) error {
	tx := &Transaction{
		Ops: []KeyChanger{
			RemoveKey(key),
		},
		Conds: []WriteCondition{
			KeyEquals(key, "", oldValue), // adjust depending on your condition struct
		},
	}
	return c.Write(ctx, tx)
}
