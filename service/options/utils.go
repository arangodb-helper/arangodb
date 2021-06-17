//
// DISCLAIMER
//
// Copyright 2021 ArangoDB GmbH, Cologne, Germany
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

package options

import "strings"

// SectionName returns the name of the configuration section this option belongs to.
func SectionName(key string) string {
	return strings.SplitN(key, ".", 2)[0]
}

// SectionKey returns the name of this option within its configuration section.
func SectionKey(key string) string {
	parts := strings.SplitN(key, ".", 2)
	if len(parts) > 1 {
		return parts[1]
	}
	return ""
}

// FormattedOptionName returns the option ready to be used in a command line argument,
// prefixed with `--`.
func FormattedOptionName(key string) string {
	return "--" + key
}
