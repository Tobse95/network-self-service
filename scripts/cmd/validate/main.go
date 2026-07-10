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
	stateFile := flag.String("state", "state/vlan-allocations.yaml", "Path to state YAML")
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
		fmt.Println("VALIDATION FAILED")
		fmt.Println()
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

	if req.IsRemoval() {
		return removalPlanSummary(req, &env, state)
	}
	return provisionPlanSummary(req, &env, state)
}

func provisionPlanSummary(req *internal.Request, env *internal.Environment, state *internal.State) string {
	// Do a read-only allocation to preview what WILL be allocated.
	size := req.Subnet.Size
	if size == 0 {
		size = env.SubnetPool.DefaultSize
	}
	cidr, gateway, allocErr := internal.AllocateSubnet(req.Subnet.Environment, env, size, state)

	vlanInfo := nextVLAN(env, state, req.Subnet.Environment)

	var b strings.Builder
	b.WriteString("## Self-Service Subnet — Provision Plan\n\n")
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Subnet | `%s` |\n", req.Subnet.Name)
	fmt.Fprintf(&b, "| Size | `/%d` |\n", size)
	if allocErr != nil {
		fmt.Fprintf(&b, "| CIDR | ⚠️ allocation error: %s |\n", allocErr)
	} else {
		fmt.Fprintf(&b, "| CIDR | `%s` (auto-allocated from `%s`) |\n", cidr, env.SubnetPool.ParentBlock)
		fmt.Fprintf(&b, "| Gateway | `%s` |\n", gateway)
	}
	fmt.Fprintf(&b, "| VLAN | `%s` |\n", vlanInfo)
	fmt.Fprintf(&b, "| Environment | `%s` (%s) |\n\n", req.Subnet.Environment, env.DisplayName)

	b.WriteString("### Pull Requests that will be opened\n\n")

	fmt.Fprintf(&b, "**ACI** (`%s/%s`)\n", env.ACI.GitHubOwner, env.ACI.GitHubRepo)
	fmt.Fprintf(&b, "- `subnets/%s.yaml` → Bridge Domain `BD_%s`, EPG `EPG_%s`\n\n",
		req.Subnet.Name, strings.ToUpper(req.Subnet.Name), strings.ToUpper(req.Subnet.Name))

	if req.Features.DNSEnabled() {
		fmt.Fprintf(&b, "**BlueCat** (`%s/%s`)\n", env.BlueCat.GitHubOwner, env.BlueCat.GitHubRepo)
		if allocErr == nil {
			fmt.Fprintf(&b, "- `subnets/%s.yaml` → Network `%s`, host record `gw.%s.internal`\n\n",
				req.Subnet.Name, cidr, req.Subnet.Name)
		} else {
			fmt.Fprintf(&b, "- `subnets/%s.yaml` → DNS host record\n\n", req.Subnet.Name)
		}
	}

	fmt.Fprintf(&b, "**F5** (`%s/%s`) — one PR with %d device files\n",
		env.F5.GitHubOwner, env.F5.GitHubRepo, len(env.F5.Devices))
	for _, d := range env.F5.Devices {
		if allocErr == nil {
			selfIP, _ := internal.OffsetIP(cidr, d.SelfIPOffset)
			fmt.Fprintf(&b, "- `%s/subnets/%s.yaml` → VLAN %s, Self-IP `%s/%d`, AS3 forwarding VS",
				d.Folder, req.Subnet.Name, vlanInfo, selfIP, size)
		} else {
			fmt.Fprintf(&b, "- `%s/subnets/%s.yaml` → AS3 forwarding VS", d.Folder, req.Subnet.Name)
		}
		if d.Role == "loadbalancer" && len(req.Features.VirtualServers) > 0 {
			for _, vs := range req.Features.VirtualServers {
				fmt.Fprintf(&b, ", LB VS `%s:%d`", vs.VirtualServerIP, vs.VirtualServerPort)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("\n> Merge this PR to open the above pull requests in each IAC repo.\n")
	return b.String()
}

func removalPlanSummary(req *internal.Request, env *internal.Environment, state *internal.State) string {
	var alloc internal.VLANAllocation
	for _, a := range state.Allocations[req.Subnet.Environment] {
		if a.SubnetName == req.Subnet.Name {
			alloc = a
			break
		}
	}

	var b strings.Builder
	b.WriteString("## Self-Service Subnet — Removal Plan\n\n")
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Subnet | `%s` |\n", req.Subnet.Name)
	fmt.Fprintf(&b, "| CIDR | `%s` |\n", alloc.CIDR)
	fmt.Fprintf(&b, "| Environment | `%s` (%s) |\n", req.Subnet.Environment, env.DisplayName)
	fmt.Fprintf(&b, "| Current status | `%s` |\n\n", alloc.Status)

	b.WriteString("### Pull Requests that will be opened\n\n")
	fmt.Fprintf(&b, "- **ACI** (`%s/%s`) — delete `subnets/%s.yaml`\n",
		env.ACI.GitHubOwner, env.ACI.GitHubRepo, req.Subnet.Name)
	fmt.Fprintf(&b, "- **BlueCat** (`%s/%s`) — delete `subnets/%s.yaml`\n",
		env.BlueCat.GitHubOwner, env.BlueCat.GitHubRepo, req.Subnet.Name)
	fmt.Fprintf(&b, "- **F5** (`%s/%s`) — delete %d device subnet files\n",
		env.F5.GitHubOwner, env.F5.GitHubRepo, len(env.F5.Devices))

	b.WriteString("\n> Merge this PR to open the above removal pull requests in each IAC repo.\n")
	return b.String()
}

func nextVLAN(env *internal.Environment, state *internal.State, envName string) string {
	allocs := state.Allocations[envName]
	pool := env.F5.VLANPool
	used := make(map[int]bool, len(allocs))
	for k, a := range allocs {
		if a.Status != "removed" {
			if n, err := strconv.Atoi(k); err == nil {
				used[n] = true
			}
		}
	}
	for v := pool[0]; v <= pool[1]; v++ {
		if !used[v] {
			return fmt.Sprintf("%d (auto-assigned)", v)
		}
	}
	return "pool exhausted"
}
