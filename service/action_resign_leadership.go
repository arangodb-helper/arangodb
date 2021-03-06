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
	"time"

	"github.com/pkg/errors"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb/go-driver"
)

type ActionResignLeadership struct {
	runtimeContext runtimeServerManagerContext
}

func (a *ActionResignLeadership) Name() string {
	return "resigning leadership for dbserver"
}

func (a *ActionResignLeadership) Timeout() time.Duration {
	return getTimeoutProcessTermination(definitions.ServerTypeDBServer)
}

func (a *ActionResignLeadership) Condition(serverType definitions.ServerType) bool {
	if serverType == definitions.ServerTypeDBServer {
		return true
	}

	return false
}

func (a *ActionResignLeadership) PreStop(ctx context.Context) error {
	runtimeContext := a.runtimeContext
	jobID := ""
	sendResignLeadership := func() error {
		// create necessary API's
		clusterConfig, peer, _ := runtimeContext.ClusterConfig()
		if peer == nil {
			return errors.New("failed to get peer from cluster config")
		}

		clusterClient, err := clusterConfig.CreateClusterAPI(ctx, runtimeContext)
		if err != nil {
			return errors.Wrap(err, "failed to create cluster API")
		}
		dbServerClient, err := peer.CreateDBServerAPI(runtimeContext)
		if err != nil {
			return errors.Wrap(err, "failed to create DB server API")
		}

		// Get DB server ID.
		serverID, err := dbServerClient.ServerID(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to get DB server ID")
		}

		// Create a job for leadership resignation
		jobCtx := driver.WithJobIDResponse(ctx, &jobID)
		if err := clusterClient.ResignServer(jobCtx, serverID); err != nil {
			return errors.Wrap(err, "failed to send request for resigning leadership")
		}

		return nil
	}

	if err := retry(ctx, sendResignLeadership, time.Minute*5); err != nil {
		return maskAny(err)
	}

	// wait for the job to be finished.
	clusterConfig, _, _ := runtimeContext.ClusterConfig()
	agencyClient, err := clusterConfig.CreateAgencyAPI(runtimeContext)
	if err != nil {
		return errors.Wrap(err, "failed to create agency API")
	}

	if err := WaitForFinishedJob(ctx, jobID, agencyClient); err != nil {
		return errors.Wrapf(err, "failed waiting for the job %s to be finished", jobID)
	}

	return nil
}
