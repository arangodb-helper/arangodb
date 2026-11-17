//
// DISCLAIMER
//
// Copyright 2017-2024 ArangoDB GmbH, Cologne, Germany
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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/arangodb-helper/arangodb/pkg/definitions"
	"github.com/arangodb-helper/arangodb/pkg/logging"
	"github.com/arangodb-helper/arangodb/service/actions"
)

// runtimeServerManager implements the start, monitor, stop behavior of database servers in a runtime
// state.
type runtimeServerManager struct {
	logMutex        sync.Mutex // Mutex used to synchronize server log output
	agentProc       ProcessWrapper
	dbserverProc    ProcessWrapper
	coordinatorProc ProcessWrapper
	singleProc      ProcessWrapper
	syncMasterProc  ProcessWrapper
	syncWorkerProc  ProcessWrapper

	stopping bool
}

// runtimeServerManagerContext provides a context for the runtimeServerManager.
type runtimeServerManagerContext interface {
	ClientBuilder

	// ClusterConfig returns the current cluster configuration and the current peer
	ClusterConfig() (ClusterConfig, *Peer, ServiceMode)

	// serverPort returns the port number on which my server of given type will listen.
	serverPort(serverType definitions.ServerType) (int, error)

	// serverHostDir returns the path of the folder (in host namespace) containing data for the given server.
	serverHostDir(serverType definitions.ServerType) (string, error)
	// serverContainerDir returns the path of the folder (in container namespace) containing data for the given server.
	serverContainerDir(serverType definitions.ServerType) (string, error)

	// serverHostLogFile returns the path of the logfile (in host namespace) to which the given server will write its logs.
	serverHostLogFile(serverType definitions.ServerType) (string, error)
	// serverContainerLogFile returns the path of the logfile (in container namespace) to which the given server will write its logs.
	serverContainerLogFile(serverType definitions.ServerType) (string, error)

	// removeRecoveryFile removes any recorded RECOVERY file.
	removeRecoveryFile()

	// UpgradeManager returns the upgrade manager service.
	UpgradeManager() UpgradeManager

	// TestInstance checks the `up` status of an arangod server instance.
	TestInstance(ctx context.Context, serverType definitions.ServerType, address string, port int,
		statusChanged chan StatusItem) (up, correctRole bool, version, role, mode string, isLeader bool, statusTrail []int, cancelled bool)

	// IsLocalSlave returns true if this peer is running as a local slave
	IsLocalSlave() bool

	// DatabaseFeatures returns the detected database features.
	DatabaseFeatures() DatabaseFeatures

	// Stop the peer
	Stop()
}

