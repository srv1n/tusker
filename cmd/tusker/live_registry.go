package main

import (
	"context"
	"errors"
	"sync"
)

type LiveRunnerHandle interface {
	AttemptID() string
	ProjectID() string
	RecordID() string
	ItemID() string
	Runner() RunnerName
	Interrupt(context.Context) error
}

type LiveRegistry struct {
	mu        sync.RWMutex
	byAttempt map[string]LiveRunnerHandle
}

func NewLiveRegistry() *LiveRegistry {
	return &LiveRegistry{byAttempt: map[string]LiveRunnerHandle{}}
}

func (r *LiveRegistry) Register(handle LiveRunnerHandle) {
	if r == nil || handle == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byAttempt[handle.AttemptID()] = handle
}

func (r *LiveRegistry) Unregister(attemptID string) {
	if r == nil || attemptID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byAttempt, attemptID)
}

func (r *LiveRegistry) Find(identity string) LiveRunnerHandle {
	if r == nil || identity == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if handle, ok := r.byAttempt[identity]; ok {
		return handle
	}
	for _, handle := range r.byAttempt {
		if handle.ItemID() == identity || handle.RecordID() == identity {
			return handle
		}
	}
	return nil
}

var liveRegistry = NewLiveRegistry()

var errLiveHandleNotFound = errors.New("live runner handle not found")
