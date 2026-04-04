package socket

import (
	"encoding/json"
	"net"
	"os"

	"spaios/internal/protocol"
)

// QueryHandler processes a query request and writes Response chunks to enc.
type QueryHandler func(req *protocol.Request, enc *json.Encoder)

// ExecHandler processes an execute request and writes Response chunks to enc.
type ExecHandler func(req *protocol.Request, enc *json.Encoder)

// LLMHandler processes an llm management request and writes Response chunks to enc.
type LLMHandler func(req *protocol.Request, enc *json.Encoder)

// Serve starts a Unix domain socket server at sockPath.
// Blocks until the listener is closed or an unrecoverable error occurs.
func Serve(sockPath string, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler) error {
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
		go handleConn(conn, onQuery, onExec, onLLM)
	}
}

func handleConn(conn net.Conn, onQuery QueryHandler, onExec ExecHandler, onLLM LLMHandler) {
	defer conn.Close()

	var req protocol.Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		return
	}

	enc := json.NewEncoder(conn)
	switch req.Type {
	case "query":
		onQuery(&req, enc)
	case "execute":
		onExec(&req, enc)
	case "llm":
		onLLM(&req, enc)
	default:
		enc.Encode(protocol.Response{Type: "error", Content: "unknown request type: " + req.Type})
	}
}
