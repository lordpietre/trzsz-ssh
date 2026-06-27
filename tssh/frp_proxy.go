package tssh

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbletea/v2"
)

const kFrpDefaultPort = 7000
const kFrpAdminPort = 7400

// --- Data types ---

type frpProxyEntry struct {
	Alias       string `json:"alias"`
	Name        string `json:"name"`
	Direction   string `json:"direction"`    // "r2l" or "l2r"
	FrpsAddr    string `json:"frps_addr"`    // address where frps is reachable
	FrpsPort    int    `json:"frps_port"`    // control port of frps (default 7000)
	Token       string `json:"token"`        // auth token
	ServicePort int    `json:"service_port"` // port of the service being proxied
	ExposedPort int    `json:"exposed_port"` // port that frps exposes
	Active      bool   `json:"-"`
	LocalCmd    *exec.Cmd `json:"-"` // local FRP process
}

type frpSetupProgressMsg struct {
	step string
	err  error
}

type frpScanResultMsg struct {
	ports []portInfo
	err   error
}

type frpStartResultMsg struct {
	entry *frpProxyEntry
	err   error
}

type frpStopResultMsg struct {
	entry *frpProxyEntry
	err   error
}

// --- UI state ---

type frpProxyState struct {
	active        bool
	view          string // "menu", "direction", "setup", "scan_results", "confirm", "list"
	alias         string
	direction     string // "r2l" or "l2r" (set during setup)
	entries       []*frpProxyEntry
	cursor        int
	delMode       bool
	configPath    string
	scanPorts     []portInfo
	scanCursor    int
	scanErr       string
	frpsAddr      string // detected public IP or configured address
	frpsPort      int
	setupStep     string // current step description shown during setup
	setupErr      string
	proxyName     string // proxy name being configured
	exposedPort   string // exposed port being configured (user input)
	visibleItems  int
}

func frpDefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tssh_frp")
}

func frpLoadConfig(path, alias string) []*frpProxyEntry {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var all []*frpProxyEntry
	if err := json.Unmarshal(data, &all); err != nil {
		return nil
	}
	var filtered []*frpProxyEntry
	for _, e := range all {
		if e.Alias == alias {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func frpSaveConfig(entries []*frpProxyEntry, path string) {
	if path == "" {
		return
	}
	var all []*frpProxyEntry
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	var keep []*frpProxyEntry
	for _, e := range all {
		found := false
		for _, ne := range entries {
			if e.Alias == ne.Alias && e.Name == ne.Name {
				found = true
				break
			}
		}
		if !found {
			keep = append(keep, e)
		}
	}
	for _, e := range entries {
		if !e.Active {
			keep = append(keep, e)
		}
	}
	keep = append(keep, activeEntries()...)
	data, err := json.MarshalIndent(keep, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0600)
}

var (
	frpActiveEntries []*frpProxyEntry
	frpMu            sync.Mutex
)

func activeEntries() []*frpProxyEntry {
	frpMu.Lock()
	defer frpMu.Unlock()
	result := make([]*frpProxyEntry, len(frpActiveEntries))
	copy(result, frpActiveEntries)
	return result
}

func trackEntry(entry *frpProxyEntry) {
	frpMu.Lock()
	defer frpMu.Unlock()
	frpActiveEntries = append(frpActiveEntries, entry)
}

func untrackEntry(entry *frpProxyEntry) {
	frpMu.Lock()
	defer frpMu.Unlock()
	for i, e := range frpActiveEntries {
		if e == entry {
			frpActiveEntries = append(frpActiveEntries[:i], frpActiveEntries[i+1:]...)
			return
		}
	}
}

func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "tssh-frp-default-token"
	}
	return hex.EncodeToString(b)
}

// --- Binary management ---

func frpFindBin(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// check next to tssh binary
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// check ~/.tssh/bin/
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".tssh", "bin", name)
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// --- Config file generation ---

func writeFrpsConfig(alias string, token string, bindPort int) (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tssh")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("frps_%s.toml", alias))
	content := fmt.Sprintf(`bindPort = %d

auth.method = "token"
auth.token = "%s"

webServer.addr = "127.0.0.1"
webServer.port = %d
`, bindPort, token, kFrpAdminPort)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", err
	}
	return path, nil
}

