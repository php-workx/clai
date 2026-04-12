package daemon

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/runger/clai/internal/suggestions/ops"
)

const (
	jsonRPCVersion      = "2.0"
	maxJSONRPCMessageSz = 1024 * 1024 // 1 MiB
)

const (
	jsonRPCParseError    = -32700
	jsonRPCInvalidReq    = -32600
	jsonRPCMethodMissing = -32601
	jsonRPCInvalidParams = -32602
	jsonRPCInternalError = -32603
	jsonRPCCommandAbsent = -32001
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	Result  any              `json:"result,omitempty"`
	Error   *jsonRPCErrorObj `json:"error,omitempty"`
	JSONRPC string           `json:"jsonrpc"`
	ID      json.RawMessage  `json:"id"`
}

type jsonRPCErrorObj struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type jsonRPCNotification struct {
	Params  any    `json:"params"`
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type jsonRPCConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func (c *jsonRPCConn) writeMessage(v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	c.mu.Lock()
	defer c.mu.Unlock()

	_, err = c.conn.Write(payload)
	return err
}

func (s *Server) startJSONRPCListener() (net.Listener, error) {
	socketPath := s.paths.JSONRPCSocketFile()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, fmt.Errorf("failed to create json-rpc socket directory: %w", err)
	}
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn("failed to remove stale json-rpc socket", "path", socketPath, "error", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on json-rpc socket: %w", err)
	}
	if err := os.Chmod(socketPath, 0o600); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("failed to set json-rpc socket permissions: %w", err)
	}

	s.logger.Info("json-rpc endpoint ready", "socket", socketPath)
	return listener, nil
}

func (s *Server) serveJSONRPC(ctx context.Context, listener net.Listener, errCh chan<- error) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.shutdownChan:
				return
			default:
			}

			if errors.Is(err, net.ErrClosed) {
				return
			}
			errCh <- fmt.Errorf("json-rpc accept error: %w", err)
			return
		}

		s.wg.Add(1)
		go s.handleJSONRPCConn(ctx, conn)
	}
}

func (s *Server) handleJSONRPCConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	wrapped := &jsonRPCConn{conn: conn}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), maxJSONRPCMessageSz+1)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > maxJSONRPCMessageSz {
			_ = wrapped.writeMessage(errorResponse(nil, jsonRPCParseError, "Parse error"))
			continue
		}

		s.processJSONRPCLine(ctx, wrapped, line)
	}

	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		s.logger.Debug("json-rpc connection ended", "error", err)
	}
}

func (s *Server) processJSONRPCLine(ctx context.Context, conn *jsonRPCConn, line []byte) {
	var req jsonRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		_ = conn.writeMessage(errorResponse(nil, jsonRPCParseError, "Parse error"))
		return
	}

	if req.JSONRPC != jsonRPCVersion || req.Method == "" {
		_ = conn.writeMessage(errorResponse(req.ID, jsonRPCInvalidReq, "Invalid Request"))
		return
	}

	result, notif, rpcErr := s.handleJSONRPCMethod(ctx, &req)
	if rpcErr != nil {
		_ = conn.writeMessage(errorResponse(req.ID, rpcErr.Code, rpcErr.Message))
		return
	}

	if len(req.ID) > 0 {
		_ = conn.writeMessage(successResponse(req.ID, result))
	}

	if notif != nil {
		_ = conn.writeMessage(notif)
	}
}

type commandStartParams struct {
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
	Timestamp int64  `json:"timestamp"`
}

type commandEndParams struct {
	CommandID string `json:"command_id"`
	ExitCode  int    `json:"exit_code"`
	Timestamp int64  `json:"timestamp"`
}

type outputChunkParams struct {
	CommandID  string `json:"command_id"`
	DataBase64 string `json:"data_base64"`
	IsStderr   bool   `json:"is_stderr"`
}

