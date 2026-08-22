package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	serveassets "tusker/internal/serve"
)

type daemonServeServer struct {
	addr       string
	httpServer *http.Server
	done       chan error
	stream     *serveStreamBroker
}

type daemonServeTarget struct {
	project RegisteredProject
	addr    string
}

func (d *Daemon) startServe(_ context.Context) (*daemonServeServer, error) {
	target, enabled, err := d.serveTarget()
	if err != nil {
		return nil, err
	}
	if !enabled {
		_ = d.updateServePIDFile(false, "")
		return nil, nil
	}
	dist, err := serveassets.DistFS()
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", target.addr)
	if err != nil {
		return nil, daemonServeBindError(target.addr, err, d.stateRoot)
	}
	actualAddr := ln.Addr().String()
	_ = d.updateServePIDFile(true, actualAddr)
	stream := newServeStreamBroker()
	d.stream = stream
	server := newServeServer(target.project.VaultRoot, target.project.RepoRoot, actualAddr, d.store, dist)
	server.requireCapability = true
	server.stream = stream
	server.reconcileStatus = d.adaptiveReconcileStatus
	d.serve = server
	httpServer := serveHTTPServer(actualAddr, server)
	daemonServer := &daemonServeServer{
		addr:       actualAddr,
		httpServer: httpServer,
		done:       make(chan error, 1),
		stream:     stream,
	}
	go func() {
		err := httpServer.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		if err != nil {
			log.Printf("tusker daemon serve stopped unexpectedly: addr=%s error=%v", actualAddr, err)
		}
		daemonServer.done <- err
	}()
	return daemonServer, nil
}

func (d *Daemon) serveTarget() (daemonServeTarget, bool, error) {
	projects, err := loadRegisteredProjects(d.store, registeredProjectLoadOptions{})
	if err != nil {
		return daemonServeTarget{}, false, err
	}
	if len(projects) == 0 {
		return daemonServeTarget{}, false, nil
	}
	sort.Slice(projects, func(i, j int) bool {
		left := firstNonEmpty(projects[i].Project.ProjectID, projects[i].Project.Name, projects[i].Project.RepoRoot)
		right := firstNonEmpty(projects[j].Project.ProjectID, projects[j].Project.Name, projects[j].Project.RepoRoot)
		return left < right
	})
	for i := range projects {
		candidate := projects[i]
		if !candidate.Project.Enabled || !candidate.Loadable() {
			continue
		}
		cfg := candidate.Workflow.Data.Runtime.Serve
		if !cfg.Enabled {
			return daemonServeTarget{}, false, nil
		}
		addr, err := serveNormalizeAddr(firstNonEmpty(strings.TrimSpace(cfg.Addr), defaultServeAddr))
		if err != nil {
			return daemonServeTarget{}, false, err
		}
		return daemonServeTarget{project: candidate.Project, addr: addr}, true, nil
	}
	return daemonServeTarget{}, false, nil
}

func (d *Daemon) updateServePIDFile(enabled bool, addr string) error {
	if d == nil || d.guard == nil {
		return nil
	}
	return d.guard.updateServePIDFile(enabled, addr)
}

func (s *daemonServeServer) Close(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	if s.stream != nil {
		s.stream.Close()
	}
	if err := s.httpServer.Shutdown(ctx); err != nil {
		_ = s.httpServer.Close()
		return err
	}
	select {
	case err := <-s.done:
		return err
	case <-ctx.Done():
		_ = s.httpServer.Close()
		return ctx.Err()
	}
}

func daemonServeBindError(addr string, err error, stateRoot string) error {
	liveness := readDaemonLiveness(stateRoot, time.Now().UTC())
	holder := "external process already bound to " + addr
	if liveness.Alive && liveness.PID > 0 && liveness.PID != os.Getpid() {
		holder = fmt.Sprintf("daemon pid %d", liveness.PID)
	}
	return tuskerError(errorInvalidTransition, fmt.Sprintf("daemon serve failed to bind http://%s; holder=%s: %v", addr, holder, err), withContext(map[string]any{"addr": addr, "holder": holder}))
}

func daemonServeURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	return "http://" + addr
}