func writeFrpcConfig(alias string, serverAddr string, serverPort int, token string, proxies []*frpProxyEntry) (string, error) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".tssh")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("frpc_%s.toml", alias))
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf(`serverAddr = "%s"
serverPort = %d

auth.method = "token"
auth.token = "%s"

`, serverAddr, serverPort, token))
	for _, p := range proxies {
		buf.WriteString(fmt.Sprintf(`[[proxies]]
name = "%s"
type = "tcp"
localIP = "127.0.0.1"
localPort = %d
remotePort = %d

`, p.Name, p.ServicePort, p.ExposedPort))
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		return "", err
	}
	return path, nil
}

// --- IP detection ---

func detectPublicIP() (string, error) {
	providers := []string{
		"https://ifconfig.me",
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
		"https://ipinfo.io/ip",
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, url := range providers {
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("could not detect public IP (check internet connection)")
}

// --- Local port scanning ---

func frpScanLocalPorts() tea.Cmd {
	return func() tea.Msg {
		var ports []portInfo
		output, err := exec.Command("ss", "-tlnp").CombinedOutput()
		if err != nil {
			output2, err2 := exec.Command("netstat", "-tlnp").CombinedOutput()
			if err2 != nil {
				return frpScanResultMsg{err: fmt.Errorf("cannot scan ports: ss and netstat not available")}
			}
			output = output2
		}
		seen := make(map[int]bool)
		for _, line := range strings.Split(string(output), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			first := strings.Fields(line)[0]
			if first == "State" || first == "Proto" || first == "Netid" {
				continue
			}
			port, process, ok := parsePortLine(line)
			if ok && !seen[port] {
				seen[port] = true
				ports = append(ports, portInfo{Port: port, Process: process})
			}
		}
		sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
		return frpScanResultMsg{ports: ports, err: nil}
	}
}

// --- Remote operations ---

func frpExecRemote(alias string, cmdStr string) (string, error) {
	client, err := SshLogin(&SshArgs{
		Destination: alias,
		ConfigFile:  userConfig.configPath,
	})
	if err != nil {
		return "", fmt.Errorf("connect failed: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("session failed: %v", err)
	}
	defer session.Close()

	type result struct {
		output string
		err    error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := session.CombinedOutput(cmdStr)
		ch <- result{string(out), err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return "", fmt.Errorf("remote exec failed: %v\n%s", r.err, r.output)
		}
		return r.output, nil
	case <-time.After(30 * time.Second):
		session.Close()
		<-ch // drain
		return "", fmt.Errorf("remote exec timed out after 30s")
	}
}

func frpCopyToRemote(alias string, data []byte, remotePath string, mode string) error {
	client, err := SshLogin(&SshArgs{
		Destination: alias,
		ConfigFile:  userConfig.configPath,
	})
	if err != nil {
		return fmt.Errorf("connect failed: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("session failed: %v", err)
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe failed: %v", err)
	}
	remoteDir := filepath.Dir(remotePath)
	cmdStr := fmt.Sprintf("mkdir -p %s && cat > %s && chmod %s %s", remoteDir, remotePath, mode, remotePath)
	if err := session.Start(cmdStr); err != nil {
		return fmt.Errorf("remote command failed: %v", err)
	}
	if _, err := stdin.Write(data); err != nil {
		return fmt.Errorf("write data failed: %v", err)
	}
	stdin.Close()
	if err := session.Wait(); err != nil {
		return fmt.Errorf("copy failed: %v", err)
	}
	return nil
}

func frpInstallRemoteBinary(alias string, binPath string, remotePath string) error {
	binData, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("read binary failed: %v", err)
	}
	return frpCopyToRemote(alias, binData, remotePath, "755")
}

// --- Process management ---

func frpStartLocal(name string, args ...string) (*exec.Cmd, error) {
	bin := frpFindBin(name)
	if bin == "" {
		return nil, fmt.Errorf("%s not found", name)
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s failed: %v", name, err)
	}
	return cmd, nil
}

func frpStopProcess(cmd *exec.Cmd) error {
	if cmd != nil && cmd.Process != nil {
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
		_ = cmd.Wait()
	}
	return nil
}

// --- Setup command (runs all steps) ---

func frpSetupCmd(alias string, direction string) tea.Cmd {
	return func() tea.Msg {
		// Step 1: ensure FRP binaries are available
		if err := frpDownloadIfNeeded(); err != nil {
			return frpSetupProgressMsg{step: fmt.Sprintf("Setup failed: %v", err), err: err}
		}

		token := generateToken()
		port := kFrpDefaultPort

		var frpsAddr string
		var frpsCmd *exec.Cmd
		var frpcCmd *exec.Cmd

		if direction == "r2l" {
			// ----- Remote → Local -----
			// Step 2a: detect local public IP
			ip, err := detectPublicIP()
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("IP detection failed: %v", err), err: err}
			}
			frpsAddr = ip

			// Step 3a: write frps config and start locally
			frpsPath, err := writeFrpsConfig(alias, token, port)
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Config failed: %v", err), err: err}
			}
			frpsCmd, err = frpStartLocal("frps", "-c", frpsPath)
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Start frps failed: %v", err), err: err}
			}
			// give frps a moment to bind
			time.Sleep(500 * time.Millisecond)

			// Step 4a: install frpc on remote and start
			remoteBin := frpFindBin("frpc")
			remoteDir := "~/.tssh/bin"
			if err := frpInstallRemoteBinary(alias, remoteBin, filepath.Join(remoteDir, "frpc")); err != nil {
				frpStopProcess(frpsCmd)
				return frpSetupProgressMsg{step: fmt.Sprintf("Remote install failed: %v", err), err: err}
			}

			// write frpc config remotely
			frpcConfigPath, err := writeFrpcConfig(alias+"_remote", frpsAddr, port, token, []*frpProxyEntry{})
			if err != nil {
				frpStopProcess(frpsCmd)
				return frpSetupProgressMsg{step: fmt.Sprintf("Config failed: %v", err), err: err}
			}
			frpcData, _ := os.ReadFile(frpcConfigPath)
			remoteCfgDir := "~/.tssh"
			if err := frpCopyToRemote(alias, frpcData,
				filepath.Join(remoteCfgDir, fmt.Sprintf("frpc_%s.toml", alias)), "644"); err != nil {
				frpStopProcess(frpsCmd)
				return frpSetupProgressMsg{step: fmt.Sprintf("Config copy failed: %v", err), err: err}
			}

			// start frpc remotely (in background)
			frpcRemoteCmd := fmt.Sprintf("nohup %s/frpc -c %s/frpc_%s.toml > /tmp/frpc_%s.log 2>&1 & echo $!",
				remoteDir, remoteCfgDir, alias, alias)
			pidStr, err := frpExecRemote(alias, frpcRemoteCmd)
			if err != nil {
				frpStopProcess(frpsCmd)
				return frpSetupProgressMsg{step: fmt.Sprintf("Remote start failed: %v", err), err: err}
			}
			_ = pidStr

			// store state for UI
			_ = frpcConfigPath

			entry := &frpProxyEntry{
				Alias:       alias,
				Name:        "system:frps",
				Direction:   direction,
				FrpsAddr:    frpsAddr,
				FrpsPort:    port,
				Token:       token,
				Active:      true,
				LocalCmd:    frpsCmd,
			}
			trackEntry(entry)
			_ = frpcCmd

		} else {
			// ----- Local → Remote -----
			remoteCfgDir := "~/.tssh"

			// Step 2b: detect remote IP
			remoteIP, err := frpExecRemote(alias, "curl -s ifconfig.me || curl -s api.ipify.org || hostname -I | awk '{print $1}'")
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Remote IP detection failed: %v", err), err: err}
			}
			remoteIP = strings.TrimSpace(remoteIP)
			frpsAddr = remoteIP

			// Step 3b: install frps on remote, start it
			frpsBin := frpFindBin("frps")
			remoteDir := "~/.tssh/bin"
			if err := frpInstallRemoteBinary(alias, frpsBin, filepath.Join(remoteDir, "frps")); err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Remote install failed: %v", err), err: err}
			}

			// write frps config to remote
			frpsConfig := fmt.Sprintf(`bindPort = %d

auth.method = "token"
auth.token = "%s"

webServer.addr = "127.0.0.1"
webServer.port = %d
`, port, token, kFrpAdminPort)

			if err := frpCopyToRemote(alias, []byte(frpsConfig),
				filepath.Join(remoteCfgDir, fmt.Sprintf("frps_%s.toml", alias)), "644"); err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Config copy failed: %v", err), err: err}
			}

			// start frps on remote
			frpsRemoteCmd := fmt.Sprintf("nohup %s/frps -c %s/frps_%s.toml > /tmp/frps_%s.log 2>&1 & echo $!",
				remoteDir, remoteCfgDir, alias, alias)
			pidStr, err := frpExecRemote(alias, frpsRemoteCmd)
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Remote frps start failed: %v", err), err: err}
			}
			_ = pidStr

			// Step 4b: write frpc config locally (empty proxies for now)
			_, err = writeFrpcConfig(alias, frpsAddr, port, token, []*frpProxyEntry{})
			if err != nil {
				return frpSetupProgressMsg{step: fmt.Sprintf("Local config failed: %v", err), err: err}
			}
		}

		return frpSetupProgressMsg{step: "ready", err: nil}
	}
}

