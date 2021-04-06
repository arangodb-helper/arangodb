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

	"github.com/arangodb-helper/arangodb/service/actions"

	"github.com/arangodb/go-driver/agency"
	"github.com/pkg/errors"
)

type JobState string

type JobStatus struct {
	Reason string   `json:"reason,omitempty"`
	Server string   `json:"server,omitempty"`
	JobID  string   `json:"jobId,omitempty"`
	Type   string   `json:"type,omitempty"`
	state  JobState `json:"-"`
}

var (
	JobStateToDo     = JobState("ToDo")
	JobStatePending  = JobState("Pending")
	JobStateFinished = JobState("Finished")
	JobStateFailed   = JobState("Failed")

	agencyJobStateKeyPrefixes = []agency.Key{
		{"arango", "Target", string(JobStateToDo)},
		{"arango", "Target", string(JobStatePending)},
		{"arango", "Target", string(JobStateFinished)},
		{"arango", "Target", string(JobStateFailed)},
	}

	ErrJobNotFound = errors.New("job not found")
	ErrJobFailed   = errors.New("job failed")
)

// getJobStatus gets the status of the job.
func getJobStatus(ctx context.Context, jobID string, agencyClient agency.Agency) (JobStatus, error) {
	for _, keyPrefix := range agencyJobStateKeyPrefixes {
		var job JobStatus

		key := keyPrefix.CreateSubKey(jobID)
		if err := agencyClient.ReadKey(ctx, key, &job); err == nil {
			job.state = JobState(keyPrefix[len(keyPrefix)-1])
			return job, nil
		} else if agency.IsKeyNotFound(err) {
			continue
		} else {
			return JobStatus{}, err
		}
	}

	return JobStatus{}, ErrJobNotFound
}

// WaitForFinishedJob waits for the job to be finished until context is canceled.
// If the job fails the error ErrJobFailed is returned.
func WaitForFinishedJob(progress actions.Progressor, ctx context.Context, jobID string, agencyClient agency.Agency) error {
	for {
		job, err := getJobStatus(ctx, jobID, agencyClient)
		if err != nil {
			return err
		}

		if job.state == JobStateFinished {
			return nil
		}

		if job.state == JobStateFailed {
			return errors.Wrapf(ErrJobFailed, " with the reason %s", job.Reason)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second * 2): // TODO memory leak
			progress.Progress(fmt.Sprintf("Job %s of type %s for server %s in state %s: %s", job.JobID, job.Type, job.Server, string(job.state), job.Reason))

		}
	}
}
