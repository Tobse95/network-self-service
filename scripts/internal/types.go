package internal

// Request is the customer-facing subnet-request YAML.
type Request struct {
	Action   string          `yaml:"action"` // "provision" (default) or "remove"
	Metadata RequestMetadata `yaml:"metadata"`
	Subnet   SubnetSpec      `yaml:"subnet"`
	Features Features        `yaml:"features"`
}

func (r *Request) IsRemoval() bool {
	return r.Action == "remove"
}

type RequestMetadata struct {
	Requester   string `yaml:"requester"`
	Ticket      string `yaml:"ticket"`
	Description string `yaml:"description"`
}

type SubnetSpec struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment"`
	Size        int    `yaml:"size"`
	VLANID      *int   `yaml:"vlan_id"`
	// Populated at runtime by AllocateSubnet — not read from YAML.
	CIDR    string `yaml:"-"`
	Gateway string `yaml:"-"`
}

type Features struct {
	DNS            *bool           `yaml:"dns"`
	VirtualServers []VirtualServer `yaml:"virtual_servers"`
}

func (f Features) DNSEnabled() bool {
	return f.DNS == nil || *f.DNS
}

type VirtualServer struct {
	VirtualServerIP   string       `yaml:"virtual_server_ip"`
	VirtualServerPort int          `yaml:"virtual_server_port"`
	Protocol          string       `yaml:"protocol"`
	PoolMembers       []PoolMember `yaml:"pool_members"`
}

type PoolMember struct {
	IP   string `yaml:"ip"`
	Port int    `yaml:"port"`
}

// Environments is the ops-managed mappings/environments.yaml.
type Environments struct {
	Environments map[string]Environment `yaml:"environments"`
}

type Environment struct {
	DisplayName string           `yaml:"display_name"`
	SubnetPool  SubnetPoolConfig `yaml:"subnet_pool"`
	ACI         ACIConfig        `yaml:"aci"`
	BlueCat     BlueCatConfig    `yaml:"bluecat"`
	F5          F5Config         `yaml:"f5"`
}

type SubnetPoolConfig struct {
	ParentBlock string `yaml:"parent_block"`
	DefaultSize int    `yaml:"default_size"`
}

type ACIConfig struct {
	GitHubOwner string `yaml:"github_owner"`
	GitHubRepo  string `yaml:"github_repo"`
	Tenant      string `yaml:"tenant"`
	VRF         string `yaml:"vrf"`
	AP          string `yaml:"ap"`
	Domain      string `yaml:"domain"`
}

type BlueCatConfig struct {
	GitHubOwner   string `yaml:"github_owner"`
	GitHubRepo    string `yaml:"github_repo"`
	Configuration string `yaml:"configuration"`
	View          string `yaml:"view"`
	ParentBlock   string `yaml:"parent_block"`
}

type F5Config struct {
	GitHubOwner string     `yaml:"github_owner"`
	GitHubRepo  string     `yaml:"github_repo"`
	VLANPool    [2]int     `yaml:"vlan_pool"`
	Devices     []F5Device `yaml:"devices"`
}

type F5Device struct {
	Folder               string      `yaml:"folder"`
	Role                 string      `yaml:"role"`
	SelfIPOffset         int         `yaml:"selfip_offset"`
	FloatingSelfIPOffset int         `yaml:"floating_selfip_offset"`
	Interfaces           []Interface `yaml:"interfaces"`
}

type Interface struct {
	Name   string `yaml:"name"`
	Tagged bool   `yaml:"tagged"`
}

// State is the auto-managed state/vlan-allocations.yaml.
type State struct {
	Allocations map[string]map[string]VLANAllocation `yaml:"allocations"`
}

type VLANAllocation struct {
	SubnetName  string            `yaml:"subnet_name"`
	Environment string            `yaml:"environment"`
	CIDR        string            `yaml:"cidr"`
	RequestFile string            `yaml:"request_file"`
	AllocatedAt string            `yaml:"allocated_at"`
	Status      string            `yaml:"status"`
	ActiveSince string            `yaml:"active_since,omitempty"`
	PRs         map[string]string `yaml:"prs,omitempty"`
	Platforms   map[string]string `yaml:"platforms,omitempty"`
}
