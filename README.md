# Network Self-Service — Subnet Provisioning

A GitOps-based self-service portal for provisioning subnets across Cisco ACI, BlueCat, and F5. Customers submit requests as YAML files; the pipeline generates Terraform configurations and opens merge requests in the relevant GitLab repositories.

---

## How it works

```
Customer
  └─ creates requests/<date>-<team>-<name>.yaml
  └─ opens GitHub Pull Request
        │
        ├─ [auto] GitHub Actions validates the request
        ├─ [auto] Plan comment posted on the PR
        │
        └─ Ops team reviews & merges
              │
              └─ [auto] Orchestrator generates Terraform HCL
                    ├─ Opens MR in ckae-aci   (ACI bridge domain + EPG)
                    ├─ Opens MR in ckae-bluecat (DNS/IPAM network)
                    └─ Opens MR in ckae-f5     (VLAN + Self-IP + AS3, per device)
                          │
                          └─ Ops reviews GitLab MRs → merge → TFE applies
```

**Two explicit approval gates:**
1. GitHub PR — intent review (is this request correct and authorized?)
2. GitLab MRs — config review (is the generated Terraform safe to apply?)

---

## Submitting a request

1. Copy `requests/example-webapp-prod.yaml`
2. Name your file `requests/<YYYY-MM-DD>-<your-team>-<subnet-name>.yaml`
3. Fill in the fields (see schema below)
4. Open a Pull Request — the pipeline will validate and post a plan
5. Tag the network ops team for review

### Request fields

| Field | Required | Description |
|---|---|---|
| `metadata.requester` | yes | Your team name |
| `metadata.ticket` | yes | Ticket reference (e.g. NET-1234) |
| `subnet.name` | yes | Short slug, lowercase+hyphens, used as resource name prefix |
| `subnet.environment` | yes | Logical environment (see below) |
| `subnet.cidr` | yes | IPv4 CIDR block |
| `subnet.gateway` | yes | Default gateway IP (must be within the CIDR) |
| `subnet.vlan_id` | no | Specific VLAN — auto-assigned from pool if omitted |
| `features.dns` | no | Register in BlueCat (default: `true`) |
| `features.subnet_forwarding` | no | AS3 forwarding VS on F5 loadbalancers (default: `true`) |
| `features.load_balancing` | no | Add an LB virtual server (disabled by default) |

### Available environments

Defined in `mappings/environments.yaml` (ops-managed):

| Key | Description |
|---|---|
| `prod-eu-west` | Production EU West |
| `nonprod-eu-west` | Non-Production EU West |

---

## Repository layout

```
self-service/
├── .github/workflows/
│   ├── validate.yml          # Runs on PR — validates + posts plan comment
│   └── orchestrate.yml       # Runs on merge — generates configs + creates GitLab MRs
├── requests/                 # Customers add YAML files here
├── mappings/
│   └── environments.yaml     # Ops-managed: env key → ACI/BlueCat/F5 infra mapping
├── state/
│   └── vlan-allocations.yaml # Auto-managed: tracks allocated VLANs per environment
├── templates/
│   ├── aci/                  # Jinja2 templates for ACI Terraform
│   ├── bluecat/              # Jinja2 templates for BlueCat Terraform
│   └── f5/                   # Jinja2 templates for F5 Terraform (VLAN, Self-IP, AS3)
├── scripts/
│   ├── validate.py           # Schema + business-rule validation
│   ├── orchestrate.py        # Main orchestration engine
│   └── requirements.txt
└── schemas/
    └── subnet_request.yaml   # JSON Schema for request files
```

---

## Ops: adding a new environment

Edit `mappings/environments.yaml` and add a new entry under `environments:`. Fields needed:

- `aci.gitlab_repo` + `gitlab_project_id` + tenant/VRF/AP/domain names
- `bluecat.gitlab_repo` + `gitlab_project_id` + configuration/view/parent block
- `f5.gitlab_repo` + `gitlab_project_id` + `vlan_pool` range + `devices` list

The `vlan_pool` ranges must not overlap across environments sharing the same F5 repo.

## Ops: required secrets

| Secret | Where | Description |
|---|---|---|
| `GITLAB_TOKEN` | GitHub repo secrets | Service account token with `api` scope on all target GitLab repos |

The token needs `Developer` role (to push branches and open MRs) on each `gitlab_project_id` listed in `mappings/environments.yaml`.

---

## Local development & testing

```bash
# First time: download dependencies and generate go.sum
cd scripts && go mod tidy && cd ..

# Build the binaries
cd scripts && go build -o ../bin/validate ./cmd/validate && go build -o ../bin/orchestrate ./cmd/orchestrate && cd ..

# Validate a request (no GitLab token needed)
./bin/validate --request requests/example-webapp-prod.yaml --summary

# Dry-run the full orchestration (renders templates, no GitLab calls)
./bin/orchestrate --request requests/example-webapp-prod.yaml --dry-run
```

### Go module layout

```
scripts/
├── go.mod
├── internal/           # shared library code
│   ├── types.go        # YAML structs for all config files
│   ├── config.go       # load/save YAML files
│   ├── netutil.go      # IP math (CIDR, offset, overlap)
│   ├── validator.go    # schema + business-rule checks
│   ├── allocator.go    # VLAN allocation and state recording
│   ├── render.go       # Go text/template rendering
│   └── gitlab.go       # GitLab API (branch, commit, MR)
└── cmd/
    ├── validate/main.go
    └── orchestrate/main.go
```
