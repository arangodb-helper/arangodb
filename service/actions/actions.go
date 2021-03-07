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

// Progressor describes what to do when the specific moment of action occurs.
type Progressor interface {
	// Started is launched when the action starts.
	Started(actionName string)
	// Failed is launched when the action fails.
	Failed(err error)
	// Finished is launched when the action finishes.
	Finished()
	// Progress is launched whenever some progress occurs for the specific action.
	Progress(message string)
}

// ActionPreStop describes how pre stop actions should be started.
type ActionPreStop interface {
	Action
	// PreStop runs action before server is stopped.
	PreStop(ctx context.Context, progress Progressor) error
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
func StartPreStopActions(serverType definitions.ServerType, progress Progressor) {
	if progress == nil {
		progress = &ProgressEmpty{}
	}

	for _, anyAction := range actions {
		preStopAction, ok := anyAction.(ActionPreStop)
		if !ok {
			continue
		}

		if !preStopAction.Condition(serverType) {
			continue
		}

		ctxAction, cancelAction := context.WithTimeout(context.Background(), preStopAction.Timeout())

		progress.Started(preStopAction.Name())
		if err := preStopAction.PreStop(ctxAction, progress); err != nil {
			progress.Failed(err)
		} else {
			progress.Finished()
		}

		cancelAction()
	}
}

// ProgressLog describes simple logger for actions.
type ProgressLog struct {
	LoggerOriginal zerolog.Logger
	logger         zerolog.Logger
}

// Started is launched when the action starts.
func (p *ProgressLog) Started(actionName string) {
	p.logger = p.LoggerOriginal.With().Str("name", actionName).Logger()
	p.logger.Info().Msg("Action started")
}

// Failed is launched when the action fails.
func (p *ProgressLog) Failed(err error) {
	p.logger.Error().Err(err).Msg("Action failed")
}

// Finished is launched when the action finishes.
func (p *ProgressLog) Finished() {
	p.logger.Info().Msg("Action finished")
}

// Progress is launched whenever some progress occurs for the specific action.
func (p *ProgressLog) Progress(message string) {
	p.logger.Info().Str("message", message).Msg("Action progress")
}

// ProgressEmpty describes empty progress for the actions.
type ProgressEmpty struct{}

// Started is launched when the action starts.
func (p ProgressEmpty) Started(_ string) {}

// Failed is launched when the action fails.
func (p ProgressEmpty) Failed(_ error) {}

// Finished is launched when the action finishes.
func (p ProgressEmpty) Finished() {}

// Progress is launched whenever some progress occurs for the specific action.
func (p ProgressEmpty) Progress(_ string) {}
