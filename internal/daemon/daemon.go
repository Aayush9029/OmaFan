// Package daemon runs the fan control loop and serves the omafan socket.
package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/Aayush9029/OmaFan/internal/control"
	"github.com/Aayush9029/OmaFan/internal/curve"
	"github.com/Aayush9029/OmaFan/internal/ec"
	"github.com/Aayush9029/OmaFan/internal/ipc"
	"github.com/Aayush9029/OmaFan/internal/sensors"
)

const (
	// Interval between control steps.
	Interval = time.Second
	// Resend the duty this often even when unchanged, in case the EC reset
	// itself to auto (for example across suspend).
	Reassert = 5 * time.Second
	// Hand fans back to firmware after this long without a temperature.
	SensorGrace = 5 * time.Second
	// Hand fans back to firmware after this many EC write failures in a row.
	MaxECFailures = 3
	// DefaultMaxRPM is the Framework Desktop fan speed at 100% duty.
	DefaultMaxRPM = 2320
)

// Settings are persisted across restarts.
type Settings struct {
	Enabled bool          `json:"enabled"`
	Profile string        `json:"profile"`
	Custom  []curve.Point `json:"custom"`
}

// DefaultSettings leave the firmware in charge until the user turns OmaFan on.
func DefaultSettings() Settings {
	custom, _ := curve.Preset("balanced")
	return Settings{Enabled: false, Profile: "balanced", Custom: custom}
}

// Reader is the sensor source.
type Reader interface {
	Read() (sensors.Reading, error)
}

// Daemon owns the fans while running.
type Daemon struct {
	Version   string
	StatePath string
	Fans      ec.Fans
	Sensors   Reader
	Now       func() time.Time
	Log       *log.Logger
	// Authorize reports whether a peer uid may change settings.
	Authorize func(uid uint32) bool

	mu           sync.Mutex
	settings     Settings
	ctl          control.Controller
	manual       bool
	lastDuty     int
	lastSent     time.Time
	sensorFailAt time.Time
	ecFailures   int
	maxRPM       int
	stopped      bool
	status       ipc.Status
}

func (d *Daemon) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log.Printf(format, args...)
	}
}

// Load reads settings, falling back to defaults for anything missing or invalid.
func (d *Daemon) Load() {
	s := DefaultSettings()
	if b, err := os.ReadFile(d.StatePath); err == nil {
		var saved Settings
		if json.Unmarshal(b, &saved) == nil {
			s.Enabled = saved.Enabled
			if curve.ValidProfile(saved.Profile) {
				s.Profile = saved.Profile
			}
			if curve.Validate(saved.Custom) == nil {
				// Curves saved with fewer points gain one, keeping their shape.
				s.Custom = curve.Expand(saved.Custom)
			}
		} else {
			d.logf("ignoring unreadable settings %s", d.StatePath)
		}
	}
	d.mu.Lock()
	d.settings = s
	d.mu.Unlock()
}

