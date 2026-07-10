package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Tobse95/network-self-service/internal"
)

type mrResult struct {
	label string
	url   string
}

func main() {
	requestFile := flag.String("request", "", "Path to request YAML file (required)")
	dryRun := flag.Bool("dry-run", false, "Generate variable files and print without opening PRs")
	mappingsFile := flag.String("mappings", "mappings/environments.yaml", "Path to environment mappings YAML")
	stateFile := flag.String("state", "state/vlan-allocations.yaml", "Path to VLAN state YAML")
	flag.Parse()

	if *requestFile == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --request is required")
		flag.Usage()
		os.Exit(1)
	}

	if err := run(*requestFile, *mappingsFile, *stateFile, *dryRun); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func run(requestFile, mappingsFile, stateFile string, dryRun bool) error {
	req, err := internal.LoadRequest(requestFile)
	if err != nil {
		return err
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

	vlanID, err := internal.AllocateVLAN(envName, &env, req, state)
	if err != nil {
		return err
	}

	prefixLen, err := internal.PrefixLength(req.Subnet.CIDR)
	if err != nil {
		return err
	}

	branchName := fmt.Sprintf("self-service/subnet-%s-%s",
		req.Subnet.Name,
		time.Now().UTC().Format("20060102150405"),
	)
	mrTitle := fmt.Sprintf("feat: subnet %s (%s) [%s]", req.Subnet.Name, req.Subnet.CIDR, req.Metadata.Ticket)
	mrBody := buildPRBody(req, envName, vlanID)

	var ghClient *internal.GitHubClient
	if !dryRun {
		token := os.Getenv("SELF_SERVICE_GITHUB_TOKEN")
		if token == "" {
			return fmt.Errorf("SELF_SERVICE_GITHUB_TOKEN is not set")
		}
		// All demo repos share the same owner; use ACI owner as the default.
		ghClient = internal.NewGitHubClient(token, env.ACI.GitHubOwner)
	}

	var results []mrResult

	// --- ACI ---
	fmt.Printf("\n[ACI] Generating vars for %s/%s\n", env.ACI.GitHubOwner, env.ACI.GitHubRepo)
	aciPath, aciContent, err := internal.BuildACIVars(req, &env, vlanID, prefixLen)
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
		results = append(results, mrResult{"ACI", url})
	}

	// --- BlueCat ---
	if req.Features.DNSEnabled() {
		fmt.Printf("\n[BlueCat] Generating vars for %s/%s\n", env.BlueCat.GitHubOwner, env.BlueCat.GitHubRepo)
		bcPath, bcContent, err := internal.BuildBlueCatVars(req, &env)
		if err != nil {
			return fmt.Errorf("BlueCat vars: %w", err)
		}
		if dryRun {
			printDryRun(bcPath, bcContent)
		} else {
			// BlueCat may be in a different owner; create its own client
			bcClient := internal.NewGitHubClient(os.Getenv("SELF_SERVICE_GITHUB_TOKEN"), env.BlueCat.GitHubOwner)
			url, err := bcClient.CreateBranchAndPR(env.BlueCat.GitHubRepo, branchName,
				map[string]string{bcPath: bcContent}, mrTitle, mrBody)
			if err != nil {
				return fmt.Errorf("BlueCat PR: %w", err)
			}
			results = append(results, mrResult{"BlueCat", url})
		}
	}

	// --- F5 — collect ALL device files into a single PR ---
	fmt.Printf("\n[F5] Generating vars for %s/%s (%d devices)\n",
		env.F5.GitHubOwner, env.F5.GitHubRepo, len(env.F5.Devices))
	f5Files := make(map[string]string, len(env.F5.Devices))
	for i := range env.F5.Devices {
		device := &env.F5.Devices[i]

		deviceSelfIP, err := internal.OffsetIP(req.Subnet.CIDR, device.SelfIPOffset)
		if err != nil {
			return fmt.Errorf("computing self-IP for %s: %w", device.Folder, err)
		}
		floatingSelfIP, err := internal.OffsetIP(req.Subnet.CIDR, device.FloatingSelfIPOffset)
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
		results = append(results, mrResult{"F5", url})
	}

	// --- Persist VLAN allocation ---
	if !dryRun {
		internal.RecordVLAN(envName, vlanID, req, requestFile, state)
		if err := internal.SaveState(stateFile, state); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
		fmt.Printf("\n[State] VLAN %d recorded in %s\n", vlanID, stateFile)
	}

	printSummary(req, envName, vlanID, results, dryRun)
	writeStepSummary(req, vlanID, envName, results)
	return nil
}

func printDryRun(path, content string) {
	fmt.Printf("\n  [dry-run] %s\n%s\n", path, content)
}

func printSummary(req *internal.Request, envName string, vlanID int, results []mrResult, dryRun bool) {
	sep := strings.Repeat("=", 60)
	fmt.Printf("\n%s\n", sep)
	fmt.Printf("Subnet '%s' (%s) — VLAN %d\nEnvironment: %s\n",
		req.Subnet.Name, req.Subnet.CIDR, vlanID, envName)
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

func writeStepSummary(req *internal.Request, vlanID int, envName string, results []mrResult) {
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
	fmt.Fprintln(f, "| | |\n|---|---|")
	fmt.Fprintf(f, "| CIDR | `%s` |\n| VLAN | `%d` |\n| Environment | `%s` |\n\n", req.Subnet.CIDR, vlanID, envName)
	fmt.Fprintln(f, "### Pull Requests\n")
	for _, r := range results {
		fmt.Fprintf(f, "- [%s](%s)\n", r.label, r.url)
	}
}
