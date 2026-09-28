// Package sensors reads Framework Desktop temperatures and fan speeds from hwmon.
//
// hwmon indices change between boots, so devices are matched by name and label.
package sensors

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultRoot is where the kernel exposes hwmon devices.
const DefaultRoot = "/sys/class/hwmon"

// Names the Framework EC uses for its fan slots.
var fanNames = []string{"APU fan", "Front fan", "Third fan"}

// Fan is one EC fan tachometer.
type Fan struct {
	Name string `json:"name"`
	RPM  int    `json:"rpm"`
}

// Reading is one snapshot. Temperatures are nil when a sensor is missing.
type Reading struct {
	CPU  *float64 `json:"cpu"` // k10temp Tctl
	GPU  *float64 `json:"gpu"` // amdgpu edge
	APU  *float64 `json:"apu"` // EC's APU sensor (cros_ec cpu@4c)
	Fans []Fan    `json:"fans"`
}

// Hottest is the temperature that drives the curve: the hottest die sensor.
func (r Reading) Hottest() (float64, bool) {
	best, ok := 0.0, false
	for _, v := range []*float64{r.CPU, r.GPU, r.APU} {
		if v != nil && (!ok || *v > best) {
			best, ok = *v, true
		}
	}
	return best, ok
}

// FanRPM is the fastest fan, the one worth showing.
func (r Reading) FanRPM() int {
	best := 0
	for _, f := range r.Fans {
		if f.RPM > best {
			best = f.RPM
		}
	}
	return best
}

// Source reads hwmon under Root.
type Source struct {
	Root string
}

// ErrNoSensors means neither CPU temperature (k10temp Tctl or the EC's APU
// sensor) could be read. The GPU edge sensor runs cooler and lags, so it alone
// never drives the fans.
var ErrNoSensors = errors.New("no CPU temperature from k10temp or the EC")

// Read takes one snapshot.
func (s Source) Read() (Reading, error) {
	root := s.Root
	if root == "" {
		root = DefaultRoot
	}
	dirs, err := filepath.Glob(filepath.Join(root, "hwmon*"))
	if err != nil {
		return Reading{}, err
	}
	sort.Strings(dirs)

	var r Reading
	for _, dir := range dirs {
		switch readString(filepath.Join(dir, "name")) {
		case "k10temp":
			if r.CPU == nil {
				r.CPU = labeledTemp(dir, "Tctl")
			}
		case "amdgpu":
			if r.GPU == nil {
				r.GPU = labeledTemp(dir, "edge")
			}
		case "cros_ec":
			if r.APU == nil {
				r.APU = labeledTemp(dir, "cpu@4c")
			}
			if r.Fans == nil {
				r.Fans = fans(dir)
			}
		}
	}
	if r.CPU == nil && r.APU == nil {
		return r, ErrNoSensors
	}
	return r, nil
}

func labeledTemp(dir, label string) *float64 {
	labels, _ := filepath.Glob(filepath.Join(dir, "temp*_label"))
	sort.Strings(labels)
	for _, l := range labels {
		if readString(l) != label {
			continue
		}
		if v, ok := readInt(strings.TrimSuffix(l, "_label") + "_input"); ok {
			c := float64(v) / 1000
			// Reject obviously bogus values rather than drive the fan with them.
			if c > -40 && c < 150 {
				return &c
			}
		}
	}
	return nil
}

func fans(dir string) []Fan {
	var out []Fan
	for i := 1; i <= 8; i++ {
		v, ok := readInt(filepath.Join(dir, "fan"+strconv.Itoa(i)+"_input"))
		if !ok {
			continue
		}
		name := "Fan " + strconv.Itoa(i)
		if i <= len(fanNames) {
			name = fanNames[i-1]
		}
		out = append(out, Fan{Name: name, RPM: v})
	}
	return out
}

func readString(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readInt(path string) (int, bool) {
	v, err := strconv.Atoi(readString(path))
	return v, err == nil
}
