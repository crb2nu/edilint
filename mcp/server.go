// Package mcp serves edilint's checks over the Model Context Protocol, so that
// a coding agent or an LLM-driven tool can lint an interchange file and look
// up what a rule identifier means without shelling out.
//
// The server speaks the stdio binding: one JSON-RPC message per line on
// standard input and output, diagnostics on standard error. It implements
// both generations of the protocol at once. Clients written against the
// 2025-11-25 revision and earlier open with an "initialize" handshake;
// clients written against the 2026-07-28 revision carry their protocol
// version in every request's "_meta" and may probe with "server/discover".
// Each request is answered in the era it arrived in, so one process serves
// either kind of client.
//
// Only the standard library is used, and the server never opens a network
// connection or a file it was not asked to lint.
package mcp

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/crb2nu/edilint"
)

// Protocol revisions this server speaks.
const (
	// modernVersion is the per-request-metadata revision.
	modernVersion = "2026-07-28"
	// latestLegacyVersion is offered to a handshake client that asks for a
	// revision this server does not know.
	latestLegacyVersion = "2025-11-25"
)

// legacyVersions are the handshake revisions accepted by "initialize". The
// subset of the protocol this server uses (tools/list, tools/call, ping) is
// unchanged across them.
var legacyVersions = []string{latestLegacyVersion, "2025-06-18", "2025-03-26", "2024-11-05"}

// modernVersions are the revisions accepted in a request's "_meta".
var modernVersions = []string{modernVersion}

// Reserved "_meta" keys from the 2026-07-28 revision.
const (
	metaProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	metaClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	metaServerInfo         = "io.modelcontextprotocol/serverInfo"
)

// ServerName is the name reported to clients.
const ServerName = "edilint"

// instructions is the guidance a client may show its model.
const instructions = "edilint checks healthcare interchange files (X12 EDI, HL7v2 messages and " +
	"batches, EDIFACT, delimited and fixed-width records) for the defects that break a " +
	"downstream parser or draw a trading-partner rejection. Call lint_file with one or " +
	"more paths, or lint_text with file content, and read the findings: each carries a " +
	"stable rule identifier (EL3006 and the like) that explain_rule describes and " +
	"list_rules catalogs. An exit_status of 0 is clean, 1 means findings, 2 means an " +
	"input could not be read. Findings are the normal result of a call, not an error. " +
	"The server reads only the files it is given and never uses the network."

// Server serves edilint over MCP. The zero value is usable; Serve does the work.
type Server struct {
	// Version is reported as the server's version. Empty means "dev".
	Version string

	// Options is the configuration every lint call starts from, normally the
	// resolved .edilint.yml. A call's own arguments are layered on top: scalar
	// arguments replace, list arguments append.
	Options edilint.Options

	// AllowWarnings is the exit-status policy a call inherits when it does not
	// set allow_warnings itself: true means a file with only warnings counts as
	// clean.
	AllowWarnings bool

	// Log receives diagnostics. Nil discards them. It must not be the same
	// stream as the one Serve writes messages to.
	Log io.Writer
}

// Serve reads messages from r and writes responses to w until r reaches end
// of input, which is how a client asks the server to stop. It returns a
// non-nil error only when the transport itself fails.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	out := &writer{w: w}
	return readLines(r, func(line []byte) error {
		return s.handleLine(line, out)
	})
}

// logf writes a diagnostic. A failure to write one is not actionable.
func (s *Server) logf(format string, args ...any) {
	if s.Log == nil {
		return
	}
	_, _ = fmt.Fprintf(s.Log, "edilint mcp: "+format+"\n", args...)
}

// handleLine parses one message and writes its response, if it has one.
func (s *Server) handleLine(line []byte, out *writer) error {
	// A JSON array would be a batch. Batching was removed from the protocol
	// and is rejected as an invalid request rather than parsed.
	if line[0] == '[' {
		return out.write(response{JSONRPC: "2.0", ID: nullID,
			Error: errorf(codeInvalidRequest, "batch requests are not supported")})
	}

	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		return out.write(response{JSONRPC: "2.0", ID: nullID,
			Error: errorf(codeParseError, "parse error: %v", err)})
	}
	if m.JSONRPC != "2.0" {
		return out.write(response{JSONRPC: "2.0", ID: idOrNull(m.ID),
			Error: errorf(codeInvalidRequest, "jsonrpc must be \"2.0\"")})
	}
	if m.hasNullID() {
		return out.write(response{JSONRPC: "2.0", ID: nullID,
			Error: errorf(codeInvalidRequest, "request id must not be null")})
	}
	if m.Method == "" {
		return out.write(response{JSONRPC: "2.0", ID: idOrNull(m.ID),
			Error: errorf(codeInvalidRequest, "method is required")})
	}

	if m.isNotification() {
		// The only notifications a client sends this server are
		// notifications/initialized and notifications/cancelled, and neither
		// needs an action: there is no state to arm, and every request is
		// answered synchronously before the next line is read.
		return nil
	}

	result, rpcErr := s.dispatch(&m)
	if rpcErr != nil {
		return out.write(response{JSONRPC: "2.0", ID: m.ID, Error: rpcErr})
	}
	return out.write(response{JSONRPC: "2.0", ID: m.ID, Result: result})
}

