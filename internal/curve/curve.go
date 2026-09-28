// Package curve evaluates fan curves: fan percent as a function of temperature.
//
// The math matches omarchy/local.omafan/Model.js so the panel draws exactly the
// curve the daemon applies: a monotone cubic (Fritsch-Carlson) through the
// points, flat before the first point and after the last.
package curve

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	TempMin = 30
	TempMax = 100
	FanMin  = 0
	FanMax  = 100
	MinGap  = 5
	// MinPoints and MaxPoints bound the number of control points in a curve.
	MinPoints = 2
	MaxPoints = 4
)

// Point is one control point: temperature in °C and fan speed in percent.
type Point struct {
	T int `json:"t"`
	F int `json:"f"`
}

// Profiles in display order. "custom" uses the user's own points.
var Profiles = []string{"quiet", "balanced", "blast", "custom"}

var presets = map[string][]Point{
	"quiet":    {{45, 15}, {60, 30}, {75, 50}, {90, 85}},
	"balanced": {{40, 20}, {55, 35}, {70, 60}, {85, 90}},
	"blast":    {{35, 45}, {50, 65}, {62, 85}, {75, 100}},
}

// Preset returns a copy of a built-in profile's points.
func Preset(name string) ([]Point, bool) {
	p, ok := presets[name]
	if !ok {
		return nil, false
	}
	return append([]Point(nil), p...), true
}

// ValidProfile reports whether name is a known profile.
func ValidProfile(name string) bool {
	for _, p := range Profiles {
		if p == name {
			return true
		}
	}
	return false
}

// Validate checks bounds and ordering so the curve is a function of temperature.
func Validate(points []Point) error {
	if len(points) < MinPoints || len(points) > MaxPoints {
		return fmt.Errorf("a curve needs %d to %d points, got %d", MinPoints, MaxPoints, len(points))
	}
	for i, p := range points {
		if p.T < TempMin || p.T > TempMax {
			return fmt.Errorf("point %d: temperature %d°C is outside %d-%d°C", i+1, p.T, TempMin, TempMax)
		}
		if p.F < FanMin || p.F > FanMax {
			return fmt.Errorf("point %d: fan speed %d%% is outside %d-%d%%", i+1, p.F, FanMin, FanMax)
		}
		if i > 0 && p.T-points[i-1].T < MinGap {
			return fmt.Errorf("points must be at least %d°C apart and in rising order", MinGap)
		}
	}
	return nil
}

// Expand adds points in the widest gaps until the curve has MaxPoints,
// placing each on the existing curve so its shape barely changes.
func Expand(points []Point) []Point {
	out := append([]Point(nil), points...)
	for len(out) >= MinPoints && len(out) < MaxPoints {
		wide := 0
		for i := 1; i < len(out)-1; i++ {
			if out[i+1].T-out[i].T > out[wide+1].T-out[wide].T {
				wide = i
			}
		}
		gap := out[wide+1].T - out[wide].T
		if gap < 2*MinGap {
			break
		}
		t := out[wide].T + gap/2
		p := Point{T: t, F: int(math.Round(Evaluate(out, float64(t))))}
		out = append(out[:wide+1], append([]Point{p}, out[wide+1:]...)...)
	}
	return out
}

// Parse reads "45:25,68:55,85:90" into validated points.
func Parse(s string) ([]Point, error) {
	var points []Point
	for _, part := range strings.Split(strings.TrimSpace(s), ",") {
		t, f, ok := strings.Cut(strings.TrimSpace(part), ":")
		if !ok {
			return nil, fmt.Errorf("point %q must look like TEMP:FAN", part)
		}
		ti, err1 := strconv.Atoi(strings.TrimSpace(t))
		fi, err2 := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(f, "%")))
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("point %q must use whole numbers", part)
		}
		points = append(points, Point{ti, fi})
	}
	if err := Validate(points); err != nil {
		return nil, err
	}
	return points, nil
}

// Format writes points as "45:25,68:55,85:90".
func Format(points []Point) string {
	parts := make([]string, len(points))
	for i, p := range points {
		parts[i] = fmt.Sprintf("%d:%d", p.T, p.F)
	}
	return strings.Join(parts, ",")
}

func tangents(points []Point) []float64 {
	n := len(points)
	d := make([]float64, n-1)
	m := make([]float64, n)
	for i := 0; i < n-1; i++ {
		d[i] = float64(points[i+1].F-points[i].F) / math.Max(1e-6, float64(points[i+1].T-points[i].T))
	}
	for j := 0; j < n; j++ {
		switch {
		case j == 0:
			m[j] = d[0]
		case j == n-1:
			m[j] = d[n-2]
		case d[j-1]*d[j] <= 0:
			m[j] = 0
		default:
			m[j] = (d[j-1] + d[j]) / 2
		}
	}
	for k := 0; k < n-1; k++ {
		if d[k] == 0 {
			m[k], m[k+1] = 0, 0
			continue
		}
		a, b := m[k]/d[k], m[k+1]/d[k]
		if s := a*a + b*b; s > 9 {
			tau := 3 / math.Sqrt(s)
			m[k] = tau * a * d[k]
			m[k+1] = tau * b * d[k]
		}
	}
	return m
}

// Evaluate returns the fan percent for a temperature.
func Evaluate(points []Point, temp float64) float64 {
	if len(points) == 0 {
		return FanMax
	}
	if temp <= float64(points[0].T) {
		return float64(points[0].F)
	}
	last := points[len(points)-1]
	if temp >= float64(last.T) || len(points) == 1 {
		return float64(last.F)
	}
	m := tangents(points)
	for i := 0; i < len(points)-1; i++ {
		a, b := points[i], points[i+1]
		if temp > float64(b.T) {
			continue
		}
		h := float64(b.T - a.T)
		s := (temp - float64(a.T)) / h
		s2, s3 := s*s, s*s*s
		v := (2*s3-3*s2+1)*float64(a.F) + (s3-2*s2+s)*h*m[i] +
			(-2*s3+3*s2)*float64(b.F) + (s3-s2)*h*m[i+1]
		return math.Max(FanMin, math.Min(FanMax, v))
	}
	return float64(last.F)
}
