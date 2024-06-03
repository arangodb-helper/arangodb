//
// DISCLAIMER
//
// Copyright 2017-2023 ArangoDB GmbH, Cologne, Germany
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

package logging

import (
	"io"
	goLog "log"
	"strings"

	"github.com/rs/zerolog"
)

// logAdapter is used as an adapter for standard go logged
type logAdapter zerolog.Logger

var (
	_ io.Writer = logAdapter{}
)

func (l logAdapter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if !strings.Contains(line, "TLS handshake error") {
		log := zerolog.Logger(l)
		log.Error().Msg(line)
	}
	return len(p), nil
}

func NewAdapter(l zerolog.Logger) *goLog.Logger {
	return goLog.New(logAdapter(l), "", 0)
}
