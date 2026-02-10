// agency/health.go
package agency

import (
	"context"
	"fmt"
	"time"

	driver_http "github.com/arangodb/go-driver/v2/connection"
)

func AreAgentsHealthy(ctx context.Context, conns []driver_http.Connection) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	testKey := []string{"arango", "Plan"}
	var result any

	for _, conn := range conns {
		client := &client{conn: conn}

		if err := client.ReadKey(ctx, testKey, &result); err != nil {
			if IsKeyNotFound(err) {
				continue
			}
			return fmt.Errorf("agency health check failed: %w", err)
		}
	}

	return nil
}
