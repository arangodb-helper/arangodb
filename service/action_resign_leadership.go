//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
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
// Author Tomasz Mielech
//

package service

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/arangodb/go-driver"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/actions"
)

// ActionResignLeadership describes action for the leadership resignation.
type ActionResignLeadership struct {
	runtimeContext runtimeServerManagerContext
}

// Name returns name of the action.
func (a *ActionResignLeadership) Name() string {
	return "resigning leadership"
}

// Timeout returns how long it should wait for the action to be finished.
func (a *ActionResignLeadership) Timeout() time.Duration {
	return getTimeoutProcessTermination(definitions.ServerTypeDBServer)
}

// Condition returns true if this action should be launched.
func (a *ActionResignLeadership) Condition(serverType definitions.ServerType) bool {
	if serverType != definitions.ServerTypeDBServer {
		return false
	}

	_, _, serviceMode := a.runtimeContext.ClusterConfig()
	if serviceMode != ServiceModeCluster {
		return false
	}

	return true
}

// PreStop runs action before server is stopped.
func (a *ActionResignLeadership) PreStop(ctx context.Context, progress actions.Progressor) error {
	serverID, err := a.getDBServerID(ctx, progress)
	if err != nil {
		return maskAny(err)
	}

	jobID, err := a.resignLeadership(ctx, serverID)
	if err != nil {
		return maskAny(err)
	}

	// wait for the job to be finished.
	clusterConfig, _, _ := a.runtimeContext.ClusterConfig()
	agencyClient, err := clusterConfig.CreateAgencyAPI(a.runtimeContext)
	if err != nil {
		return errors.Wrap(err, "failed to create agency API")
	}

	progress.Progress(fmt.Sprintf("leadership resignation waits for the job ID %s to be finished", jobID))

	if err := WaitForFinishedJob(progress, ctx, jobID, agencyClient); err != nil {
		return errors.Wrapf(err, "failed waiting for the job %s to be finished", jobID)
	}

	return nil
}

// getDBServerID gets DB server ID .
func (a *ActionResignLeadership) getDBServerID(ctx context.Context, progress actions.Progressor) (string, error) {
	serverID := ""

	getServerID := func() error {
		_, peer, _ := a.runtimeContext.ClusterConfig()
		if peer == nil {
			return progress.Progress("failed to get peer from cluster config")
		}

		dbServerClient, err := peer.CreateDBServerAPI(a.runtimeContext)
		if err != nil {
			return progress.Progress("failed to create DB server API")
		}

		// Get DB server ID.
		serverID, err = dbServerClient.ServerID(ctx)
		if err != nil {
			return progress.Progress("failed to get DB server ID")
		}

		return nil
	}

	if err := retry(ctx, getServerID, a.Timeout()); err != nil {
		return "", err
	}

	return serverID, nil
}

// resignLeadership creates a job for resignation leadership.
func (a *ActionResignLeadership) resignLeadership(ctx context.Context, serverID string) (string, error) {
	jobID := ""
	resignLeadership := func() error {
		clusterConfig, peer, _ := a.runtimeContext.ClusterConfig()
		if peer == nil {
			return errors.New("failed to get peer from cluster config")
		}

		clusterClient, err := clusterConfig.CreateClusterAPI(ctx, a.runtimeContext)
		if err != nil {
			return errors.Wrap(err, "failed to create cluster API")
		}

		jobCtx := driver.WithJobIDResponse(ctx, &jobID)
		if err := clusterClient.ResignServer(jobCtx, serverID); err != nil {
			return errors.Wrap(err, "failed to send request for resigning leadership")
		}

		return nil
	}

	// Retry for a few seconds to create resignation job.
	// If it is the last start instance then there is not coordinator and it will return error.
	if err := retry(ctx, resignLeadership, time.Second*3); err != nil {
		return "", err
	}

	return jobID, nil
}
