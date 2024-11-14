//
// DISCLAIMER
//
// Copyright 2020-2024 ArangoDB GmbH, Cologne, Germany
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
	"encoding/json"
	"net/http"
	"time"

	"github.com/arangodb-helper/arangodb/pkg/api"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/features"
	client "github.com/arangodb-helper/arangodb/service/clients"
)

// localInventory returns inventory details.
func (s *httpServer) localInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	_, p, mode := s.context.ClusterConfig()
	i, err := s.localInventoryObject(p, mode)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	d, err := json.Marshal(i)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	w.Header().Set(contentTypeHeader, contentTypeJSON)
	w.Write(d)
}

// clusterInventory returns inventory details.
func (s *httpServer) clusterInventory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	c, err := s.clusterInventoryObject()

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}

	d, err := json.Marshal(c)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set(contentTypeHeader, contentTypeJSON)
	w.Write(d)
}

func (s *httpServer) clusterInventoryObject() (*api.ClusterInventory, error) {
	c := api.ClusterInventory{
		Peers: map[string]api.Inventory{},
	}

	peers, _, mode := s.context.ClusterConfig()
	for _, p := range peers.AllPeers {
		i, err := s.localInventoryObject(&p, mode)

		if err != nil {
			return nil, err
		}

		c.Peers[p.ID] = *i
	}

	for _, p := range c.Peers {
		if p.Error != nil {
			c.Error = p.Error
			continue
		}
	}

	return &c, nil
}

func (s *httpServer) localInventoryObject(p *Peer, mode ServiceMode) (*api.Inventory, error) {
	m := &api.Inventory{
		Members: map[definitions.ServerType]api.MemberInventory{},
	}

	if err := s.forEachServerType(mode, p, func(mode ServiceMode, p *Peer, t definitions.ServerType) error {
		s.localInventoryMemberAdd(m, p, t)
		return nil
	}); err != nil {
		return nil, err
	}

	for _, member := range m.Members {
		if member.Error != nil {
			m.Error = member.Error
			continue
		}
	}

	return m, nil
}

func (s *httpServer) localInventoryMemberAdd(inv *api.Inventory, p *Peer, t definitions.ServerType) {
	s.localInventoryMemberAddWithCondition(inv, p, t, func() bool {
		return true
	})
}

func (s *httpServer) localInventoryMemberAddWithCondition(inv *api.Inventory, p *Peer, t definitions.ServerType, condition func() bool) {
	if !condition() {
		return
	}

	i, err := s.localInventoryMember(p, t)
	if err != nil {
		inv.Members[t] = api.MemberInventory{
			Error: api.NewError(err),
		}
		return
	}

	inv.Members[t] = i
	return
}

func (s *httpServer) localInventoryMember(p *Peer, t definitions.ServerType) (api.MemberInventory, error) {
	c, err := p.CreateClient(s.context, t)
	if err != nil {
		return api.MemberInventory{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	i := api.MemberInventory{}

	v, err := c.Version(ctx)
	if err != nil {
		return i, err
	}
	i.Version = v

	if features.JWTRotation().Enabled(features.Version{
		Enterprise: v.IsEnterprise(),
		Version:    v.Version,
	}) {
		i.Hashes = &api.MemberHashes{}

		ic := client.NewClient(c)

		jwt, err := ic.GetJWT(ctx)
		if err != nil {
			return i, err
		}

		i.Hashes.JWT = jwt.Result

	}

	return i, nil
}

func (s *httpServer) forEachServerType(m ServiceMode, p *Peer, action func(m ServiceMode, p *Peer, t definitions.ServerType) error) error {
	switch m {
	case ServiceModeSingle:
		if err := action(m, p, definitions.ServerTypeSingle); err != nil {
			return err
		}
	case ServiceModeCluster:
		if p.HasDBServer() {
			if err := action(m, p, definitions.ServerTypeDBServer); err != nil {
				return err
			}
		}
		if p.HasCoordinator() {
			if err := action(m, p, definitions.ServerTypeCoordinator); err != nil {
				return err
			}
		}
		if p.HasAgent() {
			if err := action(m, p, definitions.ServerTypeAgent); err != nil {
				return err
			}
		}
	}
	return nil
}