//nolint:cyclop,funlen // JSON-RPC method dispatch is a single switch; splitting would obscure the protocol.
func (s *Server) handleJSONRPCMethod(
	ctx context.Context,
	req *jsonRPCRequest,
) (result any, notif *jsonRPCNotification, rpcErr *jsonRPCErrorObj) {
	s.touchActivity()

	switch req.Method {
	case "ping":
		return map[string]bool{"pong": true}, nil, nil

	case "command.start":
		var params commandStartParams
		if err := decodeParams(req.Params, &params); err != nil {
			return nil, nil, invalidParamsError(err)
		}
		if params.SessionID == "" || params.CommandID == "" {
			return nil, nil, invalidParamsError(errors.New("session_id and command_id are required"))
		}
		if params.Timestamp == 0 {
			params.Timestamp = time.Now().UnixMilli()
		}

		if err := ops.UpsertCommandEventStart(ctx, s.db, params.SessionID, params.CommandID, params.Timestamp); err != nil {
			return nil, nil, internalError(err)
		}

		s.setCapturedBytes(params.CommandID, 0)
		return map[string]bool{"ok": true}, nil, nil

	case "output.chunk":
		var params outputChunkParams
		if err := decodeParams(req.Params, &params); err != nil {
			return nil, nil, invalidParamsError(err)
		}
		if params.CommandID == "" {
			return nil, nil, invalidParamsError(errors.New("command_id is required"))
		}
		if params.DataBase64 == "" {
			return nil, nil, invalidParamsError(errors.New("data_base64 is required"))
		}

		chunk, err := base64.StdEncoding.DecodeString(params.DataBase64)
		if err != nil {
			return nil, nil, invalidParamsError(fmt.Errorf("invalid base64 output chunk: %w", err))
		}
		if len(chunk) == 0 {
			return map[string]bool{"ok": true}, nil, nil
		}

		now := time.Now().UnixMilli()
		expiresAt := now + int64((7*24*time.Hour)/time.Millisecond)
		if err := ops.AppendCommandOutputChunk(ctx, s.db, params.CommandID, chunk, params.IsStderr, now, expiresAt); err != nil {
			return nil, nil, internalError(err)
		}

		s.addCapturedBytes(params.CommandID, int64(len(chunk)))
		return map[string]bool{"ok": true}, nil, nil

	case "command.end":
		var params commandEndParams
		if err := decodeParams(req.Params, &params); err != nil {
			return nil, nil, invalidParamsError(err)
		}
		if params.CommandID == "" {
			return nil, nil, invalidParamsError(errors.New("command_id is required"))
		}
		if params.Timestamp == 0 {
			params.Timestamp = time.Now().UnixMilli()
		}

		capturedBytes := s.popCapturedBytes(params.CommandID)
		err := ops.FinalizeCommandEvent(
			ctx,
			s.db,
			params.CommandID,
			params.ExitCode,
			params.Timestamp,
			false,
			capturedBytes,
		)
		if err != nil {
			if errors.Is(err, ops.ErrCommandNotFound) {
				return nil, nil, &jsonRPCErrorObj{
					Code:    jsonRPCCommandAbsent,
					Message: "Command not found",
				}
			}
			return nil, nil, internalError(err)
		}

		var notification *jsonRPCNotification
		if params.ExitCode != 0 {
			notification = &jsonRPCNotification{
				JSONRPC: jsonRPCVersion,
				Method:  "suggestion.available",
				Params: map[string]string{
					"command_id": params.CommandID,
					"suggestion": "Check command syntax, flags, and current working directory.",
				},
			}
		}
		return map[string]bool{"ok": true}, notification, nil

	default:
		return nil, nil, &jsonRPCErrorObj{
			Code:    jsonRPCMethodMissing,
			Message: "Method not found",
		}
	}
}

func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return errors.New("params are required")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return err
	}
	return nil
}

func successResponse(id json.RawMessage, result any) *jsonRPCResponse {
	return &jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Result:  result,
	}
}

func errorResponse(id json.RawMessage, code int, message string) *jsonRPCResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return &jsonRPCResponse{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error: &jsonRPCErrorObj{
			Code:    code,
			Message: message,
		},
	}
}

func invalidParamsError(err error) *jsonRPCErrorObj {
	return &jsonRPCErrorObj{
		Code:    jsonRPCInvalidParams,
		Message: fmt.Sprintf("Invalid params: %v", err),
	}
}

func internalError(err error) *jsonRPCErrorObj {
	return &jsonRPCErrorObj{
		Code:    jsonRPCInternalError,
		Message: fmt.Sprintf("Internal error: %v", err),
	}
}
