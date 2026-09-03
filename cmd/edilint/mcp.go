package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crb2nu/edilint"
	"github.com/crb2nu/edilint/mcp"
)

// stdin is the transport input for "edilint mcp". It is a variable so that the
// tests can drive the server without a real process.
var stdin io.Reader = os.Stdin

// runMCP implements the "mcp" subcommand: it resolves the same configuration
// file the linter would, then serves the Model Context Protocol on standard
// input and output until the client closes its input.
func runMCP(args []string, in io.Reader, stdout, stderr io.Writer) int {
	var (
		configPath string
		noConfig   bool
		verbose    bool
		showHelp   bool
	)
	for i := 0; i < len(args); i++ {
		name, val, hasInline := strings.Cut(args[i], "=")
		var err error
		switch name {
		case "-h", "--help":
			showHelp = true
		case "--no-config":
			noConfig = true
		case "-v", "--verbose":
			verbose = true
		case "--config":
			if !hasInline {
				if i+1 >= len(args) {
					err = fmt.Errorf("--config requires a value")
					break
				}
				i++
				val = args[i]
			}
			configPath = val
		default:
			err = fmt.Errorf("unknown flag: %s", args[i])
		}
		if err == nil && hasInline && name != "--config" {
			err = fmt.Errorf("%s does not take a value", name)
		}
		if err != nil {
			diagf(stderr, "edilint mcp: %v\n", err)
			diagf(stderr, "Try 'edilint mcp --help' for usage.\n")
			return exitUsage
		}
	}
	if showHelp {
		printMCPUsage(stdout)
		return exitClean
	}
	if noConfig && configPath != "" {
		diagf(stderr, "edilint mcp: --config and --no-config cannot be used together\n")
		return exitUsage
	}

	set, err := resolve(config{configPath: configPath, noConfig: noConfig, set: map[string]bool{}})
	if err != nil {
		diagf(stderr, "edilint mcp: %v\n", err)
		return exitUsage
	}
	if set.layoutPath != "" {
		layout, err := edilint.LoadLayout(set.layoutPath)
		if err != nil {
			diagf(stderr, "edilint mcp: %v\n", err)
			return exitUsage
		}
		set.opts.Layout = layout
	}
	if verbose && set.configPath != "" {
		diagf(stderr, "edilint mcp: using config %s\n", set.configPath)
	}

	srv := &mcp.Server{
		Version:       buildVersion(),
		Options:       set.opts,
		AllowWarnings: set.allowWarnings,
		Log:           stderr,
	}
	if err := srv.Serve(in, stdout); err != nil {
		diagf(stderr, "edilint mcp: %v\n", err)
		return exitUsage
	}
	return exitClean
}

func printMCPUsage(w io.Writer) {
	diagf(w, `edilint mcp - serve edilint over the Model Context Protocol

Usage:
  edilint mcp [flags]

Speaks the stdio transport: one JSON-RPC message per line on standard input
and output, diagnostics on standard error. Register it with an MCP client as
the command "edilint" with the argument "mcp". Both the initialize handshake
(protocol revisions 2025-11-25 and earlier) and per-request metadata
(revision 2026-07-28) are accepted, so any client can connect.

Tools:
  lint_file     lint one or more files by path
  lint_text     lint content passed in the call
  list_rules    the rule catalog, optionally filtered by class
  explain_rule  what a rule identifier or name checks and how to suppress it

Flags:
      --config <file>  configuration file (default .edilint.yml in the working
                       directory, if any); its settings are the base every
                       call starts from
      --no-config      ignore any .edilint.yml in the working directory
  -v, --verbose        name the configuration file in use on standard error
  -h, --help           print this help and exit

The server never uses the network and reads only the files a call names. It
exits 0 when the client closes its input, 2 on a usage error or when the
configuration cannot be read.
`)
}
