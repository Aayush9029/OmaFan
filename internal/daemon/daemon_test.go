package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Aayush9029/OmaFan/internal/curve"
	"github.com/Aayush9029/OmaFan/internal/ipc"
	"github.com/Aayush9029/OmaFan/internal/sensors"
)

type fakeFans struct {
	mu       sync.Mutex
	duties   []int
	autos    int
	failSet  bool
	failAuto int // fail this many Auto calls
}

func (f *fakeFans) SetDuty(p int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSet {
		return errors.New("ec busy")
	}
	f.duties = append(f.duties, p)
	return nil
}
func (f *fakeFans) Auto() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.autos++
	if f.failAuto > 0 {
		f.failAuto--
		return errors.New("ec busy")
	}
	return nil
}
func (f *fakeFans) Close() error { return nil }
func (f *fakeFans) last() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.duties) == 0 {
		return -1
	}
	return f.duties[len(f.duties)-1]
}

type fakeSensors struct {
	mu   sync.Mutex
	temp float64
	err  error
}

func (s *fakeSensors) Read() (sensors.Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return sensors.Reading{}, s.err
	}
	t := s.temp
	return sensors.Reading{CPU: &t, Fans: []sensors.Fan{{Name: "APU fan", RPM: 1200}}}, nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newDaemon(t *testing.T) (*Daemon, *fakeFans, *fakeSensors, *clock) {
	t.Helper()
	fans := &fakeFans{}
	sens := &fakeSensors{temp: 68}
	clk := &clock{t: time.Unix(1000, 0)}
	d := &Daemon{
		StatePath: filepath.Join(t.TempDir(), "settings.json"),
		Fans:      fans,
		Sensors:   sens,
		Now:       clk.now,
		Authorize: func(uid uint32) bool { return true },
	}
	d.Load()
	return d, fans, sens, clk
}

func TestDisabledLeavesFirmwareInCharge(t *testing.T) {
	d, fans, _, _ := newDaemon(t)
	d.Tick()
	if fans.last() != -1 {
		t.Fatalf("disabled daemon set duty %d", fans.last())
	}
	if st := d.Status(); st.Controller != ipc.ControllerFirmware || st.Duty != nil {
		t.Fatalf("status = %+v", st)
	}
}

func TestEnableAppliesCurveAndDisableReturnsToFirmware(t *testing.T) {
	d, fans, _, _ := newDaemon(t)
	if err := d.Apply(ipc.Request{Cmd: "enable"}); err != nil {
		t.Fatal(err)
	}
	if fans.last() != 56 {
		t.Fatalf("balanced at 68°C sent %d%%, want 56", fans.last())
	}
	st := d.Status()
	if st.FanMaxRPM != DefaultMaxRPM {
		t.Fatalf("fanMaxRpm = %d", st.FanMaxRPM)
	}
	if st.Controller != ipc.ControllerOmaFan || st.Duty == nil || *st.Duty != 56 {
		t.Fatalf("status = %+v", st)
	}
	autos := fans.autos
	if err := d.Apply(ipc.Request{Cmd: "disable"}); err != nil {
		t.Fatal(err)
	}
	if fans.autos != autos+1 {
		t.Fatal("disable did not return fans to firmware")
	}
}

func TestReassertsDutyPeriodically(t *testing.T) {
	d, fans, _, clk := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	n := len(fans.duties)
	clk.advance(time.Second)
	d.Tick()
	if len(fans.duties) != n {
		t.Fatal("unchanged duty was resent early")
	}
	clk.advance(Reassert)
	d.Tick()
	if len(fans.duties) != n+1 {
		t.Fatal("duty was not reasserted")
	}
}

