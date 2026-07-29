package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type daemonControlRequest struct {
	Command   string `json:"command"`
	Identity  string `json:"identity"`
	ProjectID string `json:"project_id,omitempty"`
	// Optional rich hints preserve compatibility with existing project-only
	// reconciliation notifications.
	Cause   string                `json:"cause,omitempty"`
	Changes []daemonControlChange `json:"changes,omitempty"`
}

type daemonControlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

func runDirectiveBypassableBlocker(blocker string) bool {
	switch strings.TrimSpace(blocker) {
	case "project automation is disabled in its configuration":
		return true
	default:
		return false
	}
}

type daemonControlServer struct {
	path string
	ln   net.Listener
	wg   sync.WaitGroup
}

func daemonSocketPath(stateRoot string) string {
	return filepath.Join(stateRoot, "daemon.sock")
}

func startDaemonControlServer(stateRoot string, handler func(context.Context, daemonControlRequest) daemonControlResponse) (*daemonControlServer, error) {
	path := daemonSocketPath(stateRoot)
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	server := &daemonControlServer{path: path, ln: ln}
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
			server.wg.Add(1)
			go func(c net.Conn) {
				defer server.wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				line, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				var req daemonControlRequest
				if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &req); err != nil {
					_, _ = c.Write([]byte(`{"ok":false,"message":"invalid control request"}` + "\n"))
					return
				}
				resp := handler(context.Background(), req)
				raw, _ := json.Marshal(resp)
				_, _ = c.Write(append(raw, '\n'))
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
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return daemonControlResponse{}, err
	}
	var resp daemonControlResponse
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &resp); err != nil {
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
