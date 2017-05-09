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
	"fmt"
	"net"
)

// GuessOwnAddress takes a "best guess" approach to find the IP address used to reach the host PC.
func GuessOwnAddress() (string, error) {
	intfs, err := net.Interfaces()
	if err != nil {
		return "", maskAny(err)
	}
	validIP4s := make([]net.IP, 0, 32)
	validIP6s := make([]net.IP, 0, 32)
	for _, intf := range intfs {
		if intf.Flags&net.FlagUp == 0 {
			continue
		}
		if intf.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := intf.Addrs()
		if err != nil {
			// Just ignore this interface in case we cannot get its addresses
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil {
				continue
			}
			if ip.IsLoopback() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ip.To4(); ip4 != nil {
				validIP4s = append(validIP4s, ip4)
			} else if ip6 := ip.To16(); ip6 != nil {
				validIP6s = append(validIP6s, ip6)
			}
		}
	}
	if len(validIP4s) > 0 {
		return validIP4s[0].String(), nil
	}
	if len(validIP6s) > 0 {
		return validIP6s[0].String(), nil
	}
	return "", fmt.Errorf("No suitable addresses found")
}
