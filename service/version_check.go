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
	"regexp"
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
func (s *Service) DatabaseVersion(ctx context.Context) (driver.Version, bool, bool, error) {
	return DatabaseVersion(ctx, s.log, s.cfg.ArangodPath, s.runner)
}

// DatabaseVersion returns the version, enterprise status, and V8 support status
// of the `arangod` binary.
func DatabaseVersion(ctx context.Context, log zerolog.Logger, arangodPath string, runner Runner) (driver.Version, bool, bool, error) {
	retries := 25
	var err error
	for i := 0; i < retries; i++ {
		var v driver.Version
		var enterprise bool
		var hasV8Support bool
		v, enterprise, hasV8Support, err = databaseVersion(ctx, log, arangodPath, runner)
		if err == nil {
			return v, enterprise, hasV8Support, nil
		}

		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", false, false, ctxErr
		}

		log.Warn().Err(err).Msgf("Error while getting version. Attempt %d of %d", i+1, retries)
		time.Sleep(time.Second)
	}

	return "", false, false, fmt.Errorf("unable to get version: %s", err.Error())
}

func databaseVersion(ctx context.Context, log zerolog.Logger, arangodPath string, runner Runner) (driver.Version, bool, bool, error) {
	// Start process to print version info
	output := &bytes.Buffer{}
	containerName := "arangodb-versioncheck-" + strings.ToLower(uniuri.NewLen(6))
	// Try with --version first
	versionArgs := []string{"--version", "--log.force-direct=true"}
	p, err := runner.Start(ctx, definitions.ProcessTypeArangod, arangodPath, versionArgs, nil, nil, nil, containerName, ".", output)
	if err != nil {
		return "", false, false, errors.WithStack(err)
	}
	defer p.Cleanup()
	code := p.Wait()
	if code != 0 {
		// If --version fails, try with ARANGO_NO_AUTH environment variable
		output.Reset()
		envs := map[string]string{"ARANGO_NO_AUTH": "1"}
		p2, err2 := runner.Start(ctx, definitions.ProcessTypeArangod, arangodPath, versionArgs, envs, nil, nil, containerName+"-retry", ".", output)
		if err2 != nil {
			return "", false, false, fmt.Errorf("process exited with exit code %d - %s", code, output.String())
		}
		defer p2.Cleanup()
		if code2 := p2.Wait(); code2 != 0 {
			return "", false, false, fmt.Errorf("process exited with exit code %d - %s", code2, output.String())
		}
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
		return "", false, false, fmt.Errorf("no server-version found in '%s'", stdout)
	} else {
		v = driver.Version(vs)
	}

	l := "community"
	if lc, ok := parsedLines["license"]; ok {
		l = lc
	}
	enterprise := l == "enterprise"

	// Check version output for V8/JavaScript indicators
	// Default value is false (V8 disabled) when v8-version is not "none"
	var hasV8Support bool = false
	v8SupportFromVersion := checkV8InVersionOutput(stdout, log)
	if v8SupportFromVersion != nil {
		hasV8Support = *v8SupportFromVersion
	} else {
		// If we can't determine from version output, default to false (V8 disabled)
		// This means JavaScript parameters will NOT be added
		log.Warn().Msg("Could not determine V8 support from version output, defaulting to false (JavaScript parameters will NOT be added)")
		hasV8Support = false
	}

	return v, enterprise, hasV8Support, nil
}

var v8VersionRegex = regexp.MustCompile(`v8-version:\s*\d+`)

// checkV8InVersionOutput checks the version output for V8/JavaScript indicators
// Returns nil if unable to determine, true/false if determined
func checkV8InVersionOutput(versionOutput string, log zerolog.Logger) *bool {
	lowerOutput := strings.ToLower(versionOutput)

	if strings.Contains(lowerOutput, "v8-version:") {

		lines := strings.Split(versionOutput, "\n")

		for _, line := range lines {
			lowerLine := strings.ToLower(strings.TrimSpace(line))
			log.Info().Msgf("Checking line: %s", lowerLine)

			if strings.HasPrefix(lowerLine, "v8-version:") {

				// Case 1: V8 disabled
				if strings.Contains(lowerLine, "v8-version: none") {
					log.Info().Msg("Detected: V8 disabled (none).")
					result := false
					return &result
				}

				// Case 2: V8 version number present
				if v8VersionRegex.MatchString(lowerLine) {
					log.Info().Msg("Detected: V8 enabled (numeric version).")
					result := true
					return &result
				}

				// Case 3: Unexpected/malformed v8-version line
				log.Warn().Str("line", line).Msg("Unexpected or malformed v8-version line in version output.")
			}
		}
	}

	// We did not find any v8-version field
	return nil
}
