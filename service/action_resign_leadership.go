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

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/service/actions"
	"github.com/arangodb/go-driver"
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

	clusterConfig, _, serviceMode := a.runtimeContext.ClusterConfig()
	if serviceMode != ServiceModeCluster {
		return false
	}

	//log.Info().Bool("enough", clusterConfig.HaveEnoughAgents()).Msg("test")
	//log.Info().Interface("config", clusterConfig).Msg("test")
	//log.Info().Interface("peer", peer).Msg("test")
	if len(clusterConfig.AllPeers) <= 1 {
		// It is the last starter so it does not make sense to resign leadership.
		return false
	}

	return true
}

// PreStop runs action before server is stopped.
func (a *ActionResignLeadership) PreStop(ctx context.Context, progress actions.Progressor) error {
	serverID := ""
	var clusterClient driver.Cluster

	getServerID := func() error {
		// create necessary API's
		clusterConfig, peer, _ := a.runtimeContext.ClusterConfig()
		if peer == nil {
			progress.Progress("failed to get peer from cluster config")
			return errors.New("failed to get peer from cluster config")
		}

		var err error
		clusterClient, err = clusterConfig.CreateClusterAPI(ctx, a.runtimeContext)
		if err != nil {
			progress.Progress("failed to create cluster API")
			return errors.Wrap(err, "failed to create cluster API")
		}
		dbServerClient, err := peer.CreateDBServerAPI(a.runtimeContext)
		if err != nil {
			progress.Progress("failed to create DB server API")
			return errors.Wrap(err, "failed to create DB server API")
		}

		// Get DB server ID.
		serverID, err = dbServerClient.ServerID(ctx)
		if err != nil {
			progress.Progress("failed to get DB server ID")
			return errors.Wrap(err, "failed to get DB server ID")
		}

		return nil
	}

	if err := retry(ctx, getServerID, a.Timeout()-time.Second*5); err != nil {
		return maskAny(err)
	}

	// Create a job for leadership resignation
	jobID := ""
	jobCtx := driver.WithJobIDResponse(ctx, &jobID)
	if err := clusterClient.ResignServer(jobCtx, serverID); err != nil {
		return errors.Wrap(err, "failed to send request for resigning leadership")
	}

	progress.Progress(fmt.Sprintf("leadership resignation waits for the job ID %s to be finished", jobID))

	// wait for the job to be finished.
	clusterConfig, _, _ := a.runtimeContext.ClusterConfig()
	agencyClient, err := clusterConfig.CreateAgencyAPI(a.runtimeContext)
	if err != nil {
		return errors.Wrap(err, "failed to create agency API")
	}

	if err := WaitForFinishedJob(ctx, jobID, agencyClient); err != nil {
		return errors.Wrapf(err, "failed waiting for the job %s to be finished", jobID)
	}

	return nil
}
