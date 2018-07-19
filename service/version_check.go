//
// DISCLAIMER
//
// Copyright 2018 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//
// Author Ewout Prangsma
//

package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	driver "github.com/arangodb/go-driver"
)

// DatabaseVersion returns the version of the `arangod` binary that is being
// used by this starter.
func (s *Service) DatabaseVersion(ctx context.Context) (driver.Version, error) {
	// Start process to print version info
	output := &bytes.Buffer{}
	p, err := s.runner.Start(ctx, ProcessTypeArangod, s.cfg.ArangodPath, []string{"--version"}, nil, nil, "", ".", output)
	if err != nil {
		return "", maskAny(err)
	}
	//defer p.Cleanup()
	p.Wait()

	// Parse output
	lines := strings.Split(output.String(), "\n")
	for _, l := range lines {
		parts := strings.Split(l, ":")
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) != "server-version" {
			continue
		}
		return driver.Version(strings.TrimSpace(parts[1])), nil
	}
	return "", fmt.Errorf("No server-version found")
}
