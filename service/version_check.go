//
// DISCLAIMER
//
// Copyright 2018-2024 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dchest/uniuri"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/arangodb/go-driver"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// DatabaseVersion returns the version of the `arangod` binary that is being
// used by this starter.
func (s *Service) DatabaseVersion(ctx context.Context) (driver.Version, bool, error) {
	return DatabaseVersion(ctx, s.log, s.cfg.ArangodPath, s.runner)
}

// DatabaseVersion returns the version of the `arangod` binary
func DatabaseVersion(ctx context.Context, log zerolog.Logger, arangodPath string, runner Runner) (driver.Version, bool, error) {
	retries := 25
	var err error
	for i := 0; i < retries; i++ {
		var v driver.Version
		var enterprise bool
		v, enterprise, err = databaseVersion(ctx, arangodPath, runner)
		if err == nil {
			return v, enterprise, nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", false, ctxErr
		}

		log.Warn().Err(err).Msgf("Error while getting version. Attempt %d of %d", i+1, retries)
		time.Sleep(time.Second)
	}

	return "", false, fmt.Errorf("unable to get version: %s", err.Error())
}

func databaseVersion(ctx context.Context, arangodPath string, runner Runner) (driver.Version, bool, error) {
	// Start process to print version info
	output := &bytes.Buffer{}
	containerName := "arangodb-versioncheck-" + strings.ToLower(uniuri.NewLen(6))
	p, err := runner.Start(ctx, definitions.ProcessTypeArangod, arangodPath, []string{"--version", "--log.force-direct=true"}, nil, nil, nil, containerName, ".", output)
	if err != nil {
		return "", false, errors.WithStack(err)
	}
	defer p.Cleanup()
	if code := p.Wait(); code != 0 {
		return "", false, fmt.Errorf("process exited with exit code %d - %s", code, output.String())
	}

	// Parse output
	stdout := output.String()
	lines := strings.Split(stdout, "\n")
	parsedLines := map[string]string{}
	for _, l := range lines {
		parts := strings.Split(l, ":")
		if len(parts) != 2 {
			continue
		}
		parsedLines[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	var v driver.Version

	if vs, ok := parsedLines["server-version"]; !ok {
		return "", false, fmt.Errorf("no server-version found in '%s'", stdout)
	} else {
		fmt.Println("ArangoDB version found: ", vs)
		v = driver.Version(vs)
	}

	l := "community"
	if lc, ok := parsedLines["license"]; ok {
		l = lc
	}

	return v, l == "enterprise", nil
}
