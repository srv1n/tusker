package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AgentContact struct {
	ProjectID     string            `json:"projectId"`
	TaskID        string            `json:"taskId"`
	Role          string            `json:"role"`
	Name          string            `json:"name,omitempty"`
	Address       AgentAddress      `json:"address"`
	Generation    int               `json:"generation"`
	PredecessorID string            `json:"predecessorId,omitempty"`
	Endpoint      map[string]string `json:"endpoint,omitempty"`
	UpdatedAt     string            `json:"updatedAt"`
}

func (c AgentContact) validate() error {
	if strings.TrimSpace(c.ProjectID) == "" || strings.TrimSpace(c.TaskID) == "" || strings.TrimSpace(c.Address.ID) == "" {
		return errors.New("contact project, task and address are required")
	}
	if c.Role != "architect" && c.Role != "origin" && c.Role != "peer" {
		return errors.New("contact role must be architect, origin or peer")
	}
	if c.Role == "peer" && strings.TrimSpace(c.Name) == "" {
		return errors.New("peer contact requires a name")
	}
	if c.Address.Kind != "task" && c.Address.Kind != "execution" {
		return errors.New("contact address must be task or execution")
	}
	return nil
}

func (s *RuntimeStore) PutAgentContact(c AgentContact, expectedGeneration int) (AgentContact, error) {
	if err := c.validate(); err != nil {
		return AgentContact{}, err
	}
	c.Generation = expectedGeneration + 1
	if c.UpdatedAt == "" {
		c.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	endpoint, _ := json.Marshal(c.Endpoint)
	var result sql.Result
	var err error
	if expectedGeneration == 0 {
		result, err = s.exec(`INSERT OR IGNORE INTO agent_contacts(project_id,task_id,role,name,address_kind,address_id,generation,predecessor_id,endpoint_json,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, c.ProjectID, c.TaskID, c.Role, c.Name, c.Address.Kind, c.Address.ID, c.Generation, c.PredecessorID, string(endpoint), c.UpdatedAt)
	} else {
		result, err = s.exec(`UPDATE agent_contacts SET address_kind=?,address_id=?,generation=?,predecessor_id=?,endpoint_json=?,updated_at=? WHERE project_id=? AND task_id=? AND role=? AND name=? AND generation=?`, c.Address.Kind, c.Address.ID, c.Generation, c.PredecessorID, string(endpoint), c.UpdatedAt, c.ProjectID, c.TaskID, c.Role, c.Name, expectedGeneration)
	}
	if err != nil {
		return AgentContact{}, err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return AgentContact{}, errors.New("contact generation changed; reload before replacing")
	}
	return c, nil
}

func (s *RuntimeStore) AgentContacts(projectID, taskID string) ([]AgentContact, error) {
	rows, err := s.query(`SELECT project_id,task_id,role,name,address_kind,address_id,generation,predecessor_id,endpoint_json,updated_at FROM agent_contacts WHERE project_id=? AND task_id=? ORDER BY CASE role WHEN 'architect' THEN 0 WHEN 'origin' THEN 1 ELSE 2 END,name`, projectID, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AgentContact{}
	for rows.Next() {
		var c AgentContact
		var endpoint string
		if err := rows.Scan(&c.ProjectID, &c.TaskID, &c.Role, &c.Name, &c.Address.Kind, &c.Address.ID, &c.Generation, &c.PredecessorID, &endpoint, &c.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(endpoint), &c.Endpoint)
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *RuntimeStore) ResolveAgentContact(projectID, taskID, role, name string) (AgentAddress, error) {
	contacts, err := s.AgentContacts(projectID, taskID)
	if err != nil {
		return AgentAddress{}, err
	}
	var found []AgentAddress
	for _, c := range contacts {
		if c.Role == role && (role != "peer" || c.Name == name) {
			found = append(found, c.Address)
		}
	}
	if len(found) != 1 {
		return AgentAddress{}, errors.New("contact does not resolve to one current address")
	}
	return found[0], nil
}

func parseAgentAddress(value string) (AgentAddress, error) {
	parts := strings.SplitN(strings.TrimSpace(value), ":", 2)
	if len(parts) != 2 || parts[1] == "" || (parts[0] != "task" && parts[0] != "execution") {
		return AgentAddress{}, errors.New("contact address must be task:<id> or execution:<id>")
	}
	return AgentAddress{Kind: parts[0], ID: parts[1]}, nil
}

func authoredAgentContacts(task Note) []AgentContact {
	project, taskID := stringField(task.Data, "project"), stringField(task.Data, "id")
	out := []AgentContact{}
	add := func(role, name, value string) {
		address, err := parseAgentAddress(value)
		if err == nil {
			out = append(out, AgentContact{ProjectID: project, TaskID: taskID, Role: role, Name: name, Address: address, Generation: 1})
		}
	}
	add("architect", "", stringField(task.Data, "architect"))
	add("origin", "", stringField(task.Data, "origin"))
	for name, value := range normalizeStringMap(task.Data["peer_contacts"]) {
		add("peer", name, value)
	}
	return out
}