// startServer starts a single Arangod/Arangosync server of the given type.
func startServer(ctx context.Context, log zerolog.Logger, runtimeContext runtimeServerManagerContext, runner Runner,
	config Config, bsCfg BootstrapConfig, myHostAddress string, serverType definitions.ServerType, features DatabaseFeatures, restart int, output io.Writer, lastExitCode int, logMark *logFileMark) (Process, bool, error) {
	myPort, err := runtimeContext.serverPort(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myHostDir, err := runtimeContext.serverHostDir(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myContainerDir, err := runtimeContext.serverContainerDir(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}
	myContainerLogFile, err := runtimeContext.serverContainerLogFile(serverType)
	if err != nil {
		return nil, false, maskAny(err)
	}

	os.MkdirAll(filepath.Join(myHostDir, "data"), 0755)
	os.MkdirAll(filepath.Join(myHostDir, "apps"), 0755)
	os.MkdirAll(filepath.Join(myHostDir, definitions.ArangodJWTSecretFolderName), 0700)

	// Check if the server is already running
	log.Info().Msgf("Looking for a running instance of %s on port %d", serverType, myPort)
	p, err := runner.GetRunningServer(myHostDir)
	if err != nil {
		return nil, false, maskAny(err)
	}
	if p != nil {
		log.Info().Msgf("%s seems to be running already, checking port %d...", serverType, myPort)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		up, correctRole, _, _, _, _, _, _ := runtimeContext.TestInstance(ctx, serverType, myHostAddress, myPort, nil)
		cancel()
		if up && correctRole {
			log.Info().Msgf("%s is already running on %d. No need to start anything.", serverType, myPort)
			return p, false, nil
		} else if !up {
			log.Info().Msgf("%s is not up on port %d. Terminating existing process and restarting it...", serverType, myPort)
		} else if !correctRole {
			expectedRole, expectedMode := serverType.ExpectedServerRole()
			log.Info().Msgf("%s is not of role '%s.%s' on port %d. Terminating existing process and restarting it...", serverType, expectedRole, expectedMode, myPort)
		}

		// Terminate without actions
		terminateProcessWithActions(log, p, serverType, 0, time.Minute)
	}

	// Check availability of port
	if !WaitUntilPortAvailable("", myPort, time.Second*3) {
		return nil, true, maskAny(fmt.Errorf("Cannot start %s, because port %d is already in use", serverType, myPort))
	}

	log.Info().Msgf("Starting %s on port %d", serverType, myPort)
	processType := serverType.ProcessType()
	// Create/read arangod.conf
	var confVolumes []Volume
	var arangodConfig configFile
	var containerSecretFileName string
	if processType == definitions.ProcessTypeArangod {
		var err error
		confVolumes, arangodConfig, err = createArangodConf(log, bsCfg, myHostDir, myContainerDir, strconv.Itoa(myPort), serverType, features)
		if err != nil {
			return nil, false, maskAny(err)
		}
	}
	if processType == definitions.ProcessTypeArangoSync || features.HasJWTSecretFileOption() {
		var err error
		var secretFileVolumes []Volume
		secretFileVolumes, containerSecretFileName, err = createArangoClusterSecretFile(log, bsCfg, myHostDir, myContainerDir, serverType, features)
		if err != nil {
			return nil, false, maskAny(err)
		}
		confVolumes = append(confVolumes, secretFileVolumes...)
	}

	// Collect volumes
	v := collectServerConfigVolumes(serverType, arangodConfig)
	confVolumes = append(confVolumes, v...)

	// Create server command line arguments
	clusterConfig, myPeer, _ := runtimeContext.ClusterConfig()
	upgradeManager := runtimeContext.UpgradeManager()
	databaseAutoUpgrade := false
	launchClaimed := false
	if upgradeManager != nil {
		if upgradeManager.PrepareAutoUpgradeLaunch(serverType) {
			databaseAutoUpgrade = true
			launchClaimed = true
		} else if lastExitCode == definitions.ArangoDExitUpgradeRequired {
			databaseAutoUpgrade = true
		}
	}
	args, err := createServerArgs(log, config, clusterConfig, myContainerDir, myContainerLogFile, myPeer.ID, myHostAddress, strconv.Itoa(myPort), serverType, arangodConfig,
		containerSecretFileName, bsCfg.RecoveryAgentID, databaseAutoUpgrade, features)
	if err != nil {
		if launchClaimed {
			upgradeManager.CancelAutoUpgradeLaunch(serverType)
		}
		return nil, false, maskAny(err)
	}
	log.Debug().Msgf("%s at %d args: %+v", serverType, myPort, args)
	writeCommandFile(log, filepath.Join(myHostDir, processType.CommandFileName()), args)
	// Collect volumes
	vols := addVolume(confVolumes, myHostDir, myContainerDir, false)
	// Start process/container
	containerNamePrefix := ""
	if config.DockerConfig.HostContainerName != "" {
		containerNamePrefix = fmt.Sprintf("%s-", config.DockerConfig.HostContainerName)
	}
	containerName := fmt.Sprintf("%s%s-%s-%d-%s-%d", containerNamePrefix, serverType, myPeer.ID, restart, myHostAddress, myPort)
	ports := []int{myPort}
	// Snapshot after any leftover process has been stopped and immediately
	// before the child exists, so a fast failure is kept and the previous
	// server's shutdown lines are not.
	if logMark != nil {
		*logMark = serverHostLogMark(runtimeContext, serverType)
	}
	p, err = runner.Start(ctx, processType, args[0], args[1:], createEnvs(config, serverType), vols, ports, containerName, myHostDir, output)
	if err != nil {
		if launchClaimed {
			upgradeManager.CancelAutoUpgradeLaunch(serverType)
		}
		return nil, false, maskAny(err)
	}
	if databaseAutoUpgrade {
		// Notify the context that we've succesfully started a server with database.auto-upgrade on.
		upgradeManager.ServerDatabaseAutoUpgradeStarter(serverType)
	}
	return p, false, nil
}

// showRecentLogs dumps the most recent log lines of the server of given type to the console.
func (s *runtimeServerManager) showRecentLogs(log zerolog.Logger, runtimeContext runtimeServerManagerContext, serverType definitions.ServerType) {
	logPath, err := runtimeContext.serverHostLogFile(serverType)
	if err != nil {
		log.Error().Err(err).Msg("Cannot find server host log file")
		return
	}
	body, err := readRecentServerLog(logPath)
	if os.IsNotExist(err) {
		log.Info().Msgf("Log file for %s is empty", serverType)
	} else if err != nil {
		log.Error().Err(err).Msgf("Cannot open log file for %s", serverType)
	} else if strings.TrimSpace(body) == "" {
		log.Info().Msgf("Log file for %s is empty", serverType)
	} else {
		log.Info().Msg(formatRecentServerLog(serverType, body))
	}
}

// logFileMark is the position of an arangod log just before an attempt starts.
// Size alone is not enough: scheduled rotation can rename the file and create
// a new one at the same path before the attempt exits.
type logFileMark struct {
	size int64
	id   logFileID
	// known is false when the platform cannot identify files.
	known bool
	// absent is true when the log file did not exist at the start of the attempt.
	// prior are the rotated files that already existed then, so a rotation
	// during the attempt is read and those older files are not.
	absent bool
	prior  []logFileID
}

// logFileID identifies a file independent of its name.
type logFileID struct {
	volume uint64
	index  uint64
}

func serverHostLogMark(runtimeContext runtimeServerManagerContext, serverType definitions.ServerType) logFileMark {
	if runtimeContext == nil {
		return logFileMark{}
	}
	logPath, err := runtimeContext.serverHostLogFile(serverType)
	if err != nil {
		return logFileMark{}
	}
	return logFileMarkAt(logPath)
}

func logFileMarkAt(logPath string) logFileMark {
	// Size and identity come from one stat or handle. Separate calls could
	// pair the old log's size with the file that replaced it on rotation,
	// and reading that file from the old size would skip its first lines.
	size, id, known, err := fileSizeAndIdentity(logPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return logFileMark{}
		}
		mark := logFileMark{absent: true}
		// Identity has to work for a file that does exist, or an older rotation
		// cannot be told from one created during the attempt.
		if _, supported := fileIdentity("."); supported {
			if ids, ok := existingRotationIDs(logPath); ok {
				mark.known = true
				mark.prior = ids
			}
		}
		return mark
	}
	return logFileMark{size: size, id: id, known: known}
}

// existingRotationIDs returns the identities of logPath.1, logPath.2, ...
// ok is false when a rotation exists but cannot be identified.
func existingRotationIDs(logPath string) ([]logFileID, bool) {
	var ids []logFileID
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s.%d", logPath, i)
		id, ok := fileIdentity(candidate)
		if ok {
			ids = append(ids, id)
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			return ids, true
		}
		return nil, false
	}
}