func (d *Daemon) save() error {
	if d.StatePath == "" {
		return nil
	}
	b, err := json.MarshalIndent(d.settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := d.StatePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(d.StatePath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, d.StatePath)
}

// points returns the active curve. Callers hold d.mu.
func (d *Daemon) points() []curve.Point {
	if d.settings.Profile == "custom" {
		return d.settings.Custom
	}
	p, _ := curve.Preset(d.settings.Profile)
	return p
}

// release hands the fans to firmware. The fans count as manual until the EC
// accepts, so every caller that checks d.manual retries on the next tick.
// Callers hold d.mu.
func (d *Daemon) release(reason string) error {
	d.ctl.Reset()
	if err := d.Fans.Auto(); err != nil {
		d.logf("returning fans to firmware (%s) failed: %v", reason, err)
		return err
	}
	if d.manual {
		d.logf("fans returned to firmware: %s", reason)
	}
	d.manual = false
	return nil
}

// releaseStatus records a handback, reporting a failure the next tick will retry.
// Callers hold d.mu.
func (d *Daemon) releaseStatus(reason string) {
	st := &d.status
	if err := d.release(reason); err != nil {
		st.Error = "Couldn't return the fans to firmware, retrying: " + err.Error()
		duty := d.lastDuty
		st.Controller, st.Duty = ipc.ControllerOmaFan, &duty
		return
	}
	st.Controller, st.Duty = ipc.ControllerFirmware, nil
}

// Tick runs one control step.
func (d *Daemon) Tick() {
	reading, readErr := d.Sensors.Read()
	now := d.now()

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}

	st := &d.status
	st.CPU, st.GPU, st.APU = reading.CPU, reading.GPU, reading.APU
	st.Fans, st.FanRPM = reading.Fans, reading.FanRPM()
	// The EC caps these fans near 2300 RPM; learn a higher ceiling if one shows up.
	d.maxRPM = max(d.maxRPM, DefaultMaxRPM, st.FanRPM)
	st.FanMaxRPM = d.maxRPM
	st.UpdatedAt = now
	st.Error = ""
	st.Temp, st.Target = nil, nil
	if hot, ok := reading.Hottest(); ok {
		st.Temp = &hot
	}

	if !d.settings.Enabled {
		if d.manual {
			d.releaseStatus("turned off")
			return
		}
		st.Controller, st.Duty = ipc.ControllerFirmware, nil
		return
	}

	if readErr != nil {
		if d.sensorFailAt.IsZero() {
			d.sensorFailAt = now
		}
		st.Error = "Temperature sensors unavailable: " + readErr.Error()
		if d.manual && now.Sub(d.sensorFailAt) >= SensorGrace {
			d.releaseStatus("no CPU temperature")
			if st.Controller == ipc.ControllerFirmware {
				st.Error = "Temperature sensors unavailable, firmware took over: " + readErr.Error()
			}
			return
		}
		if !d.manual {
			st.Controller, st.Duty = ipc.ControllerFirmware, nil
		} else {
			duty := d.lastDuty
			st.Controller, st.Duty = ipc.ControllerOmaFan, &duty
		}
		return
	}
	d.sensorFailAt = time.Time{}

	res := d.ctl.Step(d.points(), *st.Temp, now)
	target := res.Target
	st.Target = &target

	if !d.manual || res.Duty != d.lastDuty || now.Sub(d.lastSent) >= Reassert {
		if err := d.Fans.SetDuty(res.Duty); err != nil {
			d.ecFailures++
			st.Error = "Couldn't set the fan speed: " + err.Error()
			d.logf("set duty %d%%: %v", res.Duty, err)
			if d.ecFailures >= MaxECFailures {
				msg := st.Error
				d.releaseStatus("repeated EC errors")
				if st.Controller == ipc.ControllerFirmware {
					st.Error = msg + " · firmware took over"
				}
				return
			}
		} else {
			d.ecFailures = 0
			d.manual = true
			d.lastDuty = res.Duty
			d.lastSent = now
		}
	}

	if !d.manual {
		st.Controller, st.Duty = ipc.ControllerFirmware, nil
		return
	}
	duty := d.lastDuty
	st.Duty = &duty
	st.Controller = ipc.ControllerOmaFan
	if res.Emergency {
		st.Controller = ipc.ControllerEmergency
	}
}

// Status returns a copy of the current status.
func (d *Daemon) Status() ipc.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	st := d.status
	st.Version = d.Version
	st.Enabled = d.settings.Enabled
	st.Profile = d.settings.Profile
	st.Points = append([]curve.Point(nil), d.points()...)
	st.Custom = append([]curve.Point(nil), d.settings.Custom...)
	st.Presets = map[string][]curve.Point{}
	for _, name := range curve.Profiles {
		if p, ok := curve.Preset(name); ok {
			st.Presets[name] = p
		}
	}
	st.Fans = append([]sensors.Fan(nil), st.Fans...)
	if st.Controller == "" {
		st.Controller = ipc.ControllerFirmware
	}
	return st
}

