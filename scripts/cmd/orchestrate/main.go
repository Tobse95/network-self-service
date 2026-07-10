package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tobse95/network-self-service/internal"
)

type prResult struct {
	label string
	url   string
}

func main() {
	requestFile := flag.String("request", "", "Path to request YAML file")
	dryRun := flag.Bool("dry-run", false, "Generate files and print without opening PRs")
	mappingsFile := flag.String("mappings", "mappings/environments.yaml", "Path to environment mappings YAML")
	stateFile := flag.String("state", "state/vlan-allocations.yaml", "Path to state YAML")
	markActive := flag.String("mark-active", "", "Mark a subnet as active (format: <env>/<subnet>/<platform>)")
	actionOverride := flag.String("action", "", "Override action from request file: provision | remove")
	flag.Parse()

	if *markActive != "" {
		if err := runMarkActive(*markActive, *stateFile); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *requestFile == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --request is required")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(*requestFile, *mappingsFile, *stateFile, *dryRun, *actionOverride); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func runMarkActive(spec, stateFile string) error {
	parts := strings.SplitN(spec, "/", 3)
	if len(parts) != 3 {
		return fmt.Errorf("--mark-active expects env/subnet/platform, got %q", spec)
	}
	envName, subnetName, platform := parts[0], parts[1], parts[2]

	state, err := internal.LoadState(stateFile)
	if err != nil {
		return err
	}
	if err := internal.MarkPlatformActive(envName, subnetName, platform, state); err != nil {
		return err
	}
	if err := internal.SaveState(stateFile, state); err != nil {
		return err
	}
	fmt.Printf("[Status] subnet %q platform %q marked active in %s\n", subnetName, platform, stateFile)
	return nil
}

func run(requestFile, mappingsFile, stateFile string, dryRun bool, actionOverride string) error {
	req, err := internal.LoadRequest(requestFile)
	if err != nil {
		return err
	}
	if actionOverride != "" {
		req.Action = actionOverride
	}
	envs, err := internal.LoadEnvironments(mappingsFile)
	if err != nil {
		return err
	}
	state, err := internal.LoadState(stateFile)
	if err != nil {
		return err
	}

	envName := req.Subnet.Environment
	env, ok := envs.Environments[envName]
	if !ok {
		return fmt.Errorf("environment %q not found in mappings", envName)
	}

	if req.IsRemoval() {
		return runRemoval(req, &env, envName, stateFile, state, dryRun)
	}
	return runProvision(req, &env, envName, requestFile, stateFile, state, dryRun)
}

func runProvision(req *internal.Request, env *internal.Environment, envName, requestFile, stateFile string, state *internal.State, dryRun bool) error {
	// Auto-allocate subnet from pool
	size := req.Subnet.Size
	cidr, gateway, err := internal.AllocateSubnet(envName, env, size, state)
	if err != nil {
		return fmt.Errorf("subnet allocation: %w", err)
	}
	req.Subnet.CIDR = cidr
	req.Subnet.Gateway = gateway

	prefixLen, _ := internal.PrefixLength(cidr)

	vlanID, err := internal.AllocateVLAN(envName, env, req, state)
	if err != nil {
		return err
	}

	branchName := fmt.Sprintf("self-service/subnet-%s-%s",
		req.Subnet.Name, time.Now().UTC().Format("20060102150405"))
	mrTitle := fmt.Sprintf("feat: subnet %s (%s) [%s]", req.Subnet.Name, cidr, req.Metadata.Ticket)
	mrBody := buildPRBody(req, envName, vlanID)

	var ghClient *internal.GitHubClient
	if !dryRun {
		token := os.Getenv("SELF_SERVICE_GITHUB_TOKEN")
		if token == "" {
			return fmt.Errorf("SELF_SERVICE_GITHUB_TOKEN is not set")
		}
		ghClient = internal.NewGitHubClient(token, env.ACI.GitHubOwner)
	}

	prs := map[string]string{}
	platforms := []string{}
	var results []prResult

	// --- ACI ---
	fmt.Printf("\n[ACI] Generating vars for %s/%s\n", env.ACI.GitHubOwner, env.ACI.GitHubRepo)
	aciPath, aciContent, err := internal.BuildACIVars(req, env, vlanID, prefixLen)
	if err != nil {
		return fmt.Errorf("ACI vars: %w", err)
	}
	if dryRun {
		printDryRun(aciPath, aciContent)
	} else {
		url, err := ghClient.CreateBranchAndPR(env.ACI.GitHubRepo, branchName,
			map[string]string{aciPath: aciContent}, mrTitle, mrBody)
		if err != nil {
			return fmt.Errorf("ACI PR: %w", err)
		}
		results = append(results, prResult{"ACI", url})
		prs["aci"] = url
		platforms = append(platforms, "aci")
	}

	// --- BlueCat ---
	if req.Features.DNSEnabled() {
		fmt.Printf("\n[BlueCat] Generating vars for %s/%s\n", env.BlueCat.GitHubOwner, env.BlueCat.GitHubRepo)
		bcPath, bcContent, err := internal.BuildBlueCatVars(req, env)
		if err != nil {
			return fmt.Errorf("BlueCat vars: %w", err)
		}
		if dryRun {
			printDryRun(bcPath, bcContent)
		} else {
			bcClient := internal.NewGitHubClient(os.Getenv("SELF_SERVICE_GITHUB_TOKEN"), env.BlueCat.GitHubOwner)
			url, err := bcClient.CreateBranchAndPR(env.BlueCat.GitHubRepo, branchName,
				map[string]string{bcPath: bcContent}, mrTitle, mrBody)
			if err != nil {
				return fmt.Errorf("BlueCat PR: %w", err)
			}
			results = append(results, prResult{"BlueCat", url})
			prs["bluecat"] = url
			platforms = append(platforms, "bluecat")
		}
	}

	// --- F5 — all device files in one PR ---
	fmt.Printf("\n[F5] Generating vars for %s/%s (%d devices)\n",
		env.F5.GitHubOwner, env.F5.GitHubRepo, len(env.F5.Devices))
	f5Files := make(map[string]string, len(env.F5.Devices))
	for i := range env.F5.Devices {
		device := &env.F5.Devices[i]
		deviceSelfIP, err := internal.OffsetIP(cidr, device.SelfIPOffset)
		if err != nil {
			return fmt.Errorf("computing self-IP for %s: %w", device.Folder, err)
		}
		floatingSelfIP, err := internal.OffsetIP(cidr, device.FloatingSelfIPOffset)
		if err != nil {
			return fmt.Errorf("computing floating self-IP for %s: %w", device.Folder, err)
		}
		path, content, err := internal.BuildF5Vars(req, device, vlanID, prefixLen, deviceSelfIP, floatingSelfIP)
		if err != nil {
			return fmt.Errorf("F5 vars for %s: %w", device.Folder, err)
		}
		f5Files[path] = content
		fmt.Printf("  + %s\n", path)
	}
	if dryRun {
		for p, c := range f5Files {
			printDryRun(p, c)
		}
	} else {
		f5Client := internal.NewGitHubClient(os.Getenv("SELF_SERVICE_GITHUB_TOKEN"), env.F5.GitHubOwner)
		url, err := f5Client.CreateBranchAndPR(env.F5.GitHubRepo, branchName, f5Files, mrTitle, mrBody)
		if err != nil {
			return fmt.Errorf("F5 PR: %w", err)
		}
		results = append(results, prResult{"F5", url})
		prs["f5"] = url
		platforms = append(platforms, "f5")
	}

	// --- Persist allocation ---
	if !dryRun {
		internal.RecordVLAN(envName, vlanID, req, requestFile, state, prs, platforms)
		if err := internal.SaveState(stateFile, state); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		fmt.Printf("\n[State] VLAN %d / %s recorded in %s\n", vlanID, cidr, stateFile)
	}

	printSummary(req, envName, vlanID, cidr, results, dryRun)
	writeStepSummary(req, vlanID, cidr, envName, results)
	return nil
}

func runRemoval(req *internal.Request, env *internal.Environment, envName, stateFile string, state *internal.State, dryRun bool) error {
	_, alloc, err := internal.RemoveSubnet(envName, req.Subnet.Name, state)
	if err != nil {
		return err
	}

	branchName := fmt.Sprintf("self-service/remove-%s-%s",
		req.Subnet.Name, time.Now().UTC().Format("20060102150405"))
	mrTitle := fmt.Sprintf("feat: remove subnet %s (%s) [%s]", req.Subnet.Name, alloc.CIDR, req.Metadata.Ticket)
	mrBody := buildRemovalPRBody(req, envName, alloc)

	var results []prResult

	if !dryRun {
		token := os.Getenv("SELF_SERVICE_GITHUB_TOKEN")
		if token == "" {
			return fmt.Errorf("SELF_SERVICE_GITHUB_TOKEN is not set")
		}

		// ACI
		aciClient := internal.NewGitHubClient(token, env.ACI.GitHubOwner)
		url, err := aciClient.DeleteFilesAndPR(env.ACI.GitHubRepo, branchName,
			[]string{"subnets/" + req.Subnet.Name + ".yaml"}, mrTitle, mrBody)
		if err != nil {
			return fmt.Errorf("ACI removal PR: %w", err)
		}
		results = append(results, prResult{"ACI", url})

		// BlueCat
		bcClient := internal.NewGitHubClient(token, env.BlueCat.GitHubOwner)
		url, err = bcClient.DeleteFilesAndPR(env.BlueCat.GitHubRepo, branchName,
			[]string{"subnets/" + req.Subnet.Name + ".yaml"}, mrTitle, mrBody)
		if err != nil {
			return fmt.Errorf("BlueCat removal PR: %w", err)
		}
		results = append(results, prResult{"BlueCat", url})

		// F5 — all device files
		f5Paths := make([]string, 0, len(env.F5.Devices))
		for _, d := range env.F5.Devices {
			f5Paths = append(f5Paths, d.Folder+"/subnets/"+req.Subnet.Name+".yaml")
		}
		f5Client := internal.NewGitHubClient(token, env.F5.GitHubOwner)
		url, err = f5Client.DeleteFilesAndPR(env.F5.GitHubRepo, branchName, f5Paths, mrTitle, mrBody)
		if err != nil {
			return fmt.Errorf("F5 removal PR: %w", err)
		}
		results = append(results, prResult{"F5", url})

		if err := internal.SaveState(stateFile, state); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		fmt.Printf("\n[State] subnet %q marked as removing in %s\n", req.Subnet.Name, stateFile)
	} else {
		fmt.Printf("\n[dry-run] Would delete:\n")
		fmt.Printf("  ACI:     subnets/%s.yaml\n", req.Subnet.Name)
		fmt.Printf("  BlueCat: subnets/%s.yaml\n", req.Subnet.Name)
		for _, d := range env.F5.Devices {
			fmt.Printf("  F5:      %s/subnets/%s.yaml\n", d.Folder, req.Subnet.Name)
		}
	}

	sep := strings.Repeat("=", 60)
	fmt.Printf("\n%s\nRemoving subnet '%s' (%s)\n", sep, req.Subnet.Name, alloc.CIDR)
	if !dryRun {
		for _, r := range results {
			fmt.Printf("  %s: %s\n", r.label, r.url)
		}
	}
	fmt.Println(sep)

	writeRemovalStepSummary(req, alloc, results)
	return nil
}

func printDryRun(path, content string) {
	fmt.Printf("\n  [dry-run] %s\n%s\n", path, content)
}

func printSummary(req *internal.Request, envName string, vlanID int, cidr string, results []prResult, dryRun bool) {
	sep := strings.Repeat("=", 60)
	fmt.Printf("\n%s\nSubnet '%s' (%s) — VLAN %d\nEnvironment: %s\n",
		sep, req.Subnet.Name, cidr, vlanID, envName)
	if dryRun {
		fmt.Println("DRY RUN — no changes made")
	} else {
		fmt.Println("Pull requests created:")
		for _, r := range results {
			fmt.Printf("  %s: %s\n", r.label, r.url)
		}
	}
	fmt.Println(sep)
}

func buildPRBody(req *internal.Request, envName string, vlanID int) string {
	return fmt.Sprintf(`## Self-Service Subnet Request

| Field | Value |
|---|---|
| Requester | %s |
| Ticket | %s |
| Subnet | %s |
| CIDR | %s |
| Gateway | %s |
| VLAN | %d |
| Environment | %s |

Variable file generated automatically by the [self-service pipeline](https://github.com/Tobse95/network-self-service).
The Terraform in this repo reads all files in **subnets/** and plans/applies them automatically.

> Review the variable values before approving.`,
		req.Metadata.Requester,
		req.Metadata.Ticket,
		req.Subnet.Name,
		req.Subnet.CIDR,
		req.Subnet.Gateway,
		vlanID,
		envName,
	)
}

func buildRemovalPRBody(req *internal.Request, envName string, alloc internal.VLANAllocation) string {
	return fmt.Sprintf(`## Self-Service Subnet Removal

| Field | Value |
|---|---|
| Requester | %s |
| Ticket | %s |
| Subnet | %s |
| CIDR | %s |
| Environment | %s |
| Originally provisioned | %s |

Removing subnet variable file. Merging this PR will trigger Terraform to decommission the subnet.`,
		req.Metadata.Requester,
		req.Metadata.Ticket,
		req.Subnet.Name,
		alloc.CIDR,
		envName,
		alloc.AllocatedAt,
	)
}

func writeStepSummary(req *internal.Request, vlanID int, cidr, envName string, results []prResult) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "## Subnet `%s` provisioned\n\n", req.Subnet.Name)
	fmt.Fprintf(f, "| | |\n|---|---|\n")
	fmt.Fprintf(f, "| CIDR | `%s` |\n| Gateway | `%s` |\n| VLAN | `%d` |\n| Environment | `%s` |\n\n",
		cidr, req.Subnet.Gateway, vlanID, envName)
	fmt.Fprintf(f, "### IAC Pull Requests (pending review)\n\n")
	for _, r := range results {
		fmt.Fprintf(f, "- [%s](%s)\n", r.label, r.url)
	}
}

func writeRemovalStepSummary(req *internal.Request, alloc internal.VLANAllocation, results []prResult) {
	summaryPath := os.Getenv("GITHUB_STEP_SUMMARY")
	if summaryPath == "" {
		return
	}
	f, err := os.OpenFile(summaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "## Subnet `%s` decommissioning\n\n", req.Subnet.Name)
	fmt.Fprintf(f, "| | |\n|---|---|\n")
	fmt.Fprintf(f, "| CIDR | `%s` |\n| Environment | `%s` |\n\n", alloc.CIDR, alloc.Environment)
	fmt.Fprintf(f, "### IAC Removal Pull Requests (pending review)\n\n")
	for _, r := range results {
		fmt.Fprintf(f, "- [%s](%s)\n", r.label, r.url)
	}
}
