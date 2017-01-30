package service

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

const (
	// Version of the process that created this. If the structure or semantics changed, you must increase this version.
	SetupConfigVersion = "0.1.0"
	setupFileName      = "setup.json"
)

// SetupConfigFile is the JSON structure stored in the setup file of this process.
type SetupConfigFile struct {
	Version string `json:"version"` // Version of the process that created this. If the structure or semantics changed, you must increase this version.
	Peers   peers  `json:"peers"`
}

// saveSetup saves the current peer configuration to disk.
func (s *Service) saveSetup() error {
	cfg := SetupConfigFile{
		Version: SetupConfigVersion,
		Peers:   s.myPeers,
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
				s.AgencySize = s.myPeers.AgencySize
				s.log.Infof("Relaunching service on %s:%d...", s.OwnAddress, s.announcePort)
				s.startHTTPServer()
				s.startRunning(runner)
				return true
			}
			s.log.Warningf("%s is outdated. Starting fresh...", setupFileName)
		} else {
			s.log.Warningf("Failed to unmarshal existing %s: %#v", setupFileName, err)
		}
	}
	return false
}
