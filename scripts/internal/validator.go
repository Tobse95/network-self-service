package internal

import (
	"fmt"
	"regexp"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// Validate checks the request against the schema and business rules.
// Returns a slice of human-readable error strings; empty means valid.
func Validate(req *Request, envs *Environments, state *State) []string {
	var errs []string

	if req.Metadata.Requester == "" {
		errs = append(errs, "metadata.requester is required")
	}
	if req.Metadata.Ticket == "" {
		errs = append(errs, "metadata.ticket is required")
	}
	if req.Subnet.Name == "" {
		errs = append(errs, "subnet.name is required")
	} else if !namePattern.MatchString(req.Subnet.Name) {
		errs = append(errs, "subnet.name must be lowercase alphanumeric with hyphens only")
	}
	if req.Subnet.Environment == "" {
		errs = append(errs, "subnet.environment is required")
	}
	if len(errs) > 0 {
		return errs
	}

	env, ok := envs.Environments[req.Subnet.Environment]
	if !ok {
		available := make([]string, 0, len(envs.Environments))
		for k := range envs.Environments {
			available = append(available, k)
		}
		errs = append(errs, fmt.Sprintf(
			"environment %q not found; available: %s",
			req.Subnet.Environment, strings.Join(available, ", "),
		))
		return errs
	}

	if req.IsRemoval() {
		// For removal: subnet must exist in state
		found := false
		for _, alloc := range state.Allocations[req.Subnet.Environment] {
			if alloc.SubnetName == req.Subnet.Name {
				if alloc.Status == "removed" {
					errs = append(errs, fmt.Sprintf("subnet %q is already removed", req.Subnet.Name))
				}
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("subnet %q not found in state for environment %q — cannot remove",
				req.Subnet.Name, req.Subnet.Environment))
		}
		return errs
	}

	// Provision path
	if req.Subnet.Size == 0 && env.SubnetPool.DefaultSize == 0 {
		errs = append(errs, "subnet.size is required (e.g. 24 for /24)")
	} else if req.Subnet.Size != 0 && (req.Subnet.Size < 8 || req.Subnet.Size > 30) {
		errs = append(errs, fmt.Sprintf("subnet.size %d is out of range (8–30)", req.Subnet.Size))
	}

	if env.SubnetPool.ParentBlock == "" {
		errs = append(errs, fmt.Sprintf("no subnet_pool configured for environment %q — contact ops", req.Subnet.Environment))
	}

	// Check for duplicate subnet name
	for _, alloc := range state.Allocations[req.Subnet.Environment] {
		if alloc.SubnetName == req.Subnet.Name && alloc.Status != "removed" {
			errs = append(errs, fmt.Sprintf("subnet name %q is already provisioned (CIDR %s)", req.Subnet.Name, alloc.CIDR))
		}
	}

	// Check VLAN pool availability
	allocs := state.Allocations[req.Subnet.Environment]
	poolStart, poolEnd := env.F5.VLANPool[0], env.F5.VLANPool[1]
	if req.Subnet.VLANID != nil {
		v := *req.Subnet.VLANID
		if v < poolStart || v > poolEnd {
			errs = append(errs, fmt.Sprintf(
				"requested VLAN %d is outside pool %d–%d for environment %q",
				v, poolStart, poolEnd, req.Subnet.Environment,
			))
		} else if existing, taken := allocs[fmt.Sprintf("%d", v)]; taken && existing.SubnetName != req.Subnet.Name && existing.Status != "removed" {
			errs = append(errs, fmt.Sprintf(
				"VLAN %d is already allocated to %q (%s)",
				v, existing.SubnetName, existing.CIDR,
			))
		}
	} else {
		activeCount := 0
		for _, a := range allocs {
			if a.Status != "removed" {
				activeCount++
			}
		}
		if activeCount >= poolEnd-poolStart+1 {
			errs = append(errs, fmt.Sprintf(
				"VLAN pool for environment %q is exhausted (%d/%d allocated)",
				req.Subnet.Environment, activeCount, poolEnd-poolStart+1,
			))
		}
	}

	for i, vs := range req.Features.VirtualServers {
		if vs.VirtualServerIP == "" {
			errs = append(errs, fmt.Sprintf("virtual_servers[%d].virtual_server_ip is required", i))
		}
		if vs.VirtualServerPort == 0 {
			errs = append(errs, fmt.Sprintf("virtual_servers[%d].virtual_server_port is required", i))
		}
		if len(vs.PoolMembers) == 0 {
			errs = append(errs, fmt.Sprintf("virtual_servers[%d].pool_members must be non-empty", i))
		}
	}

	return errs
}
