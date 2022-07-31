//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
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
//

// Code generated automatically. DO NOT EDIT.

package definitions

const (
	// EXIT_SUCCESS
	ArangoDExitSuccess = 0 // No error has occurred.
	// EXIT_FAILED
	ArangoDExitFailed = 1 // Will be returned when a general error occurred.
	// EXIT_CODE_RESOLVING_FAILED
	ArangoDExitCodeResolvingFailed = 2 // unspecified exit code
	// EXIT_INVALID_OPTION_NAME
	ArangoDExitInvalidOptionName = 3 // invalid/unknown startup option name was used
	// EXIT_INVALID_OPTION_VALUE
	ArangoDExitInvalidOptionValue = 4 // invalid startup option value was used
	// EXIT_BINARY_NOT_FOUND
	ArangoDExitBinaryNotFound = 5 // Will be returned if a referenced binary was not found
	// EXIT_CONFIG_NOT_FOUND
	ArangoDExitConfigNotFound = 6 // Will be returned if no valid configuration was found or its contents are structurally invalid
	// EXIT_UPGRADE_FAILED
	ArangoDExitUpgradeFailed = 10 // Will be returned when the database upgrade failed
	// EXIT_UPGRADE_REQUIRED
	ArangoDExitUpgradeRequired = 11 // Will be returned when a database upgrade is required
	// EXIT_DOWNGRADE_REQUIRED
	ArangoDExitDowngradeRequired = 12 // Will be returned when a database upgrade is required
	// EXIT_VERSION_CHECK_FAILED
	ArangoDExitVersionCheckFailed = 13 // Will be returned when there is a version mismatch
	// EXIT_ALREADY_RUNNING
	ArangoDExitAlreadyRunning = 20 // Will be returned when arangod is already running according to PID-file
	// EXIT_COULD_NOT_BIND_PORT
	ArangoDExitCouldNotBindPort = 21 // Will be returned when the configured tcp endpoint is already occupied by another process
	// EXIT_COULD_NOT_LOCK
	ArangoDExitCouldNotLock = 22 // Will be returned if another ArangoDB process is running, or the state can not be cleared
	// EXIT_RECOVERY
	ArangoDExitRecovery = 23 // Will be returned if the automatic database startup recovery fails
	// EXIT_DB_NOT_EMPTY
	ArangoDExitDbNotEmpty = 24 // Will be returned when commanding to initialize a non empty directory as database
	// EXIT_UNSUPPORTED_STORAGE_ENGINE
	ArangoDExitUnsupportedStorageEngine = 25 // Will be returned when trying to start with an unsupported storage engine
	// EXIT_ICU_INITIALIZATION_FAILED
	ArangoDExitIcuInitializationFailed = 26 // Will be returned if icudtl.dat is not found, of the wrong version or invalid. Check for an incorrectly set ICU_DATA environment variable
	// EXIT_TZDATA_INITIALIZATION_FAILED
	ArangoDExitTzdataInitializationFailed = 27 // Will be returned if tzdata is not found
	// EXIT_RESOURCES_TOO_LOW
	ArangoDExitResourcesTooLow = 28 // Will be returned if i.e. ulimit is too restrictive
)

var arangoDExitReason = map[int]string{
	// EXIT_SUCCESS
	ArangoDExitSuccess: "success",
	// EXIT_FAILED
	ArangoDExitFailed: "exit with error",
	// EXIT_CODE_RESOLVING_FAILED
	ArangoDExitCodeResolvingFailed: "exit code resolving failed",
	// EXIT_INVALID_OPTION_NAME
	ArangoDExitInvalidOptionName: "invalid startup option name",
	// EXIT_INVALID_OPTION_VALUE
	ArangoDExitInvalidOptionValue: "invalid startup option value",
	// EXIT_BINARY_NOT_FOUND
	ArangoDExitBinaryNotFound: "binary not found",
	// EXIT_CONFIG_NOT_FOUND
	ArangoDExitConfigNotFound: "config not found or invalid",
	// EXIT_UPGRADE_FAILED
	ArangoDExitUpgradeFailed: "upgrade failed",
	// EXIT_UPGRADE_REQUIRED
	ArangoDExitUpgradeRequired: "db upgrade required",
	// EXIT_DOWNGRADE_REQUIRED
	ArangoDExitDowngradeRequired: "db downgrade required",
	// EXIT_VERSION_CHECK_FAILED
	ArangoDExitVersionCheckFailed: "version check failed",
	// EXIT_ALREADY_RUNNING
	ArangoDExitAlreadyRunning: "already running",
	// EXIT_COULD_NOT_BIND_PORT
	ArangoDExitCouldNotBindPort: "port blocked",
	// EXIT_COULD_NOT_LOCK
	ArangoDExitCouldNotLock: "could not lock - another process could be running",
	// EXIT_RECOVERY
	ArangoDExitRecovery: "recovery failed",
	// EXIT_DB_NOT_EMPTY
	ArangoDExitDbNotEmpty: "database not empty",
	// EXIT_UNSUPPORTED_STORAGE_ENGINE
	ArangoDExitUnsupportedStorageEngine: "unsupported storage engine",
	// EXIT_ICU_INITIALIZATION_FAILED
	ArangoDExitIcuInitializationFailed: "failed to initialize ICU library",
	// EXIT_TZDATA_INITIALIZATION_FAILED
	ArangoDExitTzdataInitializationFailed: "failed to locate tzdata",
	// EXIT_RESOURCES_TOO_LOW
	ArangoDExitResourcesTooLow: "the system restricts resources below what is required to start arangod",
}
