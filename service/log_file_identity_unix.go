//go:build unix

//
// DISCLAIMER
//
// Copyright 2026 ArangoDB GmbH, Cologne, Germany
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

package service

import (
	"os"
	"syscall"
)

// fileIdentity returns the device and inode of the file at path.
func fileIdentity(path string) (logFileID, bool) {
	_, id, known, err := fileSizeAndIdentity(path)
	return id, known && err == nil
}

// fileSizeAndIdentity returns the size and the device and inode of the file
// at path from a single stat, so both describe the same file even when the
// log is rotated meanwhile.
func fileSizeAndIdentity(path string) (int64, logFileID, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, logFileID{}, false, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return info.Size(), logFileID{}, false, nil
	}
	return info.Size(), logFileID{volume: uint64(stat.Dev), index: uint64(stat.Ino)}, true, nil
}

// openedFileIdentity returns the device and inode of an open file, so a
// reader can check which file it actually opened.
func openedFileIdentity(f *os.File) (logFileID, bool) {
	info, err := f.Stat()
	if err != nil {
		return logFileID{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return logFileID{}, false
	}
	return logFileID{volume: uint64(stat.Dev), index: uint64(stat.Ino)}, true
}