func frpAddProxyCmd(alias string, entry *frpProxyEntry) tea.Cmd {
	return func() tea.Msg {
		if entry.Direction == "r2l" {
			// add proxy to remote frpc config and reload
			remoteDir := "~/.tssh"
			// read existing config
			existingOutput, err := frpExecRemote(alias, fmt.Sprintf("cat %s/frpc_%s.toml", remoteDir, alias))
			if err != nil {
				return frpStartResultMsg{entry: entry, err: fmt.Errorf("read remote config: %v", err)}
			}
			// append new proxy
			proxyBlock := fmt.Sprintf(`
[[proxies]]
name = "%s"
type = "tcp"
localIP = "127.0.0.1"
localPort = %d
remotePort = %d
`, entry.Name, entry.ServicePort, entry.ExposedPort)

			newConfig := existingOutput + proxyBlock
			// write back
			if err := frpCopyToRemote(alias, []byte(newConfig),
				fmt.Sprintf("%s/frpc_%s.toml", remoteDir, alias), "644"); err != nil {
				return frpStartResultMsg{entry: entry, err: fmt.Errorf("write config: %v", err)}
			}
			// reload frpc
			reloadOut, err := frpExecRemote(alias,
				fmt.Sprintf("%s/frpc reload -c %s/frpc_%s.toml",
					filepath.Join(remoteDir, "bin"), remoteDir, alias))
			if err != nil {
				// frpc reload might fail if no admin port configured - try restart
				restartOut, err2 := frpExecRemote(alias,
					fmt.Sprintf("pkill -f 'frpc.*%s' 2>/dev/null; nohup %s/frpc -c %s/frpc_%s.toml > /tmp/frpc_%s.log 2>&1 &",
						alias, filepath.Join(remoteDir, "bin"), remoteDir, alias, alias))
				if err2 != nil {
					return frpStartResultMsg{entry: entry, err: fmt.Errorf("reload failed: %v", err)}
				}
				_ = restartOut
			} else {
				_ = reloadOut
			}

			entry.Active = true
			trackEntry(entry)
			return frpStartResultMsg{entry: entry, err: nil}
		} else {
			// add proxy to local frpc config and restart frpc
			home, _ := os.UserHomeDir()
			frpcConfigPath := filepath.Join(home, ".tssh", fmt.Sprintf("frpc_%s.toml", alias))

			// read existing config
			existingData, err := os.ReadFile(frpcConfigPath)
			if err != nil {
				return frpStartResultMsg{entry: entry, err: fmt.Errorf("read config: %v", err)}
			}
			proxyBlock := fmt.Sprintf(`
[[proxies]]
name = "%s"
type = "tcp"
localIP = "127.0.0.1"
localPort = %d
remotePort = %d
`, entry.Name, entry.ServicePort, entry.ExposedPort)
			newConfig := string(existingData) + proxyBlock
			if err := os.WriteFile(frpcConfigPath, []byte(newConfig), 0600); err != nil {
				return frpStartResultMsg{entry: entry, err: fmt.Errorf("write config: %v", err)}
			}

			// Kill existing frpc for this alias, restart
			for _, e := range activeEntries() {
				if e.Alias == alias && e.Name == "system:frpc" && e.LocalCmd != nil {
					_ = frpStopProcess(e.LocalCmd)
					untrackEntry(e)
				}
			}
			frpcCmd, err := frpStartLocal("frpc", "-c", frpcConfigPath)
			if err != nil {
				return frpStartResultMsg{entry: entry, err: fmt.Errorf("start frpc: %v", err)}
			}
			frpcEntry := &frpProxyEntry{
				Alias:    alias,
				Name:     "system:frpc",
				Active:   true,
				LocalCmd: frpcCmd,
			}
			trackEntry(frpcEntry)
			entry.Active = true
			trackEntry(entry)
			return frpStartResultMsg{entry: entry, err: nil}
		}
	}
}