// readServerLogSince reads the log written at and after offset. It streams
// the whole attempt, keeping a bounded set of ERROR and FATAL lines and the
// actual tail, so a long log does not hide either.
func readServerLogSince(logPath string, offset int64) (string, error) {
	return readServerLogSinceMark(logPath, logFileMark{size: offset})
}

// readServerLogSinceMark is readServerLogSince for a log that rotation may
// have renamed while the attempt was running. It then reads the marked file
// from its offset, every newer rotation, and the current file, oldest first.
func readServerLogSinceMark(logPath string, mark logFileMark) (string, error) {
	// Files are selected by identity and opened afterwards. A rotation in
	// between makes the opened files differ from the selection; it is then
	// selected again, so ERROR and FATAL lines are not skipped.
	const attempts = 3
	for attempt := 1; ; attempt++ {
		parts, files, consistent, err := openServerLogSinceMark(logPath, mark)
		if err != nil {
			return "", err
		}
		if !consistent && attempt < attempts {
			closeLogFiles(files)
			continue
		}
		defer closeLogFiles(files)
		if len(parts) == 0 {
			return "", nil
		}
		return streamImportantAndTail(io.MultiReader(parts...))
	}
}

// logSelectionHook, if set, runs before the log files are opened. Tests use it
// to rotate the log at that moment.
var logSelectionHook func()

