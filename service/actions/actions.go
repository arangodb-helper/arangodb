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
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
)

// Registry allows to register new actions.
type Registry struct {
	// mutex protects the internal fields of this structure.
	mutex sync.RWMutex
	// actions holds already registered actions.
	actions map[string]Action
}

var registry Registry

// ActionTypes is the list of ActionType
type ActionTypes []ActionType

// Contains returns true if requested action is on list
func (a ActionTypes) Contains(t ActionType) bool {
	for _, b := range a {
		if b == t {
			return true
		}
	}

	return false
}

// ActionType keeps type of action
type ActionType string

// String returns string value of ActionType
func (a ActionType) String() string {
	return string(a)
}

const (
	// ActionTypeAll filter which run all action types
	ActionTypeAll ActionType = "All"
	// ActionTypePreStop PreStop Action Type
	ActionTypePreStop ActionType = "PreStop"
)

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
	Progress(message string) error
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

	registry.mutex.Lock()
	defer registry.mutex.Unlock()

	if registry.actions == nil {
		registry.actions = make(map[string]Action)
	}

	registry.actions[action.Name()] = action
}

// GetActions returns actions which are already registered.
func (r *Registry) GetActions() map[string]Action {
	registry.mutex.RLock()
	defer registry.mutex.RUnlock()

	return registry.actions
}

// StartAction starts actions based on type if actionType is on the limit list
func StartLimitedAction(logger zerolog.Logger, actionType ActionType, serverType definitions.ServerType, limit ActionTypes) {
	if !limit.Contains(actionType) && !limit.Contains(ActionTypeAll) {
		return
	}

	StartAction(logger, actionType, serverType)
}

// StartAction starts actions based on type
func StartAction(logger zerolog.Logger, actionType ActionType, serverType definitions.ServerType) {
	switch actionType {
	case ActionTypePreStop:
		log := logger.With().Str("action", actionType.String()).Logger()
		log.Info().Msgf("Starting actions")
		StartPreStopActions(log, serverType, &ProgressLog{
			LoggerOriginal: log,
			logger:         log,
		})
	}
}

// StartPreStopActions runs registered pre stop actions.
func StartPreStopActions(logger zerolog.Logger, serverType definitions.ServerType, progress Progressor) {
	if progress == nil {
		progress = &ProgressLog{
			LoggerOriginal: logger,
			logger:         logger,
		}
	}

	for _, anyAction := range registry.GetActions() {
		preStopAction, ok := anyAction.(ActionPreStop)
		if !ok {
			continue
		}

		if !preStopAction.Condition(serverType) {
			continue
		}

		logger.Info().Str("Name", anyAction.Name()).Msgf("Starting Action")

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
func (p *ProgressLog) Progress(message string) error {
	p.logger.Info().Str("progress", message).Msg("Action progress")
	return errors.New(message)
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
func (p ProgressEmpty) Progress(message string) error {
	return errors.New(message)
}
