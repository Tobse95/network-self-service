package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v62/github"
)

// GitHubClient wraps go-github for the operations needed by the orchestrator.
type GitHubClient struct {
	client *github.Client
	owner  string
	ctx    context.Context
}

// NewGitHubClient creates a client authenticated with the given token.
func NewGitHubClient(token, owner string) *GitHubClient {
	return &GitHubClient{
		client: github.NewClient(nil).WithAuthToken(token),
		owner:  owner,
		ctx:    context.Background(),
	}
}

// CreateBranchAndPR creates a branch with all files in a single commit, then opens a PR.
// files maps repo-relative file paths to their content.
// Returns the PR HTML URL.
func (g *GitHubClient) CreateBranchAndPR(
	repoName string,
	branchName string,
	files map[string]string,
	prTitle string,
	prBody string,
) (string, error) {
	repo, _, err := g.client.Repositories.Get(g.ctx, g.owner, repoName)
	if err != nil {
		return "", fmt.Errorf("getting repo %s/%s: %w", g.owner, repoName, err)
	}
	defaultBranch := repo.GetDefaultBranch()

	// Resolve HEAD SHA of the default branch
	ref, _, err := g.client.Git.GetRef(g.ctx, g.owner, repoName, "refs/heads/"+defaultBranch)
	if err != nil {
		return "", fmt.Errorf("getting ref %s: %w", defaultBranch, err)
	}
	baseSHA := ref.GetObject().GetSHA()

	// Create branch
	_, _, err = g.client.Git.CreateRef(g.ctx, g.owner, repoName, &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: github.String(baseSHA)},
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return "", fmt.Errorf("creating branch %q: %w", branchName, err)
	}

	// Build a single tree entry per file and commit all at once
	entries := make([]*github.TreeEntry, 0, len(files))
	for path, content := range files {
		c := content
		p := path
		entries = append(entries, &github.TreeEntry{
			Path:    github.String(p),
			Mode:    github.String("100644"),
			Type:    github.String("blob"),
			Content: github.String(c),
		})
	}

	newTree, _, err := g.client.Git.CreateTree(g.ctx, g.owner, repoName, baseSHA, entries)
	if err != nil {
		return "", fmt.Errorf("creating tree in %s: %w", repoName, err)
	}

	commitMsg := fmt.Sprintf("feat: add subnet vars [self-service]\n\n%s", prTitle)
	newCommit, _, err := g.client.Git.CreateCommit(g.ctx, g.owner, repoName, &github.Commit{
		Message: github.String(commitMsg),
		Tree:    &github.Tree{SHA: newTree.SHA},
		Parents: []*github.Commit{{SHA: github.String(baseSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating commit in %s: %w", repoName, err)
	}

	_, _, err = g.client.Git.UpdateRef(g.ctx, g.owner, repoName, &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: newCommit.SHA},
	}, false)
	if err != nil {
		return "", fmt.Errorf("updating branch %q: %w", branchName, err)
	}

	pr, _, err := g.client.PullRequests.Create(g.ctx, g.owner, repoName, &github.NewPullRequest{
		Title: github.String(prTitle),
		Head:  github.String(branchName),
		Base:  github.String(defaultBranch),
		Body:  github.String(prBody),
	})
	if err != nil {
		return "", fmt.Errorf("creating PR in %s: %w", repoName, err)
	}

	// Best-effort label addition — repos may not have the labels pre-created
	g.client.Issues.AddLabelsToIssue(g.ctx, g.owner, repoName, pr.GetNumber(), //nolint
		[]string{"self-service", "subnet"})

	return pr.GetHTMLURL(), nil
}

// DeleteFilesAndPR creates a branch that removes the listed file paths, then opens a PR.
// Returns the PR HTML URL.
func (g *GitHubClient) DeleteFilesAndPR(
	repoName string,
	branchName string,
	paths []string,
	prTitle string,
	prBody string,
) (string, error) {
	repo, _, err := g.client.Repositories.Get(g.ctx, g.owner, repoName)
	if err != nil {
		return "", fmt.Errorf("getting repo %s/%s: %w", g.owner, repoName, err)
	}
	defaultBranch := repo.GetDefaultBranch()

	ref, _, err := g.client.Git.GetRef(g.ctx, g.owner, repoName, "refs/heads/"+defaultBranch)
	if err != nil {
		return "", fmt.Errorf("getting ref %s: %w", defaultBranch, err)
	}
	baseSHA := ref.GetObject().GetSHA()

	_, _, err = g.client.Git.CreateRef(g.ctx, g.owner, repoName, &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: github.String(baseSHA)},
	})
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return "", fmt.Errorf("creating branch %q: %w", branchName, err)
	}

	// sha: null signals deletion in a tree entry
	entries := make([]*github.TreeEntry, 0, len(paths))
	for _, p := range paths {
		path := p
		entries = append(entries, &github.TreeEntry{
			Path: github.String(path),
			Mode: github.String("100644"),
			Type: github.String("blob"),
			SHA:  nil,
		})
	}

	newTree, _, err := g.client.Git.CreateTree(g.ctx, g.owner, repoName, baseSHA, entries)
	if err != nil {
		return "", fmt.Errorf("creating deletion tree in %s: %w", repoName, err)
	}

	commitMsg := fmt.Sprintf("feat: remove subnet vars [self-service]\n\n%s", prTitle)
	newCommit, _, err := g.client.Git.CreateCommit(g.ctx, g.owner, repoName, &github.Commit{
		Message: github.String(commitMsg),
		Tree:    &github.Tree{SHA: newTree.SHA},
		Parents: []*github.Commit{{SHA: github.String(baseSHA)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating commit in %s: %w", repoName, err)
	}

	_, _, err = g.client.Git.UpdateRef(g.ctx, g.owner, repoName, &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: newCommit.SHA},
	}, false)
	if err != nil {
		return "", fmt.Errorf("updating branch %q: %w", branchName, err)
	}

	pr, _, err := g.client.PullRequests.Create(g.ctx, g.owner, repoName, &github.NewPullRequest{
		Title: github.String(prTitle),
		Head:  github.String(branchName),
		Base:  github.String(defaultBranch),
		Body:  github.String(prBody),
	})
	if err != nil {
		return "", fmt.Errorf("creating removal PR in %s: %w", repoName, err)
	}

	g.client.Issues.AddLabelsToIssue(g.ctx, g.owner, repoName, pr.GetNumber(), //nolint
		[]string{"self-service", "subnet-removal"})

	return pr.GetHTMLURL(), nil
}
