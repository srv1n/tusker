package main

import ()

type closePolicyMigrationReport struct {
	WorkflowPath string   `json:"workflow_path"`
	ConfigPath   string   `json:"config_path"`
	Changed      []string `json:"changed"`
	Write        bool     `json:"write"`
}
