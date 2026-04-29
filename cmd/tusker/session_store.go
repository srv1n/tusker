package main

import "sync"

type AttemptRecord struct {
	AttemptID     string
	ProjectID     string
	RecordID      string
	ItemID        string
	WorkRevision  int
	Runner        RunnerName
	LeaseState    LeaseState
	Outcome       AttemptOutcome
	WorkspacePath string
	SessionRef    string
	StartedAt     string
	FinishedAt    string
}

type SessionRecord struct {
	AttemptID  string
	SessionRef string
	Runner     RunnerName
}

type SessionStore interface {
	SaveAttempt(record AttemptRecord) error
	SaveSession(record SessionRecord) error
	GetSession(attemptID string) (SessionRecord, bool)
}

type MemorySessionStore struct {
	mu       sync.Mutex
	attempts map[string]AttemptRecord
	sessions map[string]SessionRecord
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{attempts: map[string]AttemptRecord{}, sessions: map[string]SessionRecord{}}
}

func (s *MemorySessionStore) SaveAttempt(record AttemptRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts[record.AttemptID] = record
	return nil
}

func (s *MemorySessionStore) SaveSession(record SessionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[record.AttemptID] = record
	return nil
}

func (s *MemorySessionStore) GetSession(attemptID string) (SessionRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.sessions[attemptID]
	return record, ok
}