func frpStopProxyCmd(alias string, entry *frpProxyEntry) tea.Cmd {
	return func() tea.Msg {
		removed := false
		if entry.Direction == "r2l" {
			// remove proxy from remote frpc config and reload
			remoteDir := "~/.tssh"
			existingOutput, err := frpExecRemote(alias, fmt.Sprintf("cat %s/frpc_%s.toml", remoteDir, alias))
			if err == nil {
				// remove the proxy block for this entry
				lines := strings.Split(existingOutput, "\n")
				var newLines []string
				skip := false
				for _, line := range lines {
					if strings.Contains(line, fmt.Sprintf(`name = "%s"`, entry.Name)) {
						skip = true
						removed = true
						continue
					}
					if skip {
						if strings.HasPrefix(strings.TrimSpace(line), "[[proxies]]") {
							skip = false
						} else if strings.TrimSpace(line) == "" {
							continue
						} else {
							// within the proxy block
							continue
						}
					}
					newLines = append(newLines, line)
				}
				if removed {
					newConfig := strings.Join(newLines, "\n")
					_ = frpCopyToRemote(alias, []byte(newConfig),
						fmt.Sprintf("%s/frpc_%s.toml", remoteDir, alias), "644")
					// reload
					_, _ = frpExecRemote(alias,
						fmt.Sprintf("%s/frpc reload -c %s/frpc_%s.toml 2>/dev/null || true",
							filepath.Join(remoteDir, "bin"), remoteDir, alias))
				}
			}
		} else {
			// remove proxy from local config and restart frpc
			home, _ := os.UserHomeDir()
			frpcConfigPath := filepath.Join(home, ".tssh", fmt.Sprintf("frpc_%s.toml", alias))
			if data, err := os.ReadFile(frpcConfigPath); err == nil {
				lines := strings.Split(string(data), "\n")
				var newLines []string
				skip := false
				for _, line := range lines {
					if strings.Contains(line, fmt.Sprintf(`name = "%s"`, entry.Name)) {
						skip = true
						removed = true
						continue
					}
					if skip {
						if strings.HasPrefix(strings.TrimSpace(line), "[[proxies]]") {
							skip = false
						} else {
							continue
						}
					}
					newLines = append(newLines, line)
				}
				if removed {
					_ = os.WriteFile(frpcConfigPath, []byte(strings.Join(newLines, "\n")), 0600)
					// restart frpc
					for _, e := range activeEntries() {
						if e.Alias == alias && e.Name == "system:frpc" && e.LocalCmd != nil {
							_ = frpStopProcess(e.LocalCmd)
							untrackEntry(e)
						}
					}
					frpcCmd, err := frpStartLocal("frpc", "-c", frpcConfigPath)
					if err == nil {
						frpcEntry := &frpProxyEntry{
							Alias:    alias,
							Name:     "system:frpc",
							Active:   true,
							LocalCmd: frpcCmd,
						}
						trackEntry(frpcEntry)
					}
				}
			}
		}

		entry.Active = false
		untrackEntry(entry)
		return frpStopResultMsg{entry: entry, err: nil}
	}
}

