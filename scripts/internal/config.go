package internal

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

func LoadRequest(path string) (*Request, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading request file: %w", err)
	}
	var req Request
	if err := yaml.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("parsing request YAML: %w", err)
	}
	return &req, nil
}

func LoadEnvironments(path string) (*Environments, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading environments file: %w", err)
	}
	var envs Environments
	if err := yaml.Unmarshal(data, &envs); err != nil {
		return nil, fmt.Errorf("parsing environments YAML: %w", err)
	}
	return &envs, nil
}

func LoadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &State{Allocations: map[string]map[string]VLANAllocation{}}, nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}
	var state State
	if err := yaml.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state YAML: %w", err)
	}
	if state.Allocations == nil {
		state.Allocations = map[string]map[string]VLANAllocation{}
	}
	return &state, nil
}

func SaveState(path string, state *State) error {
	data, err := yaml.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing state file: %w", err)
	}
	return nil
}
