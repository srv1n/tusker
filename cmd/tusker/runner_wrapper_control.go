package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const claudeControlAckTimeout = 10 * time.Second

type claudeControlRequest struct {
	AttemptID       string `json:"attempt_id"`
	LeaseGeneration int    `json:"lease_generation"`
	DeliveryID      string `json:"delivery_id"`
	Op              string `json:"op"`
	Body            string `json:"body,omitempty"`
}

type claudeControlResponse struct {
	Receipt   string `json:"receipt,omitempty"`
	Uncertain bool   `json:"uncertain,omitempty"`
	Error     string `json:"error,omitempty"`
}

// The status path is already recorded in both run and attempt rows. Hashing it
// keeps the Unix socket below Darwin's short sun_path limit.
func claudeControlSocketPath(statusPath string) string {
	hash := sha256.Sum256([]byte(statusPath))
	return filepath.Join("/tmp", fmt.Sprintf("tusker-claude-%d", os.Getuid()), fmt.Sprintf("%x", hash[:12]), "control.sock")
}

func serveClaudeWrapperControl(ctx context.Context, req StartRequest, handle *claudeLiveHandle) (func(), error) {
	path := claudeControlSocketPath(req.StatusPath)
	dir := filepath.Dir(path)
	parent := filepath.Dir(dir)
	if err := os.Mkdir(parent, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0o700 || !ownedByCurrentUID(parentInfo) {
		return nil, fmt.Errorf("Claude control root is not private: %s", parent)
	}
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || !ownedByCurrentUID(info) {
		return nil, fmt.Errorf("Claude control directory is not private: %s", dir)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 || !ownedByCurrentUID(info) {
			return nil, fmt.Errorf("Claude control path is not an owned socket: %s", path)
		}
		if active, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond); dialErr == nil {
			_ = active.Close()
			return nil, fmt.Errorf("Claude control socket is already active")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Serialization includes echo waits so two messages cannot swap receipts.
			handleClaudeControlConnection(ctx, conn, req, handle)
		}
	}()
	return func() { _ = listener.Close(); _ = os.Remove(path); _ = os.Remove(dir) }, nil
}

func handleClaudeControlConnection(ctx context.Context, conn net.Conn, req StartRequest, handle *claudeLiveHandle) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(claudeControlAckTimeout + time.Second))
	response := claudeControlResponse{}
	defer func() { _ = json.NewEncoder(conn).Encode(response) }()
	if err := sameUIDPeer(conn); err != nil {
		response.Error = "control peer refused: " + err.Error()
		return
	}
	line, err := bufio.NewReader(io.LimitReader(conn, 1<<20)).ReadBytes('\n')
	if err != nil {
		response.Error = "invalid control request"
		return
	}
	var request claudeControlRequest
	if json.Unmarshal(line, &request) != nil || request.AttemptID != req.AttemptID || request.LeaseGeneration != req.LeaseGeneration || strings.TrimSpace(request.DeliveryID) == "" {
		response.Error = "control fence refused"
		return
	}
	if ctx.Err() != nil {
		response.Error = "wrapper is stopping"
		return
	}
	switch request.Op {
	case "say":
		if request.Body == "" {
			response.Error = "message is empty"
			return
		}
		receipt, err := handle.sendUserMessageAwaitEcho(request.Body, claudeControlAckTimeout)
		if err != nil {
			response.Uncertain = true
			response.Error = err.Error()
			return
		}
		response.Receipt = receipt
	case "interrupt":
		if err := handle.Interrupt(ctx); err != nil {
			response.Error = err.Error()
		}
	default:
		response.Error = "unsupported control operation"
	}
}

func sendClaudeWrapperControl(ctx context.Context, statusPath string, request claudeControlRequest) (claudeControlResponse, error) {
	var response claudeControlResponse
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.DialContext(ctx, "unix", claudeControlSocketPath(statusPath))
	if err != nil {
		return response, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(claudeControlAckTimeout + time.Second))
	if err := json.NewEncoder(conn).Encode(request); err != nil {
		response.Uncertain, response.Error = true, err.Error()
		return response, nil
	}
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		response.Uncertain, response.Error = true, err.Error()
		return response, nil
	}
	return response, nil
}
