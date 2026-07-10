package internal

import (
	"fmt"
	"strconv"
	"time"
)

// AllocateVLAN returns a free VLAN ID for the request.
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

// AllocateSubnet finds the next free subnet of prefixLen within the environment's
// subnet pool. Does not mutate state — call RecordVLAN to persist.
func AllocateSubnet(envName string, env *Environment, prefixLen int, state *State) (cidr, gateway string, err error) {
	pool := env.SubnetPool.ParentBlock
	if pool == "" {
		return "", "", fmt.Errorf("no subnet_pool.parent_block configured for environment %q", envName)
	}
	if prefixLen == 0 {
		if env.SubnetPool.DefaultSize == 0 {
			return "", "", fmt.Errorf("subnet size is required (no default configured for %q)", envName)
		}
		prefixLen = env.SubnetPool.DefaultSize
	}

	var usedCIDRs []string
	for _, alloc := range state.Allocations[envName] {
		if alloc.CIDR != "" && alloc.Status != "removed" {
			usedCIDRs = append(usedCIDRs, alloc.CIDR)
		}
	}
	return AllocateFromBlock(pool, prefixLen, usedCIDRs)
}

// RecordVLAN persists a VLAN + subnet allocation in state. Call after successful IAC PRs are opened.
func RecordVLAN(envName string, vlanID int, req *Request, requestFile string, state *State, prs map[string]string, platforms []string) {
	if state.Allocations == nil {
		state.Allocations = map[string]map[string]VLANAllocation{}
	}
	if state.Allocations[envName] == nil {
		state.Allocations[envName] = map[string]VLANAllocation{}
	}
	platformStatus := make(map[string]string, len(platforms))
	for _, p := range platforms {
		platformStatus[p] = "pending"
	}
	state.Allocations[envName][strconv.Itoa(vlanID)] = VLANAllocation{
		SubnetName:  req.Subnet.Name,
		Environment: envName,
		CIDR:        req.Subnet.CIDR,
		RequestFile: requestFile,
		AllocatedAt: time.Now().UTC().Format(time.RFC3339),
		Status:      "pending",
		PRs:         prs,
		Platforms:   platformStatus,
	}
}

// MarkPlatformActive marks a platform as active for a subnet. If all platforms
// are active the overall status transitions to "active".
// Returns an error if the subnet is not found.
func MarkPlatformActive(envName, subnetName, platform string, state *State) error {
	for vlanStr, alloc := range state.Allocations[envName] {
		if alloc.SubnetName != subnetName {
			continue
		}
		if alloc.Platforms == nil {
			alloc.Platforms = map[string]string{}
		}
		alloc.Platforms[platform] = "active"
		allReady := true
		for _, s := range alloc.Platforms {
			if s != "active" {
				allReady = false
				break
			}
		}
		if allReady && alloc.Status != "active" {
			alloc.Status = "active"
			alloc.ActiveSince = time.Now().UTC().Format(time.RFC3339)
		}
		state.Allocations[envName][vlanStr] = alloc
		return nil
	}
	return fmt.Errorf("subnet %q not found in environment %q", subnetName, envName)
}

// RemoveSubnet marks a subnet as removed and returns its VLAN ID (for the caller to clean up files).
func RemoveSubnet(envName, subnetName string, state *State) (vlanID int, alloc VLANAllocation, err error) {
	for vlanStr, a := range state.Allocations[envName] {
		if a.SubnetName != subnetName {
			continue
		}
		n, _ := strconv.Atoi(vlanStr)
		a.Status = "removed"
		state.Allocations[envName][vlanStr] = a
		return n, a, nil
	}
	return 0, VLANAllocation{}, fmt.Errorf("subnet %q not found in environment %q", subnetName, envName)
}
