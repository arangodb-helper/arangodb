//
// DISCLAIMER
//
// Copyright 2020-2021 ArangoDB GmbH, Cologne, Germany
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
// Author Tomasz Mielech
//

package service

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path"

	"github.com/pkg/errors"

	"github.com/arangodb-helper/arangodb/pkg/api"
	"github.com/arangodb-helper/arangodb/pkg/definitions"
	rotateClient "github.com/arangodb-helper/arangodb/service/clients"
)

func newJWTManager(d string) jwtManager {
	return jwtManager{d}
}

type jwtManager struct {
	dir string
}

func (j jwtManager) setActive(i *api.ClusterInventory, sha string) error {
	tokens, err := j.tokens()
	if err != nil {
		return err
	}

	if i.Error != nil {
		return errors.Errorf("Unable to set active token if member is failed: %s", i.Error.Error)
	}

	data, ok := tokens[sha]
	if !ok {
		return errors.Errorf("Token not found locally")
	}

	for pname, p := range i.Peers {
		for mname, n := range p.Members {
			if n.Hashes == nil {
				return errors.Errorf("Unable to get hashes - probably not supported by server")
			}

			if n.Hashes.JWT.Active.GetSHA().Checksum() != sha && !n.Hashes.JWT.Passive.ContainsSha(sha) {
				return errors.Errorf("JWT Token %s is not installed on peer %s and member %s", sha, pname, mname)
			}
		}
	}

	if err := ioutil.WriteFile(path.Join(j.dir, definitions.ArangodJWTSecretActive), data, 0600); err != nil {
		return err
	}

	return nil
}

func (j jwtManager) add(d []byte) error {
	if err := ioutil.WriteFile(path.Join(j.dir, Sha256sum(d)), d, 0600); err != nil {
		return err
	}

	return nil
}

func (j jwtManager) remove(i *api.ClusterInventory, p *Peer, s string, d []byte) error {
	if i.Error != nil {
		return errors.Errorf("Unable to remove token if member is failed: %s", i.Error.Error)
	}

	for pname, p := range i.Peers {
		for mname, n := range p.Members {
			if n.Hashes == nil {
				return errors.Errorf("Unable to get hashes - probably not supported by server")
			}

			if n.Hashes.JWT.Active.GetSHA().Checksum() == Sha256sum(d) {
				return errors.Errorf("JWT token %s is active on peer %s and member %s", Sha256sum(d), pname, mname)
			}
		}
	}

	return os.Remove(path.Join(j.dir, s))
}

type tokens map[string][]byte

func (t tokens) containsData(d []byte) bool {
	s := Sha256sum(d)

	for _, data := range t {
		if Sha256sum(data) == s {
			return true
		}
	}

	return false
}

func (t tokens) getAny() (string, []byte, bool) {
	for k, v := range t {
		return k, v, true
	}

	return "", nil, false
}

func (j jwtManager) tokens() (tokens, error) {
	files, err := ioutil.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}

	files = FilterFiles(files, FilterOnlyFiles)

	m := map[string][]byte{}

	for _, f := range files {
		d, err := ioutil.ReadFile(path.Join(j.dir, f.Name()))
		if err != nil {
			return nil, err
		}

		m[f.Name()] = d
	}

	return m, nil
}

func (s *httpServer) registerJWTFunctions(m *http.ServeMux) {
	m.HandleFunc("/admin/jwt/activate", s.jwtActivate)
	m.HandleFunc("/admin/jwt/refresh", s.jwtRefresh)
}

func (s *httpServer) jwtActivate(w http.ResponseWriter, r *http.Request) {
	// Ensure that all secrets are compatible with local cache

	code, err := s.jwtActivateE(r)

	e := api.Empty{
		Error: api.NewError(err),
	}

	if err != nil {
		if code == 0 {
			code = http.StatusInternalServerError
		}
	} else {
		code = http.StatusOK
	}

	w.WriteHeader(code)

	m, err := json.Marshal(e)
	if err == nil {
		w.Write(m)
	}
}

func (s *httpServer) jwtActivateE(r *http.Request) (int, error) {
	// Ensure that all secrets are compatible with local cache

	if !s.context.DatabaseFeatures().GetJWTFolderOption() {
		return http.StatusNotImplemented, errors.Errorf("Only available in folder mode")
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		return http.StatusBadRequest, errors.Errorf("Token query param needs to be set")
	}

	i, err := s.clusterInventoryObject()
	if err != nil {
		return 0, err
	}

	switch r.Method {
	case http.MethodPost:
		s.log.Info().Msgf("Received JWT Refresh call")
		if err := s.synchronizeJWTOnMembers(i, token); err != nil {
			s.log.Warn().Err(err).Msgf("JWT Refresh call failed")
			return 0, err
		}
		s.log.Info().Msgf("JWT Refresh call done")

	default:
		return http.StatusMethodNotAllowed, errors.Errorf("Method not allowed")
	}

	return 0, nil
}

