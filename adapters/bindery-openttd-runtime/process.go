package openttd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Everything in this file drives an unmodified OpenTTD binary. The game is not
// aware of Bindery, exposes no hook for it, and offers exactly two ways in: the
// command line and the admin network. That is the point of the run -- the
// dedicated adapter's integration mechanism was chosen by the same people who
// wrote the contracts it tests.

// BinaryEnvVar names the OpenTTD executable to run. hack/fetch-openttd.sh
// installs one and prints the value to set.
const BinaryEnvVar = "BINDERY_OPENTTD_BIN"

// LocateBinary resolves the OpenTTD executable, preferring the explicit
// environment variable over whatever is on PATH.
func LocateBinary() (string, error) {
	if fromEnv := os.Getenv(BinaryEnvVar); fromEnv != "" {
		if info, err := os.Stat(fromEnv); err != nil || info.IsDir() {
			return "", fmt.Errorf("%s=%q is not an executable file", BinaryEnvVar, fromEnv)
		}
		return fromEnv, nil
	}
	return exec.LookPath("openttd")
}

// Server is a real OpenTTD dedicated server: its own process, its own map, and
// an admin port that is the only way to learn what happened inside it.
type Server struct {
	binary    string
	directory string
	command   *exec.Cmd
	logFile   *os.File

	GamePort  int
	AdminPort int
	Password  string
}

// StartServer generates a map and waits until the admin port answers.
func StartServer(binary, directory, adminPassword string, maxClients, maxCompanies int) (*Server, error) {
	gamePort, err := freePort()
	if err != nil {
		return nil, err
	}
	adminPort, err := freePort()
	if err != nil {
		return nil, err
	}
	server := &Server{
		binary:    binary,
		directory: directory,
		GamePort:  gamePort,
		AdminPort: adminPort,
		Password:  adminPassword,
	}
	config := strings.Join([]string{
		"[network]",
		fmt.Sprintf("server_port = %d", gamePort),
		fmt.Sprintf("server_admin_port = %d", adminPort),
		"admin_password = " + adminPassword,
		// The alternative to this is re-implementing an X25519 handshake; the
		// server only listens on loopback for the length of one test.
		"allow_insecure_admin_login = true",
		"server_game_type = 0",
		"server_name = bindery-openttd-acceptance",
		fmt.Sprintf("max_clients = %d", maxClients),
		fmt.Sprintf("max_companies = %d", maxCompanies),
		"autoclean_companies = false",
		"server_admin_chat = true",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, "openttd.cfg"), []byte(config), 0o600); err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(directory, "server.log"))
	if err != nil {
		return nil, err
	}
	server.logFile = logFile

	// -x keeps the game from writing the config back, so the run leaves the
	// configuration it was given rather than one the game edited.
	command := exec.Command(binary,
		"-D", fmt.Sprintf("127.0.0.1:%d", gamePort),
		"-c", filepath.Join(directory, "openttd.cfg"),
		"-x", "-g",
	)
	command.Dir = directory
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start openttd dedicated server: %w", err)
	}
	server.command = command
	if err := waitForPort(fmt.Sprintf("127.0.0.1:%d", adminPort), 90*time.Second); err != nil {
		server.Stop()
		return nil, fmt.Errorf("%w (server log: %s)", err, server.LogTail())
	}
	return server, nil
}

// AdminAddress is where an admin application connects.
func (s *Server) AdminAddress() string { return fmt.Sprintf("127.0.0.1:%d", s.AdminPort) }

// GameAddress is where a player's client connects.
func (s *Server) GameAddress() string { return fmt.Sprintf("127.0.0.1:%d", s.GamePort) }

// Stop kills the server. OpenTTD's dedicated console reads stdin, which the
// test does not own, so a signal is the honest way to end it.
func (s *Server) Stop() {
	if s.command != nil && s.command.Process != nil {
		_ = s.command.Process.Kill()
		_ = s.command.Wait()
		s.command = nil
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
}

// LogTail returns the end of the server's own log, for failures that are the
// game's rather than the control plane's.
func (s *Server) LogTail() string {
	return tail(filepath.Join(s.directory, "server.log"), 30)
}

// PlayerProcess is an actual OpenTTD client, joined over the network. It runs
// on the null video driver, which renders nothing and runs the game loop as
// fast as it can; the game has no other headless client mode.
type PlayerProcess struct {
	Name    string
	command *exec.Cmd
	logFile *os.File
	dir     string
}

// StartPlayer launches a client that joins the server and creates a company.
func StartPlayer(binary, directory, name, serverAddress string) (*PlayerProcess, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	config := strings.Join([]string{
		"[network]",
		// Without a client name the game refuses to join and says nothing about
		// it: NetworkValidateOurClientName fails, and on a headless driver
		// there is no dialog to prompt with.
		"client_name = " + name,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(directory, "openttd.cfg"), []byte(config), 0o600); err != nil {
		return nil, err
	}
	logFile, err := os.Create(filepath.Join(directory, "client.log"))
	if err != nil {
		return nil, err
	}
	command := exec.Command(binary,
		// The tick budget is how long the client lives: the null driver exits
		// when it runs out, and the process is killed long before that.
		"-v", "null:ticks=1000000000",
		"-c", filepath.Join(directory, "openttd.cfg"),
		"-x", "-n", serverAddress,
	)
	command.Dir = directory
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start openttd client %s: %w", name, err)
	}
	return &PlayerProcess{Name: name, command: command, logFile: logFile, dir: directory}, nil
}

// Stop ends the client process.
func (p *PlayerProcess) Stop() {
	if p.command != nil && p.command.Process != nil {
		_ = p.command.Process.Kill()
		_ = p.command.Wait()
		p.command = nil
	}
	if p.logFile != nil {
		_ = p.logFile.Close()
		p.logFile = nil
	}
}

// LogTail returns the end of this client's log.
func (p *PlayerProcess) LogTail() string { return tail(filepath.Join(p.dir, "client.log"), 20) }

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForPort(address string, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err == nil {
			connection.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("nothing listened on %s within %s", address, within)
}

func tail(path string, lines int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return "(no log: " + err.Error() + ")"
	}
	split := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(split) > lines {
		split = split[len(split)-lines:]
	}
	return strings.Join(split, "\n")
}
