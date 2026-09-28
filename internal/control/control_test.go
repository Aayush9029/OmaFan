package control

import (
	"testing"
	"time"

	"github.com/Aayush9029/OmaFan/internal/curve"
)

var flat = []curve.Point{{T: 40, F: 20}, {T: 60, F: 50}, {T: 80, F: 90}}

func TestFirstStepJumpsToCurve(t *testing.T) {
	var c Controller
	r := c.Step(flat, 60, time.Unix(0, 0))
	if r.Duty != 50 {
		t.Fatalf("duty = %d, want 50", r.Duty)
	}
}

func TestRisesAtLimitedRate(t *testing.T) {
	var c Controller
	t0 := time.Unix(0, 0)
	c.Step(flat, 40, t0)
	r := c.Step(flat, 80, t0.Add(time.Second))
	if r.Duty != 28 {
		t.Fatalf("after 1s duty = %d, want 20+8", r.Duty)
	}
}

func TestFallsSlowlyAndHonorsDeadband(t *testing.T) {
	var c Controller
	t0 := time.Unix(0, 0)
	c.Step(flat, 60, t0)
	if r := c.Step(flat, 59.5, t0.Add(time.Second)); r.Duty != 50 {
		t.Fatalf("small drop moved duty to %d", r.Duty)
	}
	if r := c.Step(flat, 40, t0.Add(2*time.Second)); r.Duty != 48 {
		t.Fatalf("after drop duty = %d, want 48", r.Duty)
	}
}

func TestHotFloorAndCriticalOverrideCurve(t *testing.T) {
	quiet := []curve.Point{{T: 40, F: 0}, {T: 60, F: 0}, {T: 90, F: 10}}
	var c Controller
	t0 := time.Unix(0, 0)
	if r := c.Step(quiet, 86, t0); r.Duty != 70 {
		t.Fatalf("hot floor duty = %d, want 70", r.Duty)
	}
	r := c.Step(quiet, 91, t0.Add(time.Second))
	if r.Duty != 100 || !r.Emergency {
		t.Fatalf("critical = %+v, want full speed emergency", r)
	}
	// Cooling below the critical line keeps the hot floor without a sudden drop.
	if r := c.Step(quiet, 87, t0.Add(2*time.Second)); r.Duty != 98 {
		t.Fatalf("after critical duty = %d, want 98", r.Duty)
	}
}

func TestLongGapIsClamped(t *testing.T) {
	var c Controller
	t0 := time.Unix(0, 0)
	c.Step(flat, 40, t0)
	r := c.Step(flat, 80, t0.Add(time.Hour))
	if r.Duty != 60 {
		t.Fatalf("after long gap duty = %d, want 20+8*5", r.Duty)
	}
}