func closeLogFiles(files []*os.File) {
	for _, f := range files {
		_ = f.Close()
	}
}

// openServerLogSinceMark opens the log written since mark, positioned at the
// attempt's first byte. consistent is false when a rotation changed the files
// between selecting and opening them.
func openServerLogSinceMark(logPath string, mark logFileMark) (parts []io.Reader, files []*os.File, consistent bool, err error) {
	consistent = true
	use := func(f *os.File, offset int64) error {
		files = append(files, f)
		info, err := f.Stat()
		if err != nil {
			return err
		}
		if offset < 0 || offset > info.Size() {
			offset = 0
		}
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return err
		}
		parts = append(parts, f)
		return nil
	}
	// addFrom reads path from offset. When the identity it was selected by is
	// known, the opened file must still be that file.
	addFrom := func(path string, offset int64, want logFileID, wantKnown bool) error {
		f, err := os.Open(path)
		if err != nil {
			if os.IsNotExist(err) {
				if wantKnown {
					consistent = false
				}
				return nil
			}
			return err
		}
		if wantKnown {
			if id, ok := openedFileIdentity(f); ok && id != want {
				_ = f.Close()
				consistent = false
				return nil
			}
		}
		return use(f, offset)
	}
	fail := func(err error) ([]io.Reader, []*os.File, bool, error) {
		closeLogFiles(files)
		return nil, nil, false, err
	}

	if _, err := os.Stat(logPath); err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	if logSelectionHook != nil {
		logSelectionHook()
	}
	if mark.absent && mark.known {
		rotated := rotationsSinceAbsent(logPath, mark)
		currentID, currentKnown := fileIdentity(logPath)
		for _, r := range rotated {
			if err := addFrom(r.path, r.offset, r.id, r.known); err != nil {
				return fail(err)
			}
		}
		if err := addFrom(logPath, 0, currentID, currentKnown); err != nil {
			return fail(err)
		}
		return parts, files, consistent, nil
	}
	// The marked file is still the current log: read it from the mark.
	// Its identity is checked on the opened file, not on the path, so a
	// rotation right before the open is not read from the old offset.
	if f, err := os.Open(logPath); err == nil {
		id, ok := openedFileIdentity(f)
		if mark.absent || !mark.known || !ok || id == mark.id {
			if err := use(f, mark.size); err != nil {
				return fail(err)
			}
			return parts, files, consistent, nil
		}
		_ = f.Close()
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	// The log was rotated since the mark: read the rotations written since,
	// then the current file from its start.
	rotated := rotatedLogFilesSince(logPath, mark)
	currentID, currentKnown := fileIdentity(logPath)
	for _, r := range rotated {
		if err := addFrom(r.path, r.offset, r.id, r.known); err != nil {
			return fail(err)
		}
	}
	if err := addFrom(logPath, 0, currentID, currentKnown); err != nil {
		return fail(err)
	}
	return parts, files, consistent, nil
}

