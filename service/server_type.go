package service

import "fmt"

// ServerType specifies the types of database servers.
type ServerType string

const (
	ServerTypeCoordinator = "coordinator"
	ServerTypeDBServer    = "dbserver"
	ServerTypeAgent       = "agent"
)

// String returns a string representation of the given ServerType.
func (s ServerType) String() string {
	return string(s)
}

// PortOffset returns the offset from a peer base port for the given type of server.
func (s ServerType) PortOffset() int {
	switch s {
	case ServerTypeCoordinator:
		return _portOffsetCoordinator
	case ServerTypeDBServer:
		return _portOffsetDBServer
	case ServerTypeAgent:
		return _portOffsetAgent
	default:
		panic(fmt.Sprintf("Unknown ServerType: %s", string(s)))
	}
}