// --- UI: init state ---

func initFrpState() frpProxyState {
	return frpProxyState{
		configPath: frpDefaultPath(),
	}
}

// --- UI: dispatch ---

func frpCleanupLocalProcesses(alias string) {
	var toKill []*exec.Cmd
	for _, e := range activeEntries() {
		if e.Alias == alias && strings.HasPrefix(e.Name, "system:") && e.LocalCmd != nil {
			toKill = append(toKill, e.LocalCmd)
			untrackEntry(e)
		}
	}
	for _, cmd := range toKill {
		_ = frpStopProcess(cmd)
	}
}

func (m *hostModel) handleFrp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch m.frp.view {
	case "menu":
		return m.handleFrpMenu(msg)
	case "direction":
		return m.handleFrpDirection(msg)
	case "setup":
		// only allow Esc (when error shown) and Ctrl+C during setup
		s := msg.String()
		switch s {
		case "esc":
			if m.frp.setupErr != "" {
				frpCleanupLocalProcesses(m.frp.alias)
				m.frp.view = "menu"
				m.frp.cursor = 0
				m.frp.setupErr = ""
			}
			return m, nil
		case "ctrl+c", "ctrl+q":
			m.done = true
			m.result = hostChoiceMsg{quit: true}
			return m, tea.Quit
		}
		return m, nil
	case "scan_results":
		return m.handleFrpScanResults(msg)
	case "confirm":
		return m.handleFrpConfirm(msg)
	case "list":
		return m.handleFrpList(msg)
	}
	return m, nil
}

// --- UI: menu ---

