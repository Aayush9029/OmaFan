package curve

import (
	"math"
	"testing"
)

func TestEvaluatePassesThroughPointsAndIsFlatOutside(t *testing.T) {
	p, _ := Preset("balanced")
	for _, pt := range p {
		if got := Evaluate(p, float64(pt.T)); math.Abs(got-float64(pt.F)) > 1e-9 {
			t.Fatalf("Evaluate(%d) = %v, want %d", pt.T, got, pt.F)
		}
	}
	if got := Evaluate(p, 20); got != 25 {
		t.Fatalf("below first point = %v, want 25", got)
	}
	if got := Evaluate(p, 99); got != 90 {
		t.Fatalf("above last point = %v, want 90", got)
	}
}

func TestEvaluateIsMonotoneForEveryPreset(t *testing.T) {
	for _, name := range []string{"quiet", "balanced", "blast"} {
		p, _ := Preset(name)
		prev := -1.0
		for temp := 30.0; temp <= 100; temp += 0.25 {
			v := Evaluate(p, temp)
			if v < prev-1e-9 {
				t.Fatalf("%s: fan dropped from %v to %v at %v°C", name, prev, v, temp)
			}
			prev = v
		}
	}
}

func TestEvaluateNeverOvershootsSteepCurve(t *testing.T) {
	p := []Point{{40, 0}, {45, 100}, {90, 100}}
	for temp := 30.0; temp <= 100; temp += 0.1 {
		if v := Evaluate(p, temp); v < 0 || v > 100 {
			t.Fatalf("Evaluate(%v) = %v out of range", temp, v)
		}
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	p, err := Parse(" 45:25, 68:55%,85:90 ")
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(p); got != "45:25,68:55,85:90" {
		t.Fatalf("Format = %q", got)
	}
}

func TestValidateRejectsBadCurves(t *testing.T) {
	bad := [][]Point{
		{{50, 20}},
		{{50, 20}, {52, 30}},
		{{60, 20}, {50, 30}},
		{{20, 20}, {60, 30}},
		{{50, 20}, {60, 130}},
		{{35, 10}, {45, 20}, {55, 30}, {65, 40}, {75, 50}},
	}
	for _, p := range bad {
		if err := Validate(p); err == nil {
			t.Fatalf("Validate(%v) accepted a bad curve", p)
		}
	}
}

func TestPresetReturnsCopy(t *testing.T) {
	p, _ := Preset("quiet")
	p[0].F = 99
	q, _ := Preset("quiet")
	if q[0].F == 99 {
		t.Fatal("Preset leaked its backing array")
	}
}