type rotatedLogFile struct {
	path   string
	offset int64
	// id is the identity the file was selected by, when known.
	id    logFileID
	known bool
}

// rotationsSinceAbsent returns rotated files created during an attempt whose
// log did not exist yet, oldest first. Rotations that already existed are skipped.
func rotationsSinceAbsent(logPath string, mark logFileMark) []rotatedLogFile {
	var newestFirst []rotatedLogFile
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s.%d", logPath, i)
		id, ok := fileIdentity(candidate)
		if !ok {
			if _, err := os.Stat(candidate); err != nil {
				break
			}
			continue
		}
		if priorRotation(mark, id) {
			continue
		}
		newestFirst = append(newestFirst, rotatedLogFile{path: candidate, id: id, known: true})
	}
	result := make([]rotatedLogFile, 0, len(newestFirst))
	for i := len(newestFirst) - 1; i >= 0; i-- {
		result = append(result, newestFirst[i])
	}
	return result
}

func priorRotation(mark logFileMark, id logFileID) bool {
	for _, prior := range mark.prior {
		if prior == id {
			return true
		}
	}
	return false
}

// rotatedLogFilesSince returns the rotated files written during the attempt,
// oldest first. log.1 is the newest rotation. The file holding the mark is
// read from the marked offset and every newer rotation from its start. When
// the marked file has already been removed, all remaining rotations are newer.
func rotatedLogFilesSince(logPath string, mark logFileMark) []rotatedLogFile {
	if !mark.known {
		return []rotatedLogFile{{path: logPath + ".1", offset: mark.size}}
	}
	var newestFirst []rotatedLogFile
	marked := -1
	for i := 1; ; i++ {
		candidate := logPath + fmt.Sprintf(".%d", i)
		if _, err := os.Stat(candidate); err != nil {
			break
		}
		id, known := fileIdentity(candidate)
		newestFirst = append(newestFirst, rotatedLogFile{path: candidate, id: id, known: known})
		if known && id == mark.id {
			marked = len(newestFirst) - 1
			break
		}
	}
	var result []rotatedLogFile
	for i := len(newestFirst) - 1; i >= 0; i-- {
		file := newestFirst[i]
		if i == marked {
			file.offset = mark.size
		}
		result = append(result, file)
	}
	return result
}

// readRecentServerLog returns only the last 20 lines of an arangod log file.
func readRecentServerLog(logPath string) (string, error) {
	logFile, err := os.Open(logPath)
	if err != nil {
		return "", err
	}
	defer logFile.Close()
	reader := bufio.NewReaderSize(logFile, 32*1024)
	var tail []string
	for {
		line, err := readCappedLogLine(reader, 256*1024)
		if line != "" || err == nil {
			tail = appendBounded(tail, line, 20)
		}
		if err == io.EOF {
			return strings.Join(tail, "\n"), nil
		}
		if err != nil {
			return "", err
		}
	}
}

