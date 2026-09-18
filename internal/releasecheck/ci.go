package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

type workflowRun struct {
	ID         int64  `json:"id"`
	SHA        string `json:"head_sha"`
	Branch     string `json:"head_branch"`
	Event      string `json:"event"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

func successfulRun(runs []workflowRun, sha, branch string) error {
	var latest workflowRun
	for _, run := range runs {
		if run.SHA == sha && run.Branch == branch && run.Event == "push" && run.ID > latest.ID {
			latest = run
		}
	}
	if latest.ID == 0 || latest.Status != "completed" || latest.Conclusion != "success" {
		return fmt.Errorf("latest main CI for %s must have completed successfully (run %d, status %q, conclusion %q)",
			sha, latest.ID, latest.Status, latest.Conclusion)
	}
	return nil
}

func checkCI(base, repo, sha, branch, token string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(repo) || !commitSHA.MatchString(sha) || branch == "" {
		return fmt.Errorf("CI verification needs owner/repository, full SHA, and branch")
	}
	query := url.Values{"head_sha": {sha}, "branch": {branch}, "event": {"push"}, "per_page": {"100"}}
	endpoint := base + "/repos/" + repo + "/actions/workflows/ci.yml/runs?" + query.Encode()
	req, err := http.NewRequest(http.MethodGet, endpoint, nil) // #nosec G704 -- Production pins api.github.com; tests pass only a local HTTP server.
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req) // #nosec G704 -- The endpoint is fixed by the command, not release input.
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub CI lookup returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Runs []workflowRun `json:"workflow_runs"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
		return fmt.Errorf("decode GitHub CI evidence: %w", err)
	}
	return successfulRun(result.Runs, sha, branch)
}
