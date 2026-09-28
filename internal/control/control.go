// Package control turns temperature readings into a smooth fan duty.
package control

import (
	"math"
	"time"

	"github.com/Aayush9029/OmaFan/internal/curve"
)

const (
	// Spin up quickly but not instantly, so short Tctl spikes don't pulse the fan.
	RiseRate = 8.0 // percent per second
	// Spin down slowly so the fan doesn't hunt around a threshold.
	FallRate = 2.0 // percent per second
	// Ignore tiny decreases to avoid constant micro-adjustments.
	Deadband = 2.0 // percent

	// Safety floors apply whatever the curve says. Strix Halo's Tjmax is 100°C.
	HotTemp      = 85.0 // °C: at least HotFloor
	HotFloor     = 70.0
	CriticalTemp = 90.0 // °C: full speed immediately
)

// Controller holds the smoothed duty between ticks.
type Controller struct {
	duty    float64
	started bool
	last    time.Time
}

// Result is one control decision.
type Result struct {
	Duty      int     // percent to send to the EC
	Target    float64 // curve value before smoothing and floors
	Emergency bool    // true when the critical floor forced full speed
}

// Reset forgets the smoothed duty, e.g. after control was handed to firmware.
func (c *Controller) Reset() { *c = Controller{} }

// Step computes the duty for temp at time now.
func (c *Controller) Step(points []curve.Point, temp float64, now time.Time) Result {
	target := curve.Evaluate(points, temp)
	floor := 0.0
	if temp >= HotTemp {
		floor = HotFloor
	}
	if temp >= CriticalTemp {
		c.duty, c.started, c.last = 100, true, now
		return Result{Duty: 100, Target: target, Emergency: true}
	}
	want := math.Max(target, floor)

	if !c.started {
		c.duty, c.started, c.last = want, true, now
		return Result{Duty: round(c.duty), Target: target}
	}

	dt := now.Sub(c.last).Seconds()
	c.last = now
	if dt <= 0 {
		dt = 0
	}
	if dt > 5 {
		dt = 5
	}

	switch {
	case want > c.duty:
		c.duty = math.Min(want, c.duty+RiseRate*dt)
	case c.duty-want > Deadband:
		c.duty = math.Max(want, c.duty-FallRate*dt)
	}
	// Floors are never smoothed away.
	c.duty = math.Max(c.duty, floor)
	return Result{Duty: round(c.duty), Target: target}
}

func round(v float64) int {
	return int(math.Round(math.Max(0, math.Min(100, v))))
}
