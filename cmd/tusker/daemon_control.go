package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const daemonControlMaxRequestBytes = 64 << 10
const daemonControlMaxConcurrent = 32

type daemonControlRequest struct {
	Command   string                        `json:"command"`
	Identity  string                        `json:"identity"`
	ProjectID string                        `json:"project_id,omitempty"`
	Worker    *daemonWorkerLifecycleRequest `json:"worker,omitempty"`
	// Optional rich hints preserve compatibility with existing project-only
	// reconciliation notifications.
	Cause   string                `json:"cause,omitempty"`
	Changes []daemonControlChange `json:"changes,omitempty"`
}

// daemonWorkerLifecycleRequest is transport, not authority. The daemon checks
// every injected identity against its active run before performing the write.
type daemonWorkerLifecycleRequest struct {
	Action          string `json:"action"`
	AttemptID       string `json:"attempt_id"`
	RecordID        string `json:"record_id"`
	Lane            string `json:"lane"`
	Workspace       string `json:"workspace"`
	StatusPath      string `json:"status_path"`
	LeaseGeneration int    `json:"lease_generation"`
	WorkRevision    int    `json:"work_revision"`
	Deliverable     string `json:"deliverable,omitempty"`
	Verification    string `json:"verification,omitempty"`
	GateVerdicts    string `json:"gate_verdicts,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type daemonControlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func runDirectiveBypassableBlocker(blocker string) bool {
	blocker = strings.TrimSpace(blocker)
	switch blocker {
	case "project automation is disabled in its configuration",
		"dispatch scope armed_waves requires task membership in a currently armed wave",
		"wave is not durably armed":
		return true
	}
	return strings.Contains(blocker, "wave ") && strings.Contains(blocker, " authorization is disarmed")
}

type daemonControlServer struct {
	path string
	ln   net.Listener
	wg   sync.WaitGroup
	sem  chan struct{}
}

func daemonSocketPath(stateRoot string) string {
	return filepath.Join(stateRoot, "daemon.sock")
}

func readDaemonControlLine(r *bufio.Reader) ([]byte, error) {
	var raw []byte
	for {
		part, err := r.ReadSlice('\n')
		if len(part) > daemonControlMaxRequestBytes-len(raw) {
			return nil, fmt.Errorf("control request too large")
		}
		raw = append(raw, part...)
		if err == nil {
			return raw, nil
		}
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("unterminated control request")
		}
		return nil, err
	}
}

func startDaemonControlServer(stateRoot string, handler func(context.Context, daemonControlRequest) daemonControlResponse) (*daemonControlServer, error) {
	path := daemonSocketPath(stateRoot)
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("refusing symlink daemon control socket: %s", path)
		}
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("refusing non-socket daemon control path: %s", path)
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && uint32(os.Getuid()) != stat.Uid {
			return nil, fmt.Errorf("refusing daemon control socket owned by another user: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		_ = os.Remove(path)
		return nil, err
	}
	server := &daemonControlServer{path: path, ln: ln, sem: make(chan struct{}, daemonControlMaxConcurrent)}
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			select {
			case server.sem <- struct{}{}:
			default:
				_ = conn.SetDeadline(time.Now().Add(250 * time.Millisecond))
				_, _ = conn.Write([]byte(`{"ok":false,"message":"daemon control busy; retry"}` + "\n"))
				_ = conn.Close()
				continue
			}
			server.wg.Add(1)
			go func(c net.Conn) {
				defer server.wg.Done()
				defer c.Close()
				defer func() { <-server.sem }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				raw, err := readDaemonControlLine(bufio.NewReaderSize(c, daemonControlMaxRequestBytes+1))
				if err != nil {
					return
				}
				if len(raw) > daemonControlMaxRequestBytes {
					_, _ = c.Write([]byte(`{"ok":false,"message":"control request too large or unterminated"}` + "\n"))
					return
				}
				var req daemonControlRequest
				if err := json.Unmarshal([]byte(strings.TrimSpace(string(raw))), &req); err != nil {
					_, _ = c.Write([]byte(`{"ok":false,"message":"invalid control request"}` + "\n"))
					return
				}
				resp := handler(context.Background(), req)
				responseRaw, _ := json.Marshal(resp)
				_, _ = c.Write(append(responseRaw, '\n'))
			}(conn)
		}
	}()
	return server, nil
}

func (s *daemonControlServer) Close() error {
	if s == nil {
		return nil
	}
	if s.ln != nil {
		_ = s.ln.Close()
	}
	s.wg.Wait()
	if s.path != "" {
		_ = os.Remove(s.path)
	}
	return nil
}

func sendDaemonControl(stateRoot string, req daemonControlRequest) (daemonControlResponse, error) {
	return sendDaemonControlWithTimeout(stateRoot, req, 6500*time.Millisecond)
}

func sendDaemonControlWithTimeout(stateRoot string, req daemonControlRequest, timeout time.Duration) (daemonControlResponse, error) {
	if timeout <= 0 {
		timeout = 6500 * time.Millisecond
	}
	deadline := time.Now().Add(timeout)
	dialTimeout := min(timeout, 1500*time.Millisecond)
	conn, err := net.DialTimeout("unix", daemonSocketPath(stateRoot), dialTimeout)
	if err != nil {
		return daemonControlResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(deadline)
	raw, err := json.Marshal(req)
	if err != nil {
		return daemonControlResponse{}, err
	}
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		return daemonControlResponse{}, err
	}
	rawResponse, err := readDaemonControlLine(bufio.NewReader(conn))
	if err != nil {
		return daemonControlResponse{}, err
	}
	var resp daemonControlResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(rawResponse))), &resp); err != nil {
		return daemonControlResponse{}, err
	}
	return resp, nil
}

// sendDaemonControlOneWay delivers a best-effort notification without waiting
// for a response. CLI mutation commands call this synchronously so the write is
// complete before main can os.Exit, while a missing daemon only costs the
// bounded dial timeout.
func sendDaemonControlOneWay(stateRoot string, req daemonControlRequest, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 250 * time.Millisecond
	}
	conn, err := net.DialTimeout("unix", daemonSocketPath(stateRoot), timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	return json.NewEncoder(conn).Encode(req)
}
