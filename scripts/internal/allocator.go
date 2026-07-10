package internal

import (
	"fmt"
	"strconv"
	"time"
)

// AllocateVLAN returns a free VLAN ID for the request.
// Uses the explicitly requested VLAN if provided (after validating it's free),
// otherwise picks the next available ID from the environment's pool.
func AllocateVLAN(envName string, env *Environment, req *Request, state *State) (int, error) {
	if state.Allocations == nil {
		state.Allocations = map[string]map[string]VLANAllocation{}
	}
	if state.Allocations[envName] == nil {
		state.Allocations[envName] = map[string]VLANAllocation{}
	}

	allocs := state.Allocations[envName]
	poolStart, poolEnd := env.F5.VLANPool[0], env.F5.VLANPool[1]

	if req.Subnet.VLANID != nil {
		v := *req.Subnet.VLANID
		if v < poolStart || v > poolEnd {
			return 0, fmt.Errorf("requested VLAN %d is outside pool %d–%d for %q", v, poolStart, poolEnd, envName)
		}
		if existing, taken := allocs[strconv.Itoa(v)]; taken && existing.SubnetName != req.Subnet.Name {
			return 0, fmt.Errorf("VLAN %d is already allocated to %q", v, existing.SubnetName)
		}
		return v, nil
	}

	used := make(map[int]bool, len(allocs))
	for k := range allocs {
		if n, err := strconv.Atoi(k); err == nil {
			used[n] = true
		}
	}
	for v := poolStart; v <= poolEnd; v++ {
		if !used[v] {
			return v, nil
		}
	}
	return 0, fmt.Errorf("no free VLANs in pool %d–%d for %q", poolStart, poolEnd, envName)
}

// RecordVLAN persists a VLAN allocation in state. Call after a successful orchestration.
func RecordVLAN(envName string, vlanID int, req *Request, requestFile string, state *State) {
	if state.Allocations == nil {
		state.Allocations = map[string]map[string]VLANAllocation{}
	}
	if state.Allocations[envName] == nil {
		state.Allocations[envName] = map[string]VLANAllocation{}
	}
	state.Allocations[envName][strconv.Itoa(vlanID)] = VLANAllocation{
		SubnetName:  req.Subnet.Name,
		CIDR:        req.Subnet.CIDR,
		RequestFile: requestFile,
		AllocatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}
