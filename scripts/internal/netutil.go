package internal

import (
	"encoding/binary"
	"fmt"
	"net"
)

// AllocateFromBlock finds the first subnet of prefixLen within parentBlock
// that does not overlap any entry in usedCIDRs.
// Returns the allocated CIDR and its gateway (network address + 1).
func AllocateFromBlock(parentBlock string, prefixLen int, usedCIDRs []string) (cidr, gateway string, err error) {
	_, parent, err := net.ParseCIDR(parentBlock)
	if err != nil {
		return "", "", fmt.Errorf("invalid parent block %q: %w", parentBlock, err)
	}
	parentOnes, bits := parent.Mask.Size()
	if bits != 32 {
		return "", "", fmt.Errorf("only IPv4 is supported")
	}
	if prefixLen <= parentOnes || prefixLen > 30 {
		return "", "", fmt.Errorf("requested /%d is invalid within parent /%d", prefixLen, parentOnes)
	}

	step := uint32(1) << uint(bits-prefixLen)
	parentStart := binary.BigEndian.Uint32(parent.IP.To4())
	parentEnd := parentStart | ^binary.BigEndian.Uint32([]byte(parent.Mask))

	for n := parentStart; n+step-1 <= parentEnd; n += step {
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, n)
		candidate := fmt.Sprintf("%s/%d", ip.String(), prefixLen)

		overlaps := false
		for _, used := range usedCIDRs {
			if used == "" {
				continue
			}
			if ok, _ := CIDRsOverlap(candidate, used); ok {
				overlaps = true
				break
			}
		}
		if !overlaps {
			gw, err := OffsetIP(candidate, 1)
			if err != nil {
				return "", "", err
			}
			return candidate, gw, nil
		}

		if n+step < n { // overflow guard
			break
		}
	}
	return "", "", fmt.Errorf("no free /%d subnet available in %s", prefixLen, parentBlock)
}

func PrefixLength(cidr string) (int, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	ones, _ := network.Mask.Size()
	return ones, nil
}

func NetworkAddress(cidr string) (string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	return network.IP.String(), nil
}

func OffsetIP(cidr string, offset int) (string, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	ip := network.IP.To4()
	if ip == nil {
		return "", fmt.Errorf("only IPv4 is supported (got %q)", cidr)
	}
	n := binary.BigEndian.Uint32(ip)
	n += uint32(offset)
	result := make(net.IP, 4)
	binary.BigEndian.PutUint32(result, n)
	return result.String(), nil
}

func GatewayInCIDR(gateway, cidr string) error {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	gw := net.ParseIP(gateway)
	if gw == nil {
		return fmt.Errorf("invalid gateway IP %q", gateway)
	}
	if !network.Contains(gw) {
		return fmt.Errorf("gateway %s is not within CIDR %s", gateway, cidr)
	}
	return nil
}

func CIDRsOverlap(a, b string) (bool, error) {
	_, netA, err := net.ParseCIDR(a)
	if err != nil {
		return false, fmt.Errorf("invalid CIDR %q: %w", a, err)
	}
	_, netB, err := net.ParseCIDR(b)
	if err != nil {
		return false, fmt.Errorf("invalid CIDR %q: %w", b, err)
	}
	return netA.Contains(netB.IP) || netB.Contains(netA.IP), nil
}
