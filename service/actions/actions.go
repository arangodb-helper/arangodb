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

package actions

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

var actions map[string]Action

// Action describes how some actions should be started.
type Action interface {
	// Name returns name of the action.
	Name() string
	// Timeout returns how long it should wait for the action to be finished.
	Timeout() time.Duration
	// Condition returns true if this action should be launched.
	Condition(serverType definitions.ServerType) bool
}

// ActionPreStop describes how pre stop actions should be started.
type ActionPreStop interface {
	Action
	// PreStop runs action before server is stopped.
	PreStop(ctx context.Context) error
}

// RegisterAction registers a new action if it does not exist.
func RegisterAction(action Action) {
	if action == nil {
		return
	}

	if actions == nil {
		actions = make(map[string]Action)
	}

	actions[action.Name()] = action
}

// StartPreStopActions runs registered pre stop actions.
func StartPreStopActions(log zerolog.Logger, serverType definitions.ServerType) {
	for _, anyAction := range actions {
		preStopAction, ok := anyAction.(ActionPreStop)
		if !ok {
			continue
		}

		if !preStopAction.Condition(serverType) {
			continue
		}

		ctxAction, cancelAction := context.WithTimeout(context.Background(), preStopAction.Timeout())

		logAction := log.With().Str("name", preStopAction.Name()).Logger()
		logAction.Info().Msg("Action started")
		if err := preStopAction.PreStop(ctxAction); err != nil {
			logAction.Info().Msg("Action failed")
		} else {
			logAction.Info().Msg("Action finished")
		}

		cancelAction()
	}
}
