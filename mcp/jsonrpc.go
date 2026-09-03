package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// JSON-RPC 2.0 error codes, plus the one MCP-specific code this server emits.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603

	// codeUnsupportedProtocolVersion is UnsupportedProtocolVersionError from
	// the 2026-07-28 revision: the request named a protocol version this server
	// does not speak, and the error's data lists the ones it does.
	codeUnsupportedProtocolVersion = -32022
)

// message is one incoming JSON-RPC message. ID is kept raw so that a string or
// integer identifier is echoed back byte-for-byte, and so that an absent
// identifier (a notification) can be told apart from an explicit null (which
// MCP forbids on a request).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// isNotification reports whether the message carries no identifier at all.
func (m *message) isNotification() bool {
	return len(m.ID) == 0
}

// hasNullID reports whether the message carries an explicit null identifier.
func (m *message) hasNullID() bool {
	return bytes.Equal(bytes.TrimSpace(m.ID), []byte("null"))
}

// rpcError is the error member of a JSON-RPC error response.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("jsonrpc error %d: %s", e.Code, e.Message)
}

// errorf builds an rpcError with a formatted message and no data.
func errorf(code int, format string, args ...any) *rpcError {
	return &rpcError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// response is an outgoing JSON-RPC response. Exactly one of Result and Error
// is set. ID is always emitted, as null when the request's own identifier
// could not be read.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// nullID is the identifier used when a request's own could not be read.
var nullID = json.RawMessage("null")

// writer serializes responses to the transport, one per line.
//
// The stdio binding forbids embedded newlines inside a message and forbids
// anything on standard output that is not a message. encoding/json escapes
// every control character inside strings and emits no whitespace of its own,
// so Marshal plus one trailing newline satisfies both rules.
type writer struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *writer) write(resp response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		// A result that cannot be marshaled is a programming error in this
		// package, but the client still deserves a well-formed reply.
		resp = response{JSONRPC: "2.0", ID: resp.ID,
			Error: errorf(codeInternalError, "encode response: %v", err)}
		if data, err = json.Marshal(resp); err != nil {
			return err
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err = w.w.Write(data); err != nil {
		return err
	}
	_, err = w.w.Write([]byte{'\n'})
	return err
}

// readLines calls fn for every non-blank line of r until end of input.
//
// bufio.Scanner has a fixed maximum token size, which a lint_text call
// carrying a whole interchange could exceed, so lines are read with
// ReadBytes instead. A final line with no terminating newline is still
// delivered.
func readLines(r io.Reader, fn func(line []byte) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := br.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if fnErr := fn(trimmed); fnErr != nil {
				return fnErr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
