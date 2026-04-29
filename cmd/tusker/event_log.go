package main

import (
	"encoding/json"
	"time"
)

type Event struct {
	Seq       int            `json:"seq"`
	At        string         `json:"at"`
	AttemptID string         `json:"attempt_id"`
	Runner    RunnerName     `json:"runner"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload,omitempty"`
}

type EventLog struct {
	path string
	seq  int
}

func NewEventLog(path string) *EventLog {
	return &EventLog{path: path}
}

func (l *EventLog) Append(kind string, attemptID string, runner RunnerName, payload map[string]any) error {
	l.seq++
	event := Event{Seq: l.seq, At: time.Now().UTC().Format(time.RFC3339), AttemptID: attemptID, Runner: runner, Kind: kind, Payload: payload}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	current := ""
	if fileExists(l.path) {
		text, err := readText(l.path)
		if err != nil {
			return err
		}
		current = text
	}
	return writeText(l.path, current+string(raw)+"\n")
}
