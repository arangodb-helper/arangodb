package service

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"sync"
)

const (
	// SetupConfigVersion is the semantic version of the process that created this.
	// If the structure of SetupConfigFile (or any underlying fields) or its semantics change, you must increase this version.
	SetupConfigVersion = "0.2.0"
	setupFileName      = "setup.json"
)

// SetupConfigFile is the JSON structure stored in the setup file of this process.
type SetupConfigFile struct {
	Version          string `json:"version"` // Version of the process that created this. If the structure or semantics changed, you must increase this version.
	ID               string `json:"id"`      // My unique peer ID
	Peers            peers  `json:"peers"`
	StartLocalSlaves bool   `json:"start-local-slaves,omitempty"`
}

// saveSetup saves the current peer configuration to disk.
func (s *Service) saveSetup() error {
	cfg := SetupConfigFile{
		Version:          SetupConfigVersion,
		ID:               s.ID,
		Peers:            s.myPeers,
		StartLocalSlaves: s.StartLocalSlaves,
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		s.log.Errorf("Cannot serialize config: %#v", err)
		return maskAny(err)
	}
	if err := ioutil.WriteFile(filepath.Join(s.DataDir, setupFileName), b, 0644); err != nil {
		s.log.Errorf("Error writing setup: %#v", err)
		return maskAny(err)
	}
	return nil
}

// relaunch tries to read a setup.json config file and relaunch when that file exists and is valid.
// Returns true on relaunch or false to continue with a fresh start.
func (s *Service) relaunch(runner Runner) bool {
	// Is this a new start or a restart?
	if setupContent, err := ioutil.ReadFile(filepath.Join(s.DataDir, setupFileName)); err == nil {
		// Could read file
		var cfg SetupConfigFile
		if err := json.Unmarshal(setupContent, &cfg); err == nil {
			if cfg.Version == SetupConfigVersion {
				s.myPeers = cfg.Peers
				s.ID = cfg.ID
				s.AgencySize = s.myPeers.AgencySize
				s.log.Infof("Relaunching service with id '%s' on %s:%d...", s.ID, s.OwnAddress, s.announcePort)
				s.startHTTPServer()
				wg := &sync.WaitGroup{}
				if cfg.StartLocalSlaves {
					s.startLocalSlaves(wg, cfg.Peers.Peers)
				}
				s.startRunning(runner)
				wg.Wait()
				return true
			}
			s.log.Warningf("%s is outdated. Starting fresh...", setupFileName)
		} else {
			s.log.Warningf("Failed to unmarshal existing %s: %#v", setupFileName, err)
		}
	}
	return false
}
