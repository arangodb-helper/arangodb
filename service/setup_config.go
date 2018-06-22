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

package service

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/coreos/go-semver/semver"
	"github.com/rs/zerolog"
)

var (
	// SetupConfigVersion is the semantic version of the process that created this.
	// If the structure of SetupConfigFile (or any underlying fields) or its semantics change, you must increase this version.
	setupConfigVersion    = *semver.New("0.2.2") // Current version
	minSetupConfigVersion = *semver.New("0.2.1") // Minimum version that we can support
)

const (
	setupFileName = "setup.json"
)

// SetupConfigFile is the JSON structure stored in the setup file of this process.
type SetupConfigFile struct {
	Version          string        `json:"version"` // Version of the process that created this. If the structure or semantics changed, you must increase this version.
	ID               string        `json:"id"`      // My unique peer ID
	Peers            ClusterConfig `json:"peers"`
	StartLocalSlaves bool          `json:"start-local-slaves,omitempty"`
	Mode             ServiceMode   `json:"mode,omitempty"` // Starter mode (cluster|single)
	SslKeyFile       string        `json:"ssl-keyfile,omitempty"`
	JwtSecret        string        `json:"jwt-secret,omitempty"`
}

// saveSetup saves the current peer configuration to disk.
func (s *Service) saveSetup() error {
	cfg := SetupConfigFile{
		Version:          setupConfigVersion.String(),
		ID:               s.id,
		Peers:            s.myPeers,
		StartLocalSlaves: s.startedLocalSlaves,
		Mode:             s.mode,
		SslKeyFile:       s.sslKeyFile,
		JwtSecret:        s.jwtSecret,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		s.log.Error().Err(err).Msg("Cannot serialize config")
		return maskAny(err)
	}
	if err := ioutil.WriteFile(filepath.Join(s.cfg.DataDir, setupFileName), b, 0644); err != nil {
		s.log.Error().Err(err).Msg("Error writing setup")
		return maskAny(err)
	}
	return nil
}

// ReadSetupConfig tries to read a setup.json config file and relaunch when that file exists and is valid.
// Returns true on relaunch or false to continue with a fresh start.
func ReadSetupConfig(log zerolog.Logger, dataDir string, bsCfg BootstrapConfig) (BootstrapConfig, ClusterConfig, bool, error) {
	// Is this a new start or a restart?
	setupContent, err := ioutil.ReadFile(filepath.Join(dataDir, setupFileName))
	if err != nil {
		return bsCfg, ClusterConfig{}, false, nil
	}
	// Could read file
	var cfg SetupConfigFile
	if err := json.Unmarshal(setupContent, &cfg); err != nil {
		log.Warn().Err(err).Msgf("Failed to unmarshal existing %s", setupFileName)
		return bsCfg, ClusterConfig{}, false, nil
	}
	// Parse version
	version, err := semver.NewVersion(cfg.Version)
	if err != nil {
		log.Warn().Err(err).Msgf("Failed to parse version '%s' in %s", cfg.Version, setupFileName)
		return bsCfg, ClusterConfig{}, false, nil
	}

	// If version recent enough?
	if version.LessThan(minSetupConfigVersion) {
		log.Warn().Msgf("%s is outdated (version %s). Starting fresh...", setupFileName, cfg.Version)
		return bsCfg, ClusterConfig{}, false, nil
	}

	// Reload data from config
	bsCfg.ID = cfg.ID
	if cfg.Mode != "" {
		bsCfg.Mode = cfg.Mode
	}
	bsCfg.StartLocalSlaves = cfg.StartLocalSlaves
	if cfg.SslKeyFile != "" {
		bsCfg.SslKeyFile = cfg.SslKeyFile
	}
	if cfg.JwtSecret != "" {
		bsCfg.JwtSecret = cfg.JwtSecret
	}
	bsCfg.AgencySize = cfg.Peers.AgencySize

	return bsCfg, cfg.Peers, true, nil
}

// RemoveSetupConfig tries to remove a setup.json config file.
func RemoveSetupConfig(log zerolog.Logger, dataDir string) error {
	path := filepath.Join(dataDir, setupFileName)
	if _, err := os.Stat(path); err == nil {
		log.Info().Msgf("Removing starter config %s", path)
		if err := os.Remove(path); err != nil {
			return maskAny(err)
		}
	}
	return nil
}
