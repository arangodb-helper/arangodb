//
// DISCLAIMER
//
// Copyright 2018-2024 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"context"

	"github.com/arangodb-helper/arangodb/pkg/arangodb"
	"github.com/arangodb/go-driver"
)

// DatabaseVersion returns the version of the `arangod` binary that is being
// used by this starter.
func (s *Service) DatabaseVersion(ctx context.Context) (driver.Version, bool, error) {
	return arangodb.DatabaseVersion(ctx, s.log, s.cfg.ArangodPath, s.runner)
}