func TestSensorLossReturnsToFirmwareAfterGrace(t *testing.T) {
	d, fans, sens, clk := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	autos := fans.autos
	sens.err = sensors.ErrNoSensors
	clk.advance(time.Second)
	d.Tick()
	if fans.autos != autos {
		t.Fatal("released before the grace period")
	}
	clk.advance(SensorGrace)
	d.Tick()
	if fans.autos != autos+1 {
		t.Fatal("did not release after losing sensors")
	}
	if st := d.Status(); st.Controller != ipc.ControllerFirmware || st.Error == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestRepeatedECFailuresReturnToFirmware(t *testing.T) {
	d, fans, sens, clk := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	fans.failSet = true
	autos := fans.autos
	for i := 0; i < MaxECFailures; i++ {
		sens.temp += 5
		clk.advance(time.Second)
		d.Tick()
	}
	if fans.autos != autos+1 {
		t.Fatal("did not release after repeated EC failures")
	}
}

func TestCriticalTemperatureReportsEmergency(t *testing.T) {
	d, fans, sens, _ := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	sens.temp = 92
	d.Tick()
	if fans.last() != 100 || d.Status().Controller != ipc.ControllerEmergency {
		t.Fatalf("critical temp sent %d%%, controller %s", fans.last(), d.Status().Controller)
	}
}

func TestCurveIsValidatedPersistedAndReloaded(t *testing.T) {
	d, _, _, _ := newDaemon(t)
	if err := d.Apply(ipc.Request{Cmd: "curve", Points: []curve.Point{{T: 60, F: 10}, {T: 50, F: 90}}}); err == nil {
		t.Fatal("accepted a backwards curve")
	}
	pts := []curve.Point{{T: 40, F: 10}, {T: 60, F: 40}, {T: 80, F: 100}}
	if err := d.Apply(ipc.Request{Cmd: "curve", Points: pts}); err != nil {
		t.Fatal(err)
	}
	_ = d.Apply(ipc.Request{Cmd: "enable"})

	again := &Daemon{StatePath: d.StatePath, Fans: &fakeFans{}, Sensors: &fakeSensors{temp: 50}}
	again.Load()
	st := again.Status()
	if !st.Enabled || st.Profile != "custom" || len(st.Points) != curve.MaxPoints || st.Points[0] != pts[0] {
		t.Fatalf("reloaded = %+v", st)
	}
}

func TestRunReturnsFansToFirmwareOnShutdown(t *testing.T) {
	d, fans, _, _ := newDaemon(t)
	d.Now = nil
	socket := filepath.Join(t.TempDir(), "omafan.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- d.Run(ctx, socket) }()

	var st *ipc.Status
	var err error
	for i := 0; i < 50; i++ {
		if st, err = ipc.Call(socket, ipc.Request{Cmd: "enable"}); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || !st.Enabled || fans.last() != 56 {
		t.Fatalf("enable over socket: %v %+v", err, st)
	}
	autos := fans.autos
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if fans.autos != autos+1 {
		t.Fatal("shutdown did not return fans to firmware")
	}
}

func TestUnauthorizedPeerCanOnlyReadStatus(t *testing.T) {
	d, _, _, _ := newDaemon(t)
	d.Now = nil
	d.Authorize = func(uint32) bool { return false }
	socket := filepath.Join(t.TempDir(), "omafan.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx, socket)

	var err error
	for i := 0; i < 50; i++ {
		if _, err = ipc.Call(socket, ipc.Request{Cmd: "status"}); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("status denied: %v", err)
	}
	if _, err := ipc.Call(socket, ipc.Request{Cmd: "enable"}); err == nil {
		t.Fatal("unauthorized peer changed settings")
	}
	if d.Status().Enabled {
		t.Fatal("settings changed despite denial")
	}
}

func TestFailedHandbackIsRetriedUntilTheECAccepts(t *testing.T) {
	d, fans, _, clk := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	fans.failAuto = 2
	autos := fans.autos
	_ = d.Apply(ipc.Request{Cmd: "disable"})
	if st := d.Status(); st.Controller == ipc.ControllerFirmware || st.Error == "" {
		t.Fatalf("failed handback reported as done: %+v", st)
	}
	for i := 0; i < 3; i++ {
		clk.advance(time.Second)
		d.Tick()
	}
	if fans.autos != autos+3 {
		t.Fatalf("Auto called %d times, want 3", fans.autos-autos)
	}
	if st := d.Status(); st.Controller != ipc.ControllerFirmware || st.Error != "" {
		t.Fatalf("after retry status = %+v", st)
	}
	clk.advance(time.Second)
	d.Tick()
	if fans.autos != autos+3 {
		t.Fatal("kept calling Auto after the EC accepted")
	}
}

func TestSensorLossRetriesHandback(t *testing.T) {
	d, fans, sens, clk := newDaemon(t)
	_ = d.Apply(ipc.Request{Cmd: "enable"})
	sens.err = sensors.ErrNoSensors
	fans.failAuto = 1
	autos := fans.autos
	for i := 0; i < 8; i++ {
		clk.advance(time.Second)
		d.Tick()
	}
	if fans.autos != autos+2 || d.Status().Controller != ipc.ControllerFirmware {
		t.Fatalf("autos = %d, status %+v", fans.autos-autos, d.Status())
	}
}

func TestStoppedDaemonRefusesChanges(t *testing.T) {
	d, fans, _, _ := newDaemon(t)
	d.mu.Lock()
	d.stopped = true
	d.mu.Unlock()
	if err := d.Apply(ipc.Request{Cmd: "enable"}); err == nil {
		t.Fatal("stopped daemon accepted enable")
	}
	d.Tick()
	if fans.last() != -1 {
		t.Fatal("stopped daemon set a duty")
	}
}
