package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Tobse95/network-self-service/internal"
)

func main() {
	requestFile := flag.String("request", "", "Path to request YAML file (required)")
	summary := flag.Bool("summary", false, "Print a plan summary after validation")
	mappingsFile := flag.String("mappings", "mappings/environments.yaml", "Path to environment mappings YAML")
	stateFile := flag.String("state", "state/vlan-allocations.yaml", "Path to VLAN state YAML")
	flag.Parse()

	if *requestFile == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --request is required")
		flag.Usage()
		os.Exit(1)
	}

	req, err := internal.LoadRequest(*requestFile)
	if err != nil {
		fatalf("loading request: %v", err)
	}
	envs, err := internal.LoadEnvironments(*mappingsFile)
	if err != nil {
		fatalf("loading environments: %v", err)
	}
	state, err := internal.LoadState(*stateFile)
	if err != nil {
		fatalf("loading state: %v", err)
	}

	errs := internal.Validate(req, envs, state)
	if len(errs) > 0 {
		fmt.Println("VALIDATION FAILED\n")
		for _, e := range errs {
			fmt.Printf("  ✗ %s\n", e)
		}
		os.Exit(1)
	}

	fmt.Println("Validation passed.")
	if *summary {
		fmt.Println()
		fmt.Print(planSummary(req, envs, state))
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

func planSummary(req *internal.Request, envs *internal.Environments, state *internal.State) string {
	env := envs.Environments[req.Subnet.Environment]
	prefixLen, _ := internal.PrefixLength(req.Subnet.CIDR)

	vlanInfo := "auto-assigned"
	if req.Subnet.VLANID != nil {
		vlanInfo = strconv.Itoa(*req.Subnet.VLANID)
	} else {
		allocs := state.Allocations[req.Subnet.Environment]
		pool := env.F5.VLANPool
		used := make(map[int]bool, len(allocs))
		for k := range allocs {
			if n, err := strconv.Atoi(k); err == nil {
				used[n] = true
			}
		}
		for v := pool[0]; v <= pool[1]; v++ {
			if !used[v] {
				vlanInfo = fmt.Sprintf("%d (auto-assigned)", v)
				break
			}
		}
	}

	var b strings.Builder
	b.WriteString("## Self-Service Subnet — Plan\n\n")
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Subnet | `%s` |\n", req.Subnet.Name)
	fmt.Fprintf(&b, "| CIDR | `%s` |\n", req.Subnet.CIDR)
	fmt.Fprintf(&b, "| Gateway | `%s` |\n", req.Subnet.Gateway)
	fmt.Fprintf(&b, "| VLAN | `%s` |\n", vlanInfo)
	fmt.Fprintf(&b, "| Environment | `%s` (%s) |\n\n", req.Subnet.Environment, env.DisplayName)

	b.WriteString("### Pull Requests that will be opened\n\n")

	fmt.Fprintf(&b, "**ACI** (`%s/%s`)\n", env.ACI.GitHubOwner, env.ACI.GitHubRepo)
	fmt.Fprintf(&b, "- `subnets/%s.yaml` → Bridge Domain `BD_%s`, EPG `EPG_%s`\n\n",
		req.Subnet.Name, strings.ToUpper(req.Subnet.Name), strings.ToUpper(req.Subnet.Name))

	if req.Features.DNSEnabled() {
		fmt.Fprintf(&b, "**BlueCat** (`%s/%s`)\n", env.BlueCat.GitHubOwner, env.BlueCat.GitHubRepo)
		fmt.Fprintf(&b, "- `subnets/%s.yaml` → Network `%s`, host record `gw.%s.internal`\n\n",
			req.Subnet.Name, req.Subnet.CIDR, req.Subnet.Name)
	}

	fmt.Fprintf(&b, "**F5** (`%s/%s`) — one PR with %d device files\n",
		env.F5.GitHubOwner, env.F5.GitHubRepo, len(env.F5.Devices))
	for _, d := range env.F5.Devices {
		selfIP, _ := internal.OffsetIP(req.Subnet.CIDR, d.SelfIPOffset)
		fmt.Fprintf(&b, "- `%s/subnets/%s.yaml` → VLAN %s, Self-IP `%s/%d`",
			d.Folder, req.Subnet.Name, vlanInfo, selfIP, prefixLen)
		if d.Role == "loadbalancer" && req.Features.SubnetFwdEnabled() {
			b.WriteString(", AS3 forwarding VS")
		}
		lb := req.Features.LoadBalancing
		if d.Role == "loadbalancer" && lb.Enabled && strings.HasSuffix(d.Folder, "-primary") {
			fmt.Fprintf(&b, ", AS3 LB VS `%s:%d`", lb.VirtualServerIP, lb.VirtualServerPort)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n> Merge this PR to open the above pull requests in each IAC repo.\n")
	return b.String()
}