func (m *hostModel) handleFrpMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up", "k":
		if m.frp.delMode {
			if m.frp.cursor > 0 {
				m.frp.cursor--
			}
		} else {
			if m.frp.cursor > 0 {
				m.frp.cursor--
			}
		}
	case "down", "j":
		if m.frp.delMode {
			if m.frp.cursor < len(m.frp.entries)-1 {
				m.frp.cursor++
			}
		} else {
			if m.frp.cursor < 1 { // 2 items: New, list area
				m.frp.cursor++
			}
		}
	case "enter":
		if m.frp.delMode {
			if m.frp.cursor >= 0 && m.frp.cursor < len(m.frp.entries) {
				entry := m.frp.entries[m.frp.cursor]
				m.frp.delMode = false
				return m, frpStopProxyCmd(m.frp.alias, entry)
			}
			m.frp.delMode = false
			return m, nil
		}
		if m.frp.cursor == 0 {
			// New FRP proxy
			m.frp.view = "direction"
			m.frp.cursor = 0
		}
	case "d":
		if len(m.frp.entries) > 0 {
			m.frp.delMode = !m.frp.delMode
			if m.frp.delMode {
				m.frp.cursor = 0
			}
		}
	case "esc", "q":
		if m.frp.delMode {
			m.frp.delMode = false
		} else {
			m.frp.active = false
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

// --- UI: direction ---

func (m *hostModel) handleFrpDirection(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up", "k":
		if m.frp.cursor > 0 {
			m.frp.cursor--
		}
	case "down", "j":
		if m.frp.cursor < 1 {
			m.frp.cursor++
		}
	case "enter":
		switch m.frp.cursor {
		case 0:
			m.frp.direction = "r2l"
		case 1:
			m.frp.direction = "l2r"
		}
		m.frp.view = "setup"
		m.frp.setupStep = "Setting up FRP proxy..."
		m.frp.setupErr = ""
		return m, frpSetupCmd(m.frp.alias, m.frp.direction)
	case "esc":
		m.frp.view = "menu"
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

// --- UI: scan results ---

func (m *hostModel) handleFrpScanResults(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	visible := m.frp.visibleItems
	switch s {
	case "up", "k":
		if m.frp.scanCursor > 0 {
			m.frp.scanCursor--
		}
	case "down", "j":
		if m.frp.scanCursor < visible-1 {
			m.frp.scanCursor++
		}
	case "enter":
		if visible == 0 || m.frp.scanCursor >= len(m.frp.scanPorts) {
			break
		}
		// select this port for proxying
		port := m.frp.scanPorts[m.frp.scanCursor].Port
		m.frp.view = "confirm"
		m.frp.cursor = 0
		m.frp.proxyName = fmt.Sprintf("proxy-%d", port)
		m.frp.exposedPort = ""
		m.frp.setupStep = fmt.Sprintf("%d", port) // store selected port
	case "esc":
		m.frp.view = "list"
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

// --- UI: confirm proxy details ---

func (m *hostModel) handleFrpConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	txt := msg.Key().Text
	switch s {
	case "tab", "down", "j":
		m.frp.cursor = (m.frp.cursor + 1) % 3
	case "shift+tab", "up", "k":
		m.frp.cursor = (m.frp.cursor + 2) % 3
	case "enter":
		if m.frp.proxyName == "" {
			return m, nil
		}
		exposedPort, _ := strconv.Atoi(m.frp.exposedPort)
		if exposedPort <= 0 {
			// default to same port
			servicePort, _ := strconv.Atoi(m.frp.setupStep)
			exposedPort = servicePort
		}
		servicePort, _ := strconv.Atoi(m.frp.setupStep)
		if servicePort <= 0 {
			return m, nil
		}

		entry := &frpProxyEntry{
			Alias:       m.frp.alias,
			Name:        m.frp.proxyName,
			Direction:   m.frp.direction,
			FrpsAddr:    m.frp.frpsAddr,
			FrpsPort:    m.frp.frpsPort,
			Token:       "",
			ServicePort: servicePort,
			ExposedPort: exposedPort,
		}
		m.frp.view = "setup"
		m.frp.setupStep = "Starting proxy..."
		return m, frpAddProxyCmd(m.frp.alias, entry)
	case "esc":
		m.frp.view = "scan_results"
	case "backspace":
		switch m.frp.cursor {
		case 0:
			if len(m.frp.proxyName) > 0 {
				m.frp.proxyName = m.frp.proxyName[:len(m.frp.proxyName)-1]
			}
		case 2:
			if len(m.frp.exposedPort) > 0 {
				m.frp.exposedPort = m.frp.exposedPort[:len(m.frp.exposedPort)-1]
			}
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	default:
		if txt == "" {
			return m, nil
		}
		switch m.frp.cursor {
		case 0:
			m.frp.proxyName += txt
		case 2:
			for _, r := range txt {
				if r >= '0' && r <= '9' {
					m.frp.exposedPort += string(r)
				}
			}
		}
	}
	return m, nil
}

// --- UI: list (post-setup) ---

func (m *hostModel) handleFrpList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := msg.String()
	switch s {
	case "up", "k":
		if m.frp.cursor > 0 {
			m.frp.cursor--
		}
	case "down", "j":
		if m.frp.cursor < len(m.frp.entries)-1 {
			m.frp.cursor++
		}
	case "d":
		if len(m.frp.entries) > 0 {
			m.frp.delMode = !m.frp.delMode
			m.frp.cursor = 0
		}
	case "enter":
		if m.frp.delMode && m.frp.cursor < len(m.frp.entries) {
			entry := m.frp.entries[m.frp.cursor]
			m.frp.delMode = false
			return m, frpStopProxyCmd(m.frp.alias, entry)
		}
		// add another proxy
		m.frp.view = "scan_results"
		m.frp.scanCursor = 0
		return m, tunnelScanRemote(m.frp.alias)
	case "n":
		// new proxy with scan
		m.frp.view = "scan_results"
		m.frp.scanCursor = 0
		return m, tunnelScanRemote(m.frp.alias)
	case "esc", "q":
		m.frp.active = false
		// cleanup system entries
		for _, e := range activeEntries() {
			if e.Alias == m.frp.alias && strings.HasPrefix(e.Name, "system:") {
				_ = frpStopProcess(e.LocalCmd)
				untrackEntry(e)
			}
		}
		// cleanup remote processes
		if m.frp.direction == "r2l" {
			_, _ = frpExecRemote(m.frp.alias, "pkill -f 'frpc.*"+m.frp.alias+"' 2>/dev/null || true")
		} else {
			_, _ = frpExecRemote(m.frp.alias, "pkill -f 'frps.*"+m.frp.alias+"' 2>/dev/null || true")
		}
	case "ctrl+c", "ctrl+q":
		m.done = true
		m.result = hostChoiceMsg{quit: true}
		return m, tea.Quit
	}
	return m, nil
}

// --- Rendering ---

func (m *hostModel) renderFrpView(b *strings.Builder, maxLines int) {
	switch m.frp.view {
	case "menu":
		m.renderFrpMenu(b, maxLines)
	case "direction":
		m.renderFrpDirection(b, maxLines)
	case "setup":
		m.renderFrpSetup(b, maxLines)
	case "scan_results":
		m.renderFrpScanResults(b, maxLines)
	case "confirm":
		m.renderFrpConfirm(b, maxLines)
	case "list":
		m.renderFrpList(b, maxLines)
	}
}

func (m *hostModel) renderFrpMenu(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── FRP Proxy for %s ──", m.frp.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	if len(m.frp.entries) > 0 {
		b.WriteString(m.bgLine("  Saved proxies:") + "\n")
		for i, e := range m.frp.entries {
			direction := "R2L"
			if e.Direction == "l2r" {
				direction = "L2R"
			}
			line := fmt.Sprintf("  · [%s] %s → %d (service :%d)", direction, e.Name, e.ExposedPort, e.ServicePort)
			if e.Active {
				line += " [ACTIVE]"
			}
			if m.frp.delMode && i == m.frp.cursor {
				b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
			} else {
				b.WriteString(m.bgLine(m.helpStyle.Render("   "+line)) + "\n")
			}
		}
		if m.frp.delMode {
			b.WriteString(m.bgLine(m.helpStyle.Render("  [Delete mode] Select proxy to remove, Enter confirm, Esc cancel")) + "\n")
		}
		b.WriteString(m.bgLine("") + "\n")
	}

	items := []string{"[New]  Create new FRP proxy"}
	lineCount := 3 + len(m.frp.entries)
	if len(m.frp.entries) > 0 {
		lineCount += 2
	}
	if m.frp.delMode {
		lineCount++
	}
	for i, item := range items {
		if m.frp.delMode {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+item)) + "\n")
		} else if i == m.frp.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+item)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+item)) + "\n")
		}
		lineCount++
	}
	b.WriteString(m.bgLine("") + "\n")
	lineCount++

	if m.frp.delMode {
		b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Select  Enter delete  Esc cancel  q Quit")) + "\n")
	} else {
		b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter Select  d Delete  Esc Back  q Quit")) + "\n")
	}
	lineCount++

	for i := lineCount; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderFrpDirection(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Select Direction ──")
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	items := []string{
		"  Expose remote service locally  (Remote→Local)",
		"  Expose local service remotely   (Local→Remote)",
	}
	for i, item := range items {
		if i == m.frp.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+item)) + "\n")
		} else {
			b.WriteString(m.bgLine("   " + item) + "\n")
		}
	}
	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Navigate  Enter Select  Esc back")) + "\n")
	for i := 5; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderFrpSetup(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── FRP Setup for %s ──", m.frp.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")
	if m.frp.setupErr != "" {
		b.WriteString(m.bgLine("  Error: "+m.frp.setupErr) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		b.WriteString(m.bgLine(m.helpStyle.Render("  Press Esc to go back")) + "\n")
	} else {
		b.WriteString(m.bgLine("  "+m.frp.setupStep) + "\n")
	}
	for i := 3; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderFrpScanResults(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── Select Port to Proxy ──")
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	if m.frp.scanErr != "" {
		b.WriteString(m.bgLine("  Error: "+m.frp.scanErr) + "\n")
		return
	}

	totalPorts := len(m.frp.scanPorts)
	if totalPorts == 0 {
		b.WriteString(m.bgLine("  No open ports found.") + "\n")
	}

	available := maxLines - 3
	showPorts := totalPorts
	if totalPorts > available {
		showPorts = available
	}
	visibleItems := showPorts
	if visibleItems < 1 && totalPorts > 0 {
		visibleItems = 1
	}
	if m.frp.scanCursor >= visibleItems {
		m.frp.scanCursor = visibleItems - 1
	}
	if m.frp.scanCursor < 0 {
		m.frp.scanCursor = 0
	}
	m.frp.visibleItems = visibleItems

	rendered := 0
	for i := 0; i < totalPorts && rendered < showPorts; i++ {
		p := m.frp.scanPorts[i]
		line := formatPortLabel(p)
		if i == m.frp.scanCursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.inactiveStyle.Render("   "+line)) + "\n")
		}
		rendered++
	}
	for i := rendered; i < maxLines-2; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Select  Enter confirm  Esc back")) + "\n")
}

