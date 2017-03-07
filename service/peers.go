package service

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Peer struct {
	ID         string // Unique of of the peer
	Address    string // IP address of arangodb peer server
	Port       int    // Port number of arangodb peer server
	PortOffset int    // Offset to add to base ports for the various servers (agent, coordinator, dbserver)
	DataDir    string // Directory holding my data
	HasAgent   bool   // If set, this peer is running an agent
}

// CreateStarterURL creates a URL to the relative path to the starter on this peer.
func (p Peer) CreateStarterURL(relPath string) string {
	addr := net.JoinHostPort(p.Address, strconv.Itoa(p.Port))
	relPath = strings.TrimPrefix(relPath, "/")
	return fmt.Sprintf("http://%s/%s", addr, relPath)
}

// Peer information.
// When this type (or any of the types used in here) is changed, increase `SetupConfigVersion`.
type peers struct {
	Peers      []Peer // All peers (index 0 is reserver for the master)
	AgencySize int    // Number of agents
}

// PeerByID returns a peer with given id & true, or false if not found.
func (p peers) PeerByID(id string) (Peer, bool) {
	for _, x := range p.Peers {
		if x.ID == id {
			return x, true
		}
	}
	return Peer{}, false
}

// RemovePeerByID removes the peer with given ID.
func (p *peers) RemovePeerByID(id string) bool {
	newPeers := make([]Peer, 0, len(p.Peers))
	found := false
	for _, x := range p.Peers {
		if x.ID != id {
			newPeers = append(newPeers, x)
		} else {
			found = true
		}
	}
	p.Peers = newPeers
	return found
}

// IDs returns the IDs of all peers.
func (p peers) IDs() []string {
	list := make([]string, 0, len(p.Peers))
	for _, x := range p.Peers {
		list = append(list, x.ID)
	}
	return list
}

// GetFreePortOffset returns the first unallocated port offset.
func (p peers) GetFreePortOffset(peerAddress string, allPortOffsetsUnique bool) int {
	portOffset := 0
	for {
		found := false
		for _, p := range p.Peers {
			if p.PortOffset == portOffset {
				if allPortOffsetsUnique || p.Address == peerAddress {
					found = true
					break
				}
			}
		}
		if !found {
			return portOffset
		}
		portOffset += portOffsetIncrement
	}
}
