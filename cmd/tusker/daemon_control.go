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
}

type daemonControlResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type daemonControlServer struct {
	path string
	ln   net.Listener
	wg   sync.WaitGroup
}

func (d *Daemon) handleControlRequest(ctx context.Context, req daemonControlRequest, stop func()) daemonControlResponse {
	switch req.Command {
	case "notify":
		if !d.queueWriteNotify(req.ProjectID) {
			return daemonControlResponse{OK: false, Message: "project notification is missing a project id"}
		}
		return daemonControlResponse{OK: true}
	case "interrupt":
		if err := d.InterruptRun(ctx, req.Identity); err != nil {
			return daemonControlResponse{OK: false, Message: err.Error()}
		}
		return daemonControlResponse{OK: true}
	case "stop":
		if stop != nil {
			stop()
		}
		return daemonControlResponse{OK: true, Message: "daemon stop requested"}
	default:
		return daemonControlResponse{OK: false, Message: "unknown control command"}
	}
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
	conn, err := net.DialTimeout("unix", daemonSocketPath(stateRoot), 1500*time.Millisecond)
	if err != nil {
		return daemonControlResponse{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
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

func sendDaemonControlOneWay(stateRoot string, req daemonControlRequest, timeout time.Duration) error {
	conn, err := net.DialTimeout("unix", daemonSocketPath(stateRoot), timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = conn.Write(append(raw, '\n'))
	return err
}
