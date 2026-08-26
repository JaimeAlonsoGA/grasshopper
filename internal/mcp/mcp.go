// Package mcp is a minimal Model Context Protocol server over stdio.
//
// It is here rather than pulled in as a dependency because the protocol
// grasshopper needs is three methods and a JSON envelope, and the standard
// library already speaks JSON. A dependency for this would be more code to trust,
// not less to write.
//
// The transport is newline-delimited JSON-RPC 2.0 on stdin and stdout, which is
// why nothing in this package may ever print to stdout: a stray line of output is
// a protocol violation, and the host sees a corrupt frame rather than a bug.
// Diagnostics go to stderr.
package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const protocolVersion = "2024-11-05"

// Tool is one thing the server can do. Schema is the JSON Schema for its
// arguments, which the calling agent reads to know how to invoke it.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Call        func(args json.RawMessage) (string, error)
}

// Server holds the tools and nothing else. No sessions, no state: every call is
// answered from the filesystem as it is at that moment, so a session that grew
// since the last call is simply read again.
type Server struct {
	Name    string
	Version string
	Tools   []Tool
}

// Serve reads requests until the input closes. A malformed line is answered with
// an error and the loop continues: one bad frame must not take down a server the
// host is depending on.
func (s *Server) Serve(in io.Reader, out io.Writer) error {
	reader := bufio.NewReaderSize(in, 1<<20)
	encoder := json.NewEncoder(out)

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if reply, ok := s.handle(line); ok {
				if err := encoder.Encode(reply); err != nil {
					return err
				}
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

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handle answers one frame. The second return says whether to reply at all:
// notifications carry no id and must be answered with silence.
func (s *Server) handle(line []byte) (response, bool) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}}, true
	}
	if len(req.ID) == 0 {
		return response{}, false
	}

	reply := response{JSONRPC: "2.0", ID: req.ID}
	result, err := s.dispatch(req)
	if err != nil {
		reply.Error = &rpcError{Code: -32603, Message: err.Error()}
		return reply, true
	}
	reply.Result = result
	return reply, true
}

func (s *Server) dispatch(req request) (any, error) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}, nil

	case "tools/list":
		return map[string]any{"tools": s.describe()}, nil

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("unreadable arguments: %w", err)
		}
		return s.call(params.Name, params.Arguments)

	case "ping":
		return map[string]any{}, nil

	default:
		return nil, fmt.Errorf("unknown method %q", req.Method)
	}
}

func (s *Server) call(name string, args json.RawMessage) (any, error) {
	for _, tool := range s.Tools {
		if tool.Name != name {
			continue
		}
		text, err := tool.Call(args)
		if err != nil {
			// Reported as a tool result rather than a protocol error: the call
			// arrived correctly and failed on its own terms, and the agent that
			// made it should be told why in words it can act on.
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": "grasshopper: " + err.Error()}},
				"isError": true,
			}, nil
		}
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		}, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func (s *Server) describe() []map[string]any {
	tools := make([]map[string]any, 0, len(s.Tools))
	for _, tool := range s.Tools {
		schema := tool.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools = append(tools, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": schema,
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i]["name"].(string) < tools[j]["name"].(string)
	})
	return tools
}
