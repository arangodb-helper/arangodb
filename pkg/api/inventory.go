//
// DISCLAIMER
//
// Copyright 2020 ArangoDB GmbH, Cologne, Germany
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

package api

import (
	driver "github.com/arangodb/go-driver/v2/arangodb"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	client "github.com/arangodb-helper/arangodb/service/clients"
)

type ClusterInventory struct {
	Peers map[string]Inventory `json:"peers,omitempty"`

	*Error `json:",inline"`
}

type Inventory struct {
	Members map[definitions.ServerType]MemberInventory `json:"members,omitempty"`

	*Error `json:",inline"`
}

type MemberInventory struct {
	Version driver.VersionInfo `json:"version"`

	Hashes *MemberHashes `json:"hashes,omitempty"`

	*Error `json:",inline"`
}

type MemberHashes struct {
	JWT client.JWTDetailsResult `json:"jwt"`
}
