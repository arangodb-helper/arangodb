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
	"context"

	"github.com/rs/zerolog"
)

type ContextProvider func(ctx context.Context, c zerolog.Context) zerolog.Context

var (
	contextProviders []ContextProvider
)

// RegisterContextProvider records a given context provider.
// This method is supposed to be called in an `init` method.
func RegisterContextProvider(p ContextProvider) {
	contextProviders = append(contextProviders, p)
}

// With creates a zerolog Context that contains all information available in the
// given context.
func With(ctx context.Context, log zerolog.Logger) zerolog.Context {
	c := log.With()
	for _, p := range contextProviders {
		c = p(ctx, c)
	}
	return c
}
