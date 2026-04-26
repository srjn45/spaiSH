package socket

import (
	"encoding/json"
	"net"
	"os"

	"spaish/internal/protocol"
)

// QueryHandler processes a query request and writes Response chunks to enc.
type QueryHandler func(req *protocol.Request, enc *json.Encoder)

// ExecHandler processes an execute request and writes Response chunks to enc.
type ExecHandler func(req *protocol.Request, enc *json.Encoder)

// LLMHandler processes an llm management request and writes Response chunks to enc.
type LLMHandler func(req *protocol.Request, enc *json.Encoder)

// AgentHandler processes an agent request. It receives both enc and dec because
// the agent loop may pause mid-stream to send a confirm_request and read back a
// confirm_response on the same connection.
type AgentHandler func(req *protocol.Request, enc *json.Encoder, dec *json.Decoder)

// SessionHandler processes a session management request (clear/compact).
type SessionHandler func(req *protocol.Request, enc *json.Encoder)

// ShellHandler processes a shell event request and writes Response chunks to enc.
type ShellHandler func(req *protocol.Request, enc *json.Encoder)

// Serve starts a Unix domain socket server at sockPath.
// Blocks until the listener is closed or an unrecoverable error occurs.
func Serve(sockPath string, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler, onAgent AgentHandler, onSession SessionHandler, onShell ShellHandler) error {
	os.Remove(sockPath)
	os.MkdirAll(sockPath[:len(sockPath)-len("/spaid.sock")], 0700)

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	defer ln.Close()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		go handleConn(conn, onQuery, onExec, onLLM, onAgent, onSession, onShell)
	}
}

func handleConn(conn net.Conn, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler, onAgent AgentHandler, onSession SessionHandler, onShell ShellHandler) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	var req protocol.Request
	if err := dec.Decode(&req); err != nil {
		return
	}

	switch req.Type {
	case "query":
		onQuery(&req, enc)
	case "execute":
		onExec(&req, enc)
	case "llm":
		onLLM(&req, enc)
	case "agent":
		onAgent(&req, enc, dec)
	case "session":
		onSession(&req, enc)
	case "shell":
		onShell(&req, enc)
	default:
		enc.Encode(protocol.Response{Type: "error", Content: "unknown request type: " + req.Type})
	}
}
