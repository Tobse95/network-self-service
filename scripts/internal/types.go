package internal

// Request is the customer-facing subnet-request YAML.
type Request struct {
	Metadata RequestMetadata `yaml:"metadata"`
	Subnet   SubnetSpec      `yaml:"subnet"`
	Features Features        `yaml:"features"`
}

type RequestMetadata struct {
	Requester   string `yaml:"requester"`
	Ticket      string `yaml:"ticket"`
	Description string `yaml:"description"`
}

type SubnetSpec struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment"`
	CIDR        string `yaml:"cidr"`
	Gateway     string `yaml:"gateway"`
	VLANID      *int   `yaml:"vlan_id"`
}

type Features struct {
	DNS              *bool         `yaml:"dns"`
	SubnetForwarding *bool         `yaml:"subnet_forwarding"`
	LoadBalancing    LoadBalancing `yaml:"load_balancing"`
}

func (f Features) DNSEnabled() bool {
	return f.DNS == nil || *f.DNS
}

func (f Features) SubnetFwdEnabled() bool {
	return f.SubnetForwarding == nil || *f.SubnetForwarding
}

type LoadBalancing struct {
	Enabled           bool         `yaml:"enabled"`
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
	DisplayName string        `yaml:"display_name"`
	ACI         ACIConfig     `yaml:"aci"`
	BlueCat     BlueCatConfig `yaml:"bluecat"`
	F5          F5Config      `yaml:"f5"`
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
	SubnetName  string `yaml:"subnet_name"`
	CIDR        string `yaml:"cidr"`
	RequestFile string `yaml:"request_file"`
	AllocatedAt string `yaml:"allocated_at"`
}
