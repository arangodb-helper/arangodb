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
	"strconv"
	"strings"
)

type ImagePullPolicy string

const (
	// ImagePullPolicyAlways is a policy to always pull the image
	ImagePullPolicyAlways ImagePullPolicy = "Always"
	// ImagePullPolicyIfNotPresent is a policy to only pull the image when it does not exist locally
	ImagePullPolicyIfNotPresent ImagePullPolicy = "IfNotPresent"
	// ImagePullPolicyNever is a policy to never pull the image
	ImagePullPolicyNever ImagePullPolicy = "Never"
)

// ParseImagePullPolicy parses a string into an image pull policy
func ParseImagePullPolicy(s, image string) (ImagePullPolicy, error) {
	switch strings.ToLower(s) {
	case "":
		if strings.Contains(image, ":latest") {
			return ImagePullPolicyAlways, nil
		} else {
			return ImagePullPolicyIfNotPresent, nil
		}
	case "always":
		return ImagePullPolicyAlways, nil
	case "ifnotpresent":
		return ImagePullPolicyIfNotPresent, nil
	case "never":
		return ImagePullPolicyNever, nil
	default:
		if boolValue, err := strconv.ParseBool(s); err != nil {
			return ImagePullPolicyIfNotPresent, maskAny(err)
		} else if boolValue {
			return ImagePullPolicyAlways, nil
		} else {
			return ImagePullPolicyNever, nil
		}

	}
}