// Apply handles a request. Mutations are persisted before the reply.
func (d *Daemon) Apply(req ipc.Request) error {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return errors.New("OmaFan is shutting down")
	}
	next := d.settings
	next.Custom = append([]curve.Point(nil), d.settings.Custom...)
	switch req.Cmd {
	case "status":
		d.mu.Unlock()
		return nil
	case "enable":
		next.Enabled = true
	case "disable":
		next.Enabled = false
	case "profile":
		if !curve.ValidProfile(req.Profile) {
			d.mu.Unlock()
			return fmt.Errorf("unknown profile %q", req.Profile)
		}
		next.Profile = req.Profile
	case "curve":
		if err := curve.Validate(req.Points); err != nil {
			d.mu.Unlock()
			return err
		}
		next.Custom = append([]curve.Point(nil), req.Points...)
		next.Profile = "custom"
	default:
		d.mu.Unlock()
		return fmt.Errorf("unknown command %q", req.Cmd)
	}
	prev := d.settings
	d.settings = next
	if err := d.save(); err != nil {
		d.settings = prev
		d.mu.Unlock()
		return fmt.Errorf("couldn't save settings: %w", err)
	}
	d.mu.Unlock()
	// Act on the change now rather than on the next tick.
	d.Tick()
	return nil
}

// Run takes over the fans until ctx is done, then returns them to firmware.
func (d *Daemon) Run(ctx context.Context, socket string) error {
	d.Load()
	d.mu.Lock()
	// A previous run may have left manual duty behind; assume so until the EC confirms.
	d.manual = true
	_ = d.release("startup")
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.stopped = true
		for i := 0; i < 3; i++ {
			d.manual = true
			if d.release("shutdown") == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		d.mu.Unlock()
	}()

	ln, err := listen(socket)
	if err != nil {
		return err
	}
	defer ln.Close()
	go d.serve(ctx, ln)

	d.Tick()
	notify("READY=1")
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			notify("STOPPING=1")
			return nil
		case <-t.C:
			d.Tick()
			// A hung tick stops these, so systemd's watchdog restarts us and restore runs.
			notify("WATCHDOG=1")
		}
	}
}

// notify sends a systemd sd_notify message when running under systemd.
func notify(state string) {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return
	}
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

func listen(socket string) (net.Listener, error) {
	if socket == "" {
		socket = ipc.DefaultSocket
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(socket); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	// Anyone may read status; changes are checked per connection with SO_PEERCRED.
	if err := os.Chmod(socket, 0o666); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// MaxClients bounds concurrent connections so local users can't exhaust memory.
const MaxClients = 32

func (d *Daemon) serve(ctx context.Context, ln net.Listener) {
	go func() { <-ctx.Done(); ln.Close() }()
	slots := make(chan struct{}, MaxClients)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			d.logf("accept: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		select {
		case slots <- struct{}{}:
			go func() { defer func() { <-slots }(); d.handle(conn) }()
		default:
			conn.Close()
		}
	}
}

func (d *Daemon) handle(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	reply := func(resp ipc.Response) { _ = json.NewEncoder(conn).Encode(resp) }

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 1024), ipc.MaxRequest)
	if !sc.Scan() {
		return
	}
	var req ipc.Request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		reply(ipc.Response{Error: "invalid request"})
		return
	}
	if req.Cmd != "status" {
		uid, err := peerUID(conn)
		if err != nil || d.Authorize == nil || !d.Authorize(uid) {
			reply(ipc.Response{Error: "permission denied: changing fan settings needs an administrator (wheel) account"})
			return
		}
	}
	if err := d.Apply(req); err != nil {
		st := d.Status()
		reply(ipc.Response{Error: err.Error(), Status: &st})
		return
	}
	st := d.Status()
	reply(ipc.Response{OK: true, Status: &st})
}

func peerUID(conn net.Conn) (uint32, error) {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return 0, errors.New("not a unix socket")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	return cred.Uid, nil
}

// WheelOrRoot allows root and members of the wheel group.
func WheelOrRoot(uid uint32) bool {
	if uid == 0 {
		return true
	}
	u, err := user.LookupId(fmt.Sprint(uid))
	if err != nil {
		return false
	}
	wheel, err := user.LookupGroup("wheel")
	if err != nil {
		return false
	}
	groups, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, g := range groups {
		if g == wheel.Gid {
			return true
		}
	}
	return false
}
