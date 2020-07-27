//
// DISCLAIMER
//
// Copyright 2017 ArangoDB GmbH, Cologne, Germany
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
// Author Ewout Prangsma
//

package logging

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

var (
	// The defaultLevels list is used during development to increase the
	// default level for components that we care a little less about.
	defaultLevels = map[string]string{
		"cluster.update": "info",
		//"master.sync.status": "info",
		"master.workers": "info",
		//"mq.kafka":           "info",
		//"mq.kafka.driver": "info",
		"mq.nats":          "info",
		"mq.direct.remote": "info",
		"server.requests":  "info",
		//"task.receive-inventory": "info",
		"worker.directMQ":     "debug",
		"worker.registration": "info",
	}
)

// Service exposes the interfaces for a logger service
// that supports different loggers with different levels.
type Service interface {
	// MustGetLogger creates a logger with given name.
	MustGetLogger(name string) zerolog.Logger
	// MustSetLevel sets the log level for the component with given name to given level.
	MustSetLevel(name, level string)
	// RotateLogFiles re-opens log file writer.
	RotateLogFiles()
}

// loggingService implements Service
type loggingService struct {
	mutex        sync.Mutex
	rootLog      zerolog.Logger
	defaultLevel zerolog.Level
	levels       map[string]zerolog.Level
	rotate       func()
}

type LoggerOutputOptions struct {
	Color   bool   // Produce colored logs
	JSON    bool   // Project JSON messages
	Stderr  bool   // Write logs to stderr
	LogFile string // Path of file to write to
}

func configureLogger(lg zerolog.ConsoleWriter) zerolog.ConsoleWriter {
	lg.TimeFormat = "2006-01-02T15:04:05-07:00"

	lg.FormatLevel = func(i interface{}) string {
		return fmt.Sprintf("|%s|", strings.ToUpper(fmt.Sprintf("%s", i)))
	}

	return lg
}

// NewRootLogger creates a new zerolog logger with default settings.
func NewRootLogger(options LoggerOutputOptions) (zerolog.Logger, func()) {
	var writers []io.Writer
	var errors []error
	var rotate func()
	if options.LogFile != "" {
		fileWriter, err := newRotatingWriter(options.LogFile)
		if err != nil {
			errors = append(errors, err)
			options.Stderr = true
		} else {
			rotate = func() { fileWriter.Rotate() }
			writer := io.Writer(fileWriter)
			if !options.JSON {
				writer = configureLogger(zerolog.ConsoleWriter{
					Out:     fileWriter,
					NoColor: true,
				})
			}
			writers = append(writers, writer)
		}
	}
	if options.Stderr {
		writer := io.Writer(os.Stderr)
		if !options.JSON {
			writer = configureLogger(zerolog.ConsoleWriter{
				Out:     writer,
				NoColor: !options.Color,
			})
		}
		writers = append(writers, writer)
	}

	var writer io.Writer
	switch len(writers) {
	case 0:
		writer = ioutil.Discard
	case 1:
		writer = writers[0]
	default:
		writer = io.MultiWriter(writers...)
	}

	l := zerolog.New(writer).With().Timestamp().Logger()
	if len(errors) > 0 {
		for _, err := range errors {
			l.Error().Msg(err.Error())
		}
		l.Fatal().Msg("Failed to initialize logging")
	}
	return l, rotate
}

// NewService creates a new Service.
func NewService(defaultLevel string, options LoggerOutputOptions) (Service, error) {
	l, err := stringToLevel(defaultLevel)
	if err != nil {
		return nil, maskAny(err)
	}
	rootLog, rotate := NewRootLogger(options)
	s := &loggingService{
		rootLog:      rootLog,
		defaultLevel: l,
		levels:       make(map[string]zerolog.Level),
		rotate:       rotate,
	}
	for k, v := range defaultLevels {
		s.MustSetLevel(k, v)
	}
	return s, nil
}

// MustGetLogger creates a logger with given name
func (s *loggingService) MustGetLogger(name string) zerolog.Logger {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	level, found := s.levels[name]
	if !found {
		level = s.defaultLevel
	}
	return s.rootLog.With().Str("component", name).Logger().Level(level)
}

// MustSetLevel sets the log level for the component with given name to given level.
func (s *loggingService) MustSetLevel(name, level string) {
	l, err := stringToLevel(level)
	if err != nil {
		panic(err)
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.levels[name] = l
}

// RotateLogFiles re-opens log file writer.
func (s *loggingService) RotateLogFiles() {
	if s.rotate != nil {
		s.rotate()
	}
}

// stringToLevel converts a level string to a zerolog level
func stringToLevel(l string) (zerolog.Level, error) {
	switch strings.ToLower(l) {
	case "debug":
		return zerolog.DebugLevel, nil
	case "info":
		return zerolog.InfoLevel, nil
	case "warn", "warning":
		return zerolog.WarnLevel, nil
	case "error":
		return zerolog.ErrorLevel, nil
	case "fatal":
		return zerolog.FatalLevel, nil
	case "panic":
		return zerolog.PanicLevel, nil
	}
	return zerolog.InfoLevel, fmt.Errorf("Unknown log level '%s'", l)
}
