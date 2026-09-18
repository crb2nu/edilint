// Command releasecheck verifies release inputs and artifacts in CI.
// It is not part of the distributed edilint executable.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release check:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("expected tag, ci, or artifacts")
	}
	f := flag.NewFlagSet(args[0], flag.ContinueOnError)
	switch args[0] {
	case "tag":
		tag := f.String("tag", "", "stable version tag")
		sha := f.String("sha", "", "expected commit SHA")
		mainRef := f.String("main", "origin/main", "canonical main reference")
		notes := f.String("notes", "", "optional release notes output path")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		body, err := checkTag(*tag, *sha, *mainRef)
		if err != nil {
			return err
		}
		if *notes != "" {
			return os.WriteFile(*notes, []byte(body), 0o600)
		}
	case "ci":
		repo := f.String("repo", "", "GitHub owner/repository")
		sha := f.String("sha", "", "expected commit SHA")
		branch := f.String("branch", "main", "default branch")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		return checkCI("https://api.github.com", *repo, *sha, *branch, os.Getenv("GITHUB_TOKEN"))
	case "artifacts":
		dir := f.String("dir", "dist", "GoReleaser output directory")
		if err := f.Parse(args[1:]); err != nil {
			return err
		}
		return checkArtifacts(*dir)
	default:
		return fmt.Errorf("unknown check %q", args[0])
	}
	return nil
}
