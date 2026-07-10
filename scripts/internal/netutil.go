package internal

import (
	"encoding/binary"
	"fmt"
	"net"
)

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