func streamImportantAndTail(r io.Reader) (string, error) {
	const (
		maxImportant = 40
		tailLines    = 20
		maxLine      = 256 * 1024
	)
	reader := bufio.NewReaderSize(r, 32*1024)
	// The first error is usually the cause; later ones are often follow-up
	// failures. Keep it even when more than maxImportant errors follow.
	// Lines keep their position, so the result is in log order.
	var first *numberedLine
	var important []numberedLine
	tail := make([]numberedLine, 0, tailLines)
	for n := 0; ; n++ {
		line, err := readCappedLogLine(reader, maxLine)
		if line != "" {
			nl := numberedLine{n: n, text: line}
			if isUpgradeErrorLine(line) {
				if first == nil {
					first = &nl
				}
				important = appendBounded(important, nl, maxImportant)
			}
			tail = appendBounded(tail, nl, tailLines)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if first != nil {
		important = append(important, *first)
	}
	return joinInLogOrder(important, tail), nil
}

// numberedLine is a log line with its position in the log.
type numberedLine struct {
	n    int
	text string
}

func readCappedLogLine(r *bufio.Reader, max int) (string, error) {
	var b strings.Builder
	for {
		frag, err := r.ReadSlice('\n')
		if b.Len() < max {
			remain := max - b.Len()
			if len(frag) > remain {
				b.Write(frag[:remain])
			} else {
				b.Write(frag)
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return strings.TrimRight(b.String(), "\r\n"), err
	}
}

func appendBounded[T any](lines []T, line T, max int) []T {
	if len(lines) < max {
		return append(lines, line)
	}
	copy(lines, lines[1:])
	lines[max-1] = line
	return lines
}

// joinInLogOrder merges the selected lines by their position in the log,
// keeping each position once.
func joinInLogOrder(selections ...[]numberedLine) string {
	byPosition := make(map[int]string)
	for _, lines := range selections {
		for _, l := range lines {
			if text := strings.TrimRight(l.text, "\r"); text != "" {
				byPosition[l.n] = text
			}
		}
	}
	positions := make([]int, 0, len(byPosition))
	for n := range byPosition {
		positions = append(positions, n)
	}
	sort.Ints(positions)
	out := make([]string, 0, len(positions))
	for _, n := range positions {
		out = append(out, byPosition[n])
	}
	return strings.Join(out, "\n")
}

func isUpgradeErrorLine(line string) bool {
	return strings.Contains(line, " ERROR ") || strings.Contains(line, " FATAL ")
}

func formatRecentServerLog(serverType definitions.ServerType, body string) string {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("## Start of %s log\n", serverType))
	for _, line := range strings.Split(body, "\n") {
		buf.WriteString("\t")
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	buf.WriteString(fmt.Sprintf("## End of %s log", serverType))
	return buf.String()
}

// rotateLogFile rotates the log file of a single server.
func (s *runtimeServerManager) rotateLogFile(ctx context.Context, log zerolog.Logger, runtimeContext runtimeServerManagerContext, myPeer Peer, serverType definitions.ServerType, p Process, filesToKeep int) {
	if p == nil {
		return
	}

	// Prepare log path
	logPath, err := runtimeContext.serverHostLogFile(serverType)
	if err != nil {
		log.Debug().Err(err).Msgf("Failed to get host log file for '%s'", serverType)
		return
	}
	log.Debug().Msgf("Rotating %s log file: %s", serverType, logPath)

	// Move old files
	for i := filesToKeep; i >= 0; i-- {
		var logPathX string
		if i == 0 {
			logPathX = logPath
		} else {
			logPathX = logPath + fmt.Sprintf(".%d", i)
		}
		if _, err := os.Stat(logPathX); err == nil {
			if i == filesToKeep {
				// Remove file
				if err := os.Remove(logPathX); err != nil {
					log.Error().Err(err).Msgf("Failed to remove %s", logPathX)
				} else {
					log.Debug().Msgf("Removed old log file: %s", logPathX)
				}
			} else {
				// Rename log[.i] -> log.i+1
				logPathNext := logPath + fmt.Sprintf(".%d", i+1)
				if err := os.Rename(logPathX, logPathNext); err != nil {
					log.Error().Err(err).Msgf("Failed to move %s to %s", logPathX, logPathNext)
				} else {
					log.Debug().Msgf("Moved log file %s to %s", logPathX, logPathNext)
				}
			}
		}
	}

	// Send HUP signal
	if err := p.Hup(); err != nil {
		log.Error().Err(err).Msg("Failed to send HUP signal")
	}
	return
}

// RotateLogFiles rotates the log files of all servers
func (s *runtimeServerManager) RotateLogFiles(ctx context.Context, log zerolog.Logger, logService logging.Service, runtimeContext runtimeServerManagerContext, config Config) {
	log.Info().Msg("Rotating log files...")
	logService.RotateLogFiles()
	_, myPeer, _ := runtimeContext.ClusterConfig()
	if myPeer == nil {
		log.Error().Msg("Cannot find my own peer in cluster configuration")
	} else {
		if w := s.syncWorkerProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeSyncWorker, p, config.LogRotateFilesToKeep)
			}
		}
		if w := s.syncMasterProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeSyncMaster, p, config.LogRotateFilesToKeep)
			}
		}
		if w := s.singleProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeSingle, p, config.LogRotateFilesToKeep)
			}
		}
		if w := s.coordinatorProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeCoordinator, p, config.LogRotateFilesToKeep)
			}
		}
		if w := s.dbserverProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeDBServer, p, config.LogRotateFilesToKeep)
			}
		}
		if w := s.agentProc; w != nil {
			if p := w.Process(); p != nil {
				s.rotateLogFile(ctx, log, runtimeContext, *myPeer, definitions.ServerTypeAgent, p, config.LogRotateFilesToKeep)
			}
		}
	}
}

