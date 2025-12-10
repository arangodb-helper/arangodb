//
// DISCLAIMER
//
// Copyright 2024 ArangoDB GmbH, Cologne, Germany
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

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/arangodb-helper/arangodb/pkg/arangodb"
	"github.com/arangodb-helper/arangodb/pkg/docker"
	"github.com/arangodb-helper/arangodb/service"
)

var (
	// test mode -> detected DB features
	supportedDBFeatures = make(map[string]*service.DatabaseFeatures)
)

func getSupportedDatabaseFeatures(t *testing.T, testMode string) service.DatabaseFeatures {
	dbFeatures, ok := supportedDBFeatures[testMode]
	if !ok || dbFeatures == nil {
		t.Logf("Detecting arangod version for mode %s...", testMode)
		log := zerolog.New(zerolog.NewConsoleWriter())

		arangodPath, _ := arangodb.FindExecutable(log, "arangod", "/usr/sbin/arangod", false)
		v, enterprise, hasV8Support, err := service.DatabaseVersion(context.Background(), log, arangodPath, makeRunner(t, log, testMode))
		require.NoError(t, err)
		t.Logf("Detected arangod %s, enterprise: %v, V8 support: %v", v, enterprise, hasV8Support)

		f := service.NewDatabaseFeatures(v, enterprise, hasV8Support)
		supportedDBFeatures[testMode] = &f
	}

	return *supportedDBFeatures[testMode]
}

func makeRunner(t *testing.T, log zerolog.Logger, testMode string) service.Runner {
	var err error
	runner := service.NewProcessRunner(log)
	if testMode == testModeDocker {
		dockerImage := os.Getenv("ARANGODB")
		if dockerImage == "" {
			dockerImage = os.Getenv("DOCKER_IMAGE")
		}
		dockerConfig := service.DockerConfig{
			Endpoint:     "unix:///var/run/docker.sock",
			GCDelay:      time.Minute,
			TTY:          true,
			ImageArangoD: dockerImage,
		}
		if docker.IsRunningInDocker() {
			info, err := docker.FindDockerContainerInfo(dockerConfig.Endpoint)
			if err == nil {
				dockerConfig.HostContainerName = info.Name
			}
		}

		runner, err = service.NewDockerRunner(log, dockerConfig)
		require.NoError(t, err, "could not create docker runner")
	}
	return runner
}
