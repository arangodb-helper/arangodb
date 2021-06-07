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
// Author Adam Janikowski
//

package test

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

var (
	loggerMutex sync.Mutex
	loggers     = map[*testing.T]Logger{}
)

func cleanLogger(t *testing.T) {
	loggerMutex.Lock()
	defer loggerMutex.Unlock()

	delete(loggers, t)
}

func getLogger(parent *logger, t *testing.T) Logger {
	loggerMutex.Lock()
	defer loggerMutex.Unlock()

	if l, ok := loggers[t]; ok {
		return l
	}

	l := &logger{
		start:  time.Now(),
		t:      t,
		parent: parent,
	}

	loggers[t] = l
	return l
}

type Logger interface {
	Log(format string, args ...interface{})

	SubLogger(t *testing.T) Logger
	Checkpoint() Logger

	Clean()
}

type logger struct {
	start time.Time
	t     *testing.T

	parent *logger
}

func (l *logger) Checkpoint() Logger {
	return &logger{
		start:  time.Now(),
		t:      l.t,
		parent: l,
	}
}

func (l *logger) Clean() {
	cleanLogger(l.t)
}

func (l *logger) getParent() *logger {
	if l == nil || l.parent == nil {
		return nil
	}

	if p := l.parent.getParent(); p == nil {
		return l
	} else {
		return p
	}
}

func (l *logger) Log(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if p := l.getParent(); p == nil {
		line = fmt.Sprintf("Started: %s > %s", time.Now().Sub(l.start), line)
	} else {
		line = fmt.Sprintf("Started: %s, In Test: %s > %s", time.Now().Sub(p.start).String(), time.Now().Sub(l.start).String(), line)
	}

	l.t.Log(line)
	println(line)
}

func (l *logger) SubLogger(t *testing.T) Logger {
	return getLogger(l, t)
}

func GetLogger(t *testing.T) Logger {
	return getLogger(nil, t)
}
