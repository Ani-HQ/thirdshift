package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

type Command struct {
	Action string `json:"action"`
}

type Status struct {
	NodeID            string     `json:"node_id"`
	State             string     `json:"state"`
	GPU               string     `json:"gpu"`
	ModelID           string     `json:"model_id"`
	RuntimeHash       string     `json:"runtime_hash"`
	ModelHash         string     `json:"model_hash"`
	Schedule          string     `json:"schedule"`
	TemperatureC      *int       `json:"temperature_c,omitempty"`
	PowerW            *int       `json:"power_w,omitempty"`
	SessionConnected  bool       `json:"session_connected"`
	SessionID         string     `json:"session_id,omitempty"`
	LastHeartbeatAt   *time.Time `json:"last_heartbeat_at,omitempty"`
	CredentialBackend string     `json:"credential_backend"`
	CoordinatorURL    string     `json:"coordinator_url"`
	HeartbeatInterval string     `json:"heartbeat_interval"`
	LastError         string     `json:"last_error,omitempty"`
}

type Response struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Status *Status `json:"status,omitempty"`
}

type Handler func(Command) Response

func handleConn(conn net.Conn, handler Handler) {
	defer conn.Close()
	var command Command
	if err := json.NewDecoder(conn).Decode(&command); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{OK: false, Error: "invalid command"})
		return
	}
	response := handler(command)
	if response.Error != "" {
		response.OK = false
	} else {
		response.OK = true
	}
	_ = json.NewEncoder(conn).Encode(response)
}

func decodeResponse(conn net.Conn) (Response, error) {
	var response Response
	if err := json.NewDecoder(conn).Decode(&response); err != nil {
		return Response{}, fmt.Errorf("decode control response: %w", err)
	}
	if !response.OK {
		if response.Error == "" {
			response.Error = "control command failed"
		}
		return response, errors.New(response.Error)
	}
	return response, nil
}
