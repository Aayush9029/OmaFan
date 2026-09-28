package sensors

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A copy of the Framework Desktop's hwmon layout, with shuffled indices.
func fakeDesktop(t *testing.T) string {
	root := t.TempDir()
	write(t, root+"/hwmon1/name", "acpitz")
	write(t, root+"/hwmon1/temp1_input", "90000")
	write(t, root+"/hwmon4/name", "amdgpu")
	write(t, root+"/hwmon4/temp1_label", "edge")
	write(t, root+"/hwmon4/temp1_input", "48000")
	write(t, root+"/hwmon5/name", "k10temp")
	write(t, root+"/hwmon5/temp1_label", "Tctl")
	write(t, root+"/hwmon5/temp1_input", "61250")
	write(t, root+"/hwmon8/name", "cros_ec")
	write(t, root+"/hwmon8/temp4_label", "cpu@4c")
	write(t, root+"/hwmon8/temp4_input", "59900")
	write(t, root+"/hwmon8/temp1_label", "mainboard_power@4d")
	write(t, root+"/hwmon8/temp1_input", "70000")
	write(t, root+"/hwmon8/fan1_input", "1533")
	write(t, root+"/hwmon8/fan2_input", "0")
	write(t, root+"/hwmon8/fan3_input", "0")
	return root
}

func TestReadFindsSensorsByNameAndLabel(t *testing.T) {
	r, err := Source{Root: fakeDesktop(t)}.Read()
	if err != nil {
		t.Fatal(err)
	}
	if r.CPU == nil || *r.CPU != 61.25 || r.GPU == nil || *r.GPU != 48 || r.APU == nil || *r.APU != 59.9 {
		t.Fatalf("temps = cpu %v gpu %v apu %v", r.CPU, r.GPU, r.APU)
	}
	if len(r.Fans) != 3 || r.Fans[0].Name != "APU fan" || r.FanRPM() != 1533 {
		t.Fatalf("fans = %+v", r.Fans)
	}
	// acpitz and board sensors must not drive the curve.
	if hot, _ := r.Hottest(); hot != 61.25 {
		t.Fatalf("hottest = %v, want 61.25", hot)
	}
}

func TestReadFailsWithoutTemperatures(t *testing.T) {
	root := t.TempDir()
	write(t, root+"/hwmon0/name", "cros_ec")
	write(t, root+"/hwmon0/fan1_input", "900")
	if _, err := (Source{Root: root}).Read(); err != ErrNoSensors {
		t.Fatalf("err = %v, want ErrNoSensors", err)
	}
}

func TestBogusTemperatureIsIgnored(t *testing.T) {
	root := fakeDesktop(t)
	write(t, root+"/hwmon5/temp1_input", "-273000")
	r, err := Source{Root: root}.Read()
	if err != nil {
		t.Fatal(err)
	}
	if r.CPU != nil {
		t.Fatalf("bogus CPU temp accepted: %v", *r.CPU)
	}
	if hot, _ := r.Hottest(); hot != 59.9 {
		t.Fatalf("hottest = %v, want EC APU 59.9", hot)
	}
}
