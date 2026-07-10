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
	if req.Subnet.CIDR == "" {
		errs = append(errs, "subnet.cidr is required")
	}
	if req.Subnet.Gateway == "" {
		errs = append(errs, "subnet.gateway is required")
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

	if err := GatewayInCIDR(req.Subnet.Gateway, req.Subnet.CIDR); err != nil {
		errs = append(errs, err.Error())
	}

	for vlanStr, alloc := range state.Allocations[req.Subnet.Environment] {
		overlap, err := CIDRsOverlap(req.Subnet.CIDR, alloc.CIDR)
		if err != nil || !overlap {
			continue
		}
		errs = append(errs, fmt.Sprintf(
			"CIDR %s overlaps with existing subnet %q (%s) on VLAN %s",
			req.Subnet.CIDR, alloc.SubnetName, alloc.CIDR, vlanStr,
		))
	}

	poolStart, poolEnd := env.F5.VLANPool[0], env.F5.VLANPool[1]
	allocs := state.Allocations[req.Subnet.Environment]

	if req.Subnet.VLANID != nil {
		v := *req.Subnet.VLANID
		if v < poolStart || v > poolEnd {
			errs = append(errs, fmt.Sprintf(
				"requested VLAN %d is outside pool %d–%d for environment %q",
				v, poolStart, poolEnd, req.Subnet.Environment,
			))
		} else if existing, taken := allocs[fmt.Sprintf("%d", v)]; taken && existing.SubnetName != req.Subnet.Name {
			errs = append(errs, fmt.Sprintf(
				"VLAN %d is already allocated to %q (%s)",
				v, existing.SubnetName, existing.CIDR,
			))
		}
	} else if len(allocs) >= poolEnd-poolStart+1 {
		errs = append(errs, fmt.Sprintf(
			"VLAN pool for environment %q is exhausted (%d/%d allocated)",
			req.Subnet.Environment, len(allocs), poolEnd-poolStart+1,
		))
	}

	lb := req.Features.LoadBalancing
	if lb.Enabled {
		if lb.VirtualServerIP == "" {
			errs = append(errs, "features.load_balancing.virtual_server_ip is required when enabled")
		}
		if len(lb.PoolMembers) == 0 {
			errs = append(errs, "features.load_balancing.pool_members must be non-empty when enabled")
		}
	}

	return errs
}