func (s *httpServer) jwtRefresh(w http.ResponseWriter, r *http.Request) {
	// Ensure that all secrets are compatible with local cache

	code, err := s.jwtRefreshE(r.Method)

	e := api.Empty{
		Error: api.NewError(err),
	}

	if err != nil {
		if code == 0 {
			code = http.StatusInternalServerError
		}
	} else {
		code = http.StatusOK
	}

	w.WriteHeader(code)

	m, err := json.Marshal(e)
	if err == nil {
		w.Write(m)
	}
}

func (s *httpServer) jwtRefreshE(method string) (int, error) {
	// Ensure that all secrets are compatible with local cache

	if !s.context.DatabaseFeatures().GetJWTFolderOption() {
		return http.StatusNotImplemented, errors.Errorf("Only available in folder mode")
	}

	i, err := s.clusterInventoryObject()
	if err != nil {
		return 0, err
	}

	switch method {
	case http.MethodPost:
		s.log.Info().Msgf("Received JWT Refresh call")
		if err := s.synchronizeJWTOnMembers(i, ""); err != nil {
			s.log.Warn().Err(err).Msgf("JWT Refresh call failed")
			return 0, err
		}
		s.log.Info().Msgf("JWT Refresh call done")
	default:
		return http.StatusMethodNotAllowed, errors.Errorf("Method not allowed")
	}

	return 0, nil
}

func (s *httpServer) synchronizeJWTOnMembers(ci *api.ClusterInventory, active string) error {
	_, p, mode := s.context.ClusterConfig()

	i := newJWTManager(s.context.GetLocalFolder())

	iTokens, err := i.tokens()
	if err != nil {
		return err
	}

	return s.forEachServerType(mode, p, func(m ServiceMode, p *Peer, t definitions.ServerType) error {
		d, err := s.context.serverHostDir(t)
		if err != nil {
			return err
		}

		f := newJWTManager(path.Join(d, definitions.ArangodJWTSecretFolderName))

		fTokens, err := f.tokens()
		if err != nil {
			return err
		}

		for _, iD := range iTokens {
			if _, ok := fTokens[Sha256sum(iD)]; !ok {
				if err := f.add(iD); err != nil {
					return err
				}
			}
		}

		for _, fD := range fTokens {
			fT := Sha256sum(fD)
			if !iTokens.containsData(fD) {
				if err := f.remove(ci, p, fT, fD); err != nil {
					return err
				}
			}
		}

		fTokens, err = f.tokens()
		if err != nil {
			return err
		}

		cActive, ok := fTokens[definitions.ArangodJWTSecretActive]

		if !ok {
			_, d, ok := fTokens.getAny()
			if !ok {
				return errors.Errorf("Unable to find any key in folder")
			}

			if err := f.setActive(ci, Sha256sum(d)); err != nil {
				return err
			}

			fTokens, err = f.tokens()
			if err != nil {
				return err
			}

			cActive, ok = fTokens[definitions.ArangodJWTSecretActive]
		}

		if active != "" && active != Sha256sum(cActive) {
			eActive, ok := fTokens[active]
			if !ok {
				return errors.Errorf("Unable to find key which needs to be activated")
			}

			if err := f.setActive(ci, Sha256sum(eActive)); err != nil {
				return err
			}

			fTokens, err = f.tokens()
			if err != nil {
				return err
			}

			cActive, ok = fTokens[definitions.ArangodJWTSecretActive]
		}

		client, err := p.CreateClient(s.context, t)
		if err != nil {
			return err
		}

		rClient := rotateClient.NewClient(client)

		jwt, err := rClient.RefreshJWT(context.Background())
		if err != nil {
			return err
		}

		if jwt.Result.Active.GetSHA().Checksum() != Sha256sum(cActive) {
			return errors.Errorf("Invalid active key")
		}

		if len(jwt.Result.Passive)+1 != len(fTokens) {
			return errors.Errorf("Invalid tokens length")
		}

		for t := range fTokens {
			if t == definitions.ArangodJWTSecretActive {
				continue
			}

			if !jwt.Result.Passive.ContainsSha(t) {
				return errors.Errorf("Checksum %s not found on server", t)
			}
		}

		return nil
	})
}