// Run starts all relevant servers and keeps the running.
func (s *runtimeServerManager) Run(ctx context.Context, log zerolog.Logger, runtimeContext runtimeServerManagerContext, runner Runner, config Config, bsCfg BootstrapConfig) {
	defer s.clean()

	_, myPeer, mode := runtimeContext.ClusterConfig()
	if myPeer == nil {
		log.Fatal().Msg("Cannot find my own peer in cluster configuration")
	}

	actions.RegisterAction(&ActionResignLeadership{
		runtimeContext: runtimeContext,
	})

	if mode.IsClusterMode() {
		// Start agent:
		if myPeer.HasAgent() {
			s.agentProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeAgent, time.Minute)
			time.Sleep(time.Second)
		}

		// Start DBserver:
		if bsCfg.StartDBserver == nil || *bsCfg.StartDBserver {
			s.dbserverProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeDBServer, time.Minute)
			time.Sleep(time.Second)
		}

		// Start Coordinator:
		if bsCfg.StartCoordinator == nil || *bsCfg.StartCoordinator {
			s.coordinatorProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeCoordinator, time.Minute)
		}

		// Start sync master
		if bsCfg.StartSyncMaster == nil || *bsCfg.StartSyncMaster {
			s.syncMasterProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeSyncMaster, time.Minute)
		}

		// Start sync worker
		if bsCfg.StartSyncWorker == nil || *bsCfg.StartSyncWorker {
			s.syncWorkerProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeSyncWorker, time.Minute)
		}
	} else if mode.IsActiveFailoverMode() {
		// Start agent:
		if myPeer.HasAgent() {
			s.agentProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeAgent, time.Minute)
			time.Sleep(time.Second)
		}

		// Start Single server:
		if myPeer.HasResilientSingle() {
			s.singleProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeResilientSingle, time.Minute)
		}
	} else if mode.IsSingleMode() {
		// Start Single server:
		s.singleProc = NewProcessWrapper(s, ctx, log, runtimeContext, runner, config, bsCfg, *myPeer, definitions.ServerTypeSingle, time.Minute)
	}

	// Wait until context is cancelled, then we'll stop
	<-ctx.Done()
	s.stopping = true

	log.Info().Msg("Shutting down services...")
	timeout := getTimeoutProcessTermination(definitions.ServerTypeSyncWorker)
	if p := s.syncWorkerProc; p != nil {
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeSyncWorker).
				Msg("did not terminate in time")
		}
	}

	timeout = getTimeoutProcessTermination(definitions.ServerTypeSyncMaster)
	if p := s.syncMasterProc; p != nil {
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeSyncMaster).
				Msg("did not terminate in time")
		}
	}

	timeout = getTimeoutProcessTermination(definitions.ServerTypeSingle)
	if p := s.singleProc; p != nil {
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeSingle).
				Msg("did not terminate in time")
		}
	}

	timeout = getTimeoutProcessTermination(definitions.ServerTypeCoordinator)
	if p := s.coordinatorProc; p != nil {
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeCoordinator).
				Msg("did not terminate in time")
		}
	}

	timeout = getTimeoutProcessTermination(definitions.ServerTypeDBServer)
	if p := s.dbserverProc; p != nil {
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeDBServer).
				Msg("did not terminate in time")
		}
	}

	timeout = getTimeoutProcessTermination(definitions.ServerTypeAgent)
	if p := s.agentProc; p != nil {
		time.Sleep(3 * time.Second)
		if !p.Wait(timeout) {
			log.Warn().Str("timeout", timeout.String()).
				Str("type", definitions.ServerTypeAgent).
				Msg("did not terminate in time")
		}
	}

	// Cleanup containers
	log.Debug().Msg("Shutting down: cleaning up")
	if w := s.syncWorkerProc; w != nil {
		if p := w.Process(); p != nil {
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup sync worker")
			}
		}
	}
	if w := s.syncMasterProc; w != nil {
		if p := w.Process(); p != nil {
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup sync master")
			}
		}
	}
	if w := s.singleProc; w != nil {
		if p := w.Process(); p != nil {
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup single server")
			}
		}
	}
	if w := s.coordinatorProc; w != nil {
		if p := w.Process(); p != nil {
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup coordinator")
			}
		}
	}
	if w := s.dbserverProc; w != nil {
		if p := w.Process(); p != nil {
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup dbserver")
			}
		}
	}
	if w := s.agentProc; w != nil {
		if p := w.Process(); p != nil {
			time.Sleep(3 * time.Second)
			if err := p.Cleanup(); err != nil {
				log.Warn().Err(err).Msg("Failed to cleanup agent")
			}
		}
	}

	// Cleanup runner
	log.Debug().Msg("Shutting down: cleaning up runner")
	if err := runner.Cleanup(); err != nil {
		log.Warn().Err(err).Msg("Failed to cleanup runner: %v")
	}

	log.Info().Msg("Shutdown complete")
}