// idOrNull returns the identifier to echo on an error, null when there is none.
func idOrNull(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return nullID
	}
	return id
}

// requestMeta is what a request's "_meta" told the server about the client.
type requestMeta struct {
	// modern is true when the request carried a protocol version, which is
	// what distinguishes a 2026-07-28 client from a handshake client.
	modern  bool
	version string
}

// readMeta extracts the per-request protocol fields, deciding which era the
// request belongs to and whether it is acceptable.
func readMeta(params json.RawMessage) (requestMeta, *rpcError) {
	var meta requestMeta
	if len(params) == 0 {
		return meta, nil
	}
	var envelope struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(params, &envelope); err != nil {
		return meta, errorf(codeInvalidParams, "params must be an object: %v", err)
	}
	rawVersion, ok := envelope.Meta[metaProtocolVersion]
	if !ok {
		return meta, nil
	}
	meta.modern = true
	if err := json.Unmarshal(rawVersion, &meta.version); err != nil {
		return meta, errorf(codeInvalidParams, "%s must be a string", metaProtocolVersion)
	}
	if !contains(modernVersions, meta.version) {
		return meta, &rpcError{
			Code:    codeUnsupportedProtocolVersion,
			Message: "Unsupported protocol version",
			Data: map[string]any{
				"supported": modernVersions,
				"requested": meta.version,
			},
		}
	}
	if _, ok := envelope.Meta[metaClientCapabilities]; !ok {
		return meta, errorf(codeInvalidParams, "missing required _meta field %s", metaClientCapabilities)
	}
	return meta, nil
}

// dispatch routes one request to its handler.
func (s *Server) dispatch(m *message) (any, *rpcError) {
	meta, rpcErr := readMeta(m.Params)
	if rpcErr != nil {
		return nil, rpcErr
	}

	var result map[string]any
	switch m.Method {
	case "initialize":
		result, rpcErr = s.initialize(m.Params)
	case "server/discover":
		result = s.discover()
	case "ping":
		result = map[string]any{}
	case "tools/list":
		result = map[string]any{"tools": toolDefinitions()}
	case "tools/call":
		result, rpcErr = s.callTool(m.Params)
	default:
		return nil, errorf(codeMethodNotFound, "method not found: %s", m.Method)
	}
	if rpcErr != nil {
		return nil, rpcErr
	}

	if meta.modern {
		// A 2026-07-28 result names its own kind and identifies the server on
		// every reply, because the client keeps no session to remember either.
		result["resultType"] = "complete"
		result["_meta"] = map[string]any{metaServerInfo: s.serverInfo()}
	}
	return result, nil
}

// serverInfo is the implementation description sent to clients.
func (s *Server) serverInfo() map[string]any {
	version := s.Version
	if version == "" {
		version = "dev"
	}
	return map[string]any{
		"name":    ServerName,
		"title":   "edilint",
		"version": version,
	}
}

// capabilities declares what this server offers: tools, whose list never
// changes while the process runs.
func capabilities() map[string]any {
	return map[string]any{"tools": map[string]any{}}
}

// initialize answers the handshake of the 2025-11-25 revision and earlier. A
// requested revision the server knows is echoed; any other is answered with
// the latest revision the server speaks, as the handshake rules require.
func (s *Server) initialize(params json.RawMessage) (map[string]any, *rpcError) {
	var p struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errorf(codeInvalidParams, "initialize params: %v", err)
		}
	}
	negotiated := latestLegacyVersion
	if contains(legacyVersions, p.ProtocolVersion) {
		negotiated = p.ProtocolVersion
	} else if p.ProtocolVersion != "" {
		s.logf("client asked for protocol %s; answering with %s", p.ProtocolVersion, negotiated)
	}
	return map[string]any{
		"protocolVersion": negotiated,
		"capabilities":    capabilities(),
		"serverInfo":      s.serverInfo(),
		"instructions":    instructions,
	}, nil
}

// discover answers the 2026-07-28 revision's server/discover request.
func (s *Server) discover() map[string]any {
	return map[string]any{
		"supportedVersions": modernVersions,
		"capabilities":      capabilities(),
		"instructions":      instructions,
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
