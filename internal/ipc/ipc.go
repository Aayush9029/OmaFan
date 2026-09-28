// Package ipc is the line-delimited JSON protocol between the omafan CLI and daemon.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/Aayush9029/OmaFan/internal/curve"
	"github.com/Aayush9029/OmaFan/internal/sensors"
)

// DefaultSocket is created by the daemon through systemd's RuntimeDirectory.
const DefaultSocket = "/run/omafan/omafan.sock"

// MaxMessage bounds a single request or response line.
const MaxMessage = 64 << 10

// Controllers reported in Status.Controller.
const (
	ControllerOmaFan    = "omafan"
	ControllerFirmware  = "firmware"
	ControllerEmergency = "emergency"
)

// Request is one command.
type Request struct {
	Cmd     string        `json:"cmd"` // status, enable, disable, profile, curve
	Profile string        `json:"profile,omitempty"`
	Points  []curve.Point `json:"points,omitempty"`
}

// Status is the daemon's current state.
type Status struct {
	Version    string                   `json:"version"`
	Enabled    bool                     `json:"enabled"`
	Profile    string                   `json:"profile"`
	Points     []curve.Point            `json:"points"`
	Custom     []curve.Point            `json:"custom"`
	Presets    map[string][]curve.Point `json:"presets"`
	Controller string                   `json:"controller"`
	Duty       *int                     `json:"duty"`
	Target     *float64                 `json:"target"`
	Temp       *float64                 `json:"temp"`
	CPU        *float64                 `json:"cpu"`
	GPU        *float64                 `json:"gpu"`
	APU        *float64                 `json:"apu"`
	Fans       []sensors.Fan            `json:"fans"`
	FanRPM     int                      `json:"fanRpm"`
	Error      string                   `json:"error"`
	UpdatedAt  time.Time                `json:"updatedAt"`
}

// Response answers a Request.
type Response struct {
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Status *Status `json:"status,omitempty"`
}

// ErrNotRunning means nothing is listening on the socket.
var ErrNotRunning = errors.New("the OmaFan service isn't running (sudo systemctl enable --now omafan)")

// Call sends one request and waits for the reply.
func Call(socket string, req Request) (*Status, error) {
	if socket == "" {
		socket = DefaultSocket
	}
	if env := os.Getenv("OMAFAN_SOCKET"); env != "" {
		socket = env
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errConnRefused) {
			return nil, ErrNotRunning
		}
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), MaxMessage)
	if !sc.Scan() {
		if sc.Err() != nil {
			return nil, sc.Err()
		}
		return nil, errors.New("the OmaFan service closed the connection")
	}
	var resp Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("invalid reply from the OmaFan service: %w", err)
	}
	if !resp.OK {
		return resp.Status, errors.New(resp.Error)
	}
	return resp.Status, nil
}
