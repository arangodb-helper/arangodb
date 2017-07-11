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

// URLSchemes contains URL schemes for browser & Arango Shell.
type URLSchemes struct {
	Browser  string // Scheme for use in a webbrowser
	Arangod  string // URL Scheme for use in Arangod[.conf]
	ArangoSH string // URL Scheme for use in ArangoSH
}

// NewURLSchemes creates initialized schemes depending on isSecure flag.
func NewURLSchemes(isSecure bool) URLSchemes {
	if isSecure {
		return URLSchemes{"https", "ssl", "ssl"}
	}
	return URLSchemes{"http", "tcp", "tcp"}
}