func (m *hostModel) renderFrpConfirm(b *strings.Builder, maxLines int) {
	servicePort := m.frp.setupStep
	title := fmt.Sprintf("  ── Configure Proxy for Port %s ──", servicePort)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")
	b.WriteString(m.bgLine("") + "\n")

	// Proxy name
	nameLabel := fmt.Sprintf("  Proxy name: %s", m.frp.proxyName)
	if m.frp.cursor == 0 {
		b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+nameLabel+"▌")) + "\n")
	} else {
		b.WriteString(m.bgLine("   " + nameLabel) + "\n")
	}

	// Service port (read-only)
	svcLabel := fmt.Sprintf("  Service port: %s", servicePort)
	b.WriteString(m.bgLine("   " + svcLabel) + "\n")

	// Exposed port
	expLabel := fmt.Sprintf("  Exposed port: %s", m.frp.exposedPort)
	if m.frp.cursor == 2 {
		b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+expLabel+"▌")) + "\n")
	} else {
		b.WriteString(m.bgLine("   " + expLabel) + "\n")
	}

	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  Tab/↑↓ Switch field  Enter confirm  Esc back")) + "\n")
	for i := 6; i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}

func (m *hostModel) renderFrpList(b *strings.Builder, maxLines int) {
	title := fmt.Sprintf("  ── FRP Proxies for %s ──", m.frp.alias)
	b.WriteString(m.bgLine(m.activeStyle.Render(clipString(title, m.width-1))) + "\n")

	if m.frp.setupErr != "" {
		b.WriteString(m.bgLine("  Error: "+m.frp.setupErr) + "\n")
		b.WriteString(m.bgLine("") + "\n")
		m.frp.setupErr = ""
	}

	if len(m.frp.entries) == 0 {
		b.WriteString(m.bgLine("  No active proxies.") + "\n")
	}
	for i, e := range m.frp.entries {
		direction := "R2L"
		if e.Direction == "l2r" {
			direction = "L2R"
		}
		line := fmt.Sprintf("  · [%s] %s → %s:%d (service :%d)",
			direction, e.Name, e.FrpsAddr, e.ExposedPort, e.ServicePort)
		if e.Active {
			line += " [ACTIVE]"
		}
		if m.frp.delMode && i == m.frp.cursor {
			b.WriteString(m.bgLine(m.activeStyle.Render("▐█ "+line)) + "\n")
		} else {
			b.WriteString(m.bgLine(m.helpStyle.Render("   "+line)) + "\n")
		}
	}

	b.WriteString(m.bgLine("") + "\n")
	b.WriteString(m.bgLine(m.helpStyle.Render("  ↑↓ Select  d Delete  n New proxy  Esc Quit")) + "\n")
	for i := 3 + len(m.frp.entries); i < maxLines; i++ {
		b.WriteString(m.bgLine("") + "\n")
	}
}
