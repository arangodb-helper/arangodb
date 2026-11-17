//go:build windows

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

// fileIdentity returns the volume serial number and file index of the file
// at path.
func fileIdentity(path string) (logFileID, bool) {
	_, id, known, err := fileSizeAndIdentity(path)
	return id, known && err == nil
}

// fileSizeAndIdentity returns the size, volume serial number and file index
// of the file at path, all read from one open handle, so they describe the
// same file even when the log is rotated meanwhile. os.FileInfo does not
// carry the identity on Windows, so the file is opened for metadata only.
// All share modes are allowed, so this never blocks the rename done by log
// rotation.
func fileSizeAndIdentity(path string) (int64, logFileID, bool, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, logFileID{}, false, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := syscall.CreateFile(p, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, logFileID{}, false, &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return 0, logFileID{}, false, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	size := int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow)
	return size, logFileID{
		volume: uint64(info.VolumeSerialNumber),
		index:  uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow),
	}, true, nil
}
