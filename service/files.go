//
// DISCLAIMER
//
// Copyright 2020 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
)

func FilterFiles(f []os.FileInfo, filter func(f os.FileInfo) bool) []os.FileInfo {
	localFiles := make([]os.FileInfo, 0, len(f))

	for _, file := range f {
		if !filter(file) {
			continue
		}

		localFiles = append(localFiles, file)
	}

	return localFiles
}

func FilterOnlyFiles(f os.FileInfo) bool {
	return f.Mode().IsRegular()
}

func Sha256sum(d []byte) string {
	cleanData := []byte(strings.TrimSpace(string(d)))
	return fmt.Sprintf("%0x", sha256.Sum256(cleanData))
}