// RestartServer triggers a restart of the server of the given type.
func (s *runtimeServerManager) RestartServer(log zerolog.Logger, serverType definitions.ServerType) error {
	var w ProcessWrapper

	switch serverType {
	case definitions.ServerTypeAgent:
		w = s.agentProc
	case definitions.ServerTypeDBServer:
		w = s.dbserverProc
	case definitions.ServerTypeDBServerNoResign:
		w = s.dbserverProc
	case definitions.ServerTypeCoordinator:
		w = s.coordinatorProc
	case definitions.ServerTypeSingle, definitions.ServerTypeResilientSingle:
		w = s.singleProc
	case definitions.ServerTypeSyncMaster:
		w = s.syncMasterProc
	case definitions.ServerTypeSyncWorker:
		w = s.syncWorkerProc
	default:
		return maskAny(fmt.Errorf("Unknown server type '%s'", serverType))
	}

	if w == nil {
		log.Warn().Msgf("Was asked to restart %s but no such process exists", serverType)
		return nil
	}
	// Notify before terminating. The wrapper can observe the process exit and
	// start the next iteration as soon as the process dies. The notification
	// has to be visible before that transition, and it must refer to the
	// process that is running now so a later start is not killed.
	p := w.Process()
	w.RequestRestart()
	if p != nil {
		terminateProcessWithActions(log, p, serverType, 0, time.Minute, actions.ActionTypeAll)
	} else {
		log.Warn().Msgf("Was asked to restart %s but no such process exists", serverType)
	}
	return nil
}

// getTimeoutProcessTermination returns how long it should wait for termination for a given server type.
func getTimeoutProcessTermination(serverType definitions.ServerType) time.Duration {
	if serverType == definitions.ServerTypeDBServer {
		return time.Minute * 5
	}

	// return default timeout
	return time.Minute
}
