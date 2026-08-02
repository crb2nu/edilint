# Security

edilint parses untrusted files by design. If you find a vulnerability — a crash,
unbounded resource use, or any way a crafted input escapes the report-and-exit
contract — report it privately rather than opening an issue.

## Reporting

Email **contact@flexinfer.ai** with:

- a description of the issue and its impact
- a reproducing input (the file itself, or a script that generates one)
- the version or commit you tested

You will get an acknowledgment within a few days. This is a maintained personal
project, not a company: fixes ship as fast as one person can review them, and
reporters are credited in the changelog unless they ask otherwise.

## Scope

- The latest release and `main` are supported; older versions are not patched.
- edilint makes no network calls and reads only the files you name. Anything
  that contradicts that is a vulnerability, regardless of severity.
