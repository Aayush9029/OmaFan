package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Aayush9029/OmaFan/internal/curve"
	"github.com/Aayush9029/OmaFan/internal/daemon"
	"github.com/Aayush9029/OmaFan/internal/ec"
	"github.com/Aayush9029/OmaFan/internal/ipc"
	"github.com/Aayush9029/OmaFan/internal/sensors"
)

var version = "0.1.0"

const usage = `OmaFan controls Framework Desktop fans.

Usage:
  omafan status [--json]         Show temperatures, fan speed, and the active curve
  omafan enable                  Let OmaFan drive the fans with the active curve
  omafan disable                 Hand the fans back to the firmware
  omafan profile <name>          quiet, balanced, blast, or custom
  omafan curve <T:F,T:F,...>     Save and use a custom curve, e.g. 45:25,68:55,85:90
  omafan daemon                  Run the control service (root)
  omafan restore                 Return the fans to firmware control (root)
  omafan version
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "omafan:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	switch args[0] {
	case "status":
		st, err := ipc.Call("", ipc.Request{Cmd: "status"})
		if err != nil {
			return err
		}
		if len(args) > 1 && args[1] == "--json" {
			return json.NewEncoder(os.Stdout).Encode(st)
		}
		printStatus(st)
		return nil
	case "enable", "disable":
		return change(ipc.Request{Cmd: args[0]})
	case "profile":
		if len(args) != 2 {
			return errors.New("usage: omafan profile " + strings.Join(curve.Profiles, "|"))
		}
		return change(ipc.Request{Cmd: "profile", Profile: args[1]})
	case "curve":
		if len(args) != 2 {
			return errors.New("usage: omafan curve 45:25,68:55,85:90")
		}
		points, err := curve.Parse(args[1])
		if err != nil {
			return err
		}
		return change(ipc.Request{Cmd: "curve", Points: points})
	case "daemon":
		return runDaemon()
	case "restore":
		dev, err := ec.Open("")
		if err != nil {
			return err
		}
		defer dev.Close()
		return dev.Auto()
	case "version", "--version":
		fmt.Println("omafan", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
}

func change(req ipc.Request) error {
	st, err := ipc.Call("", req)
	if err != nil {
		return err
	}
	printStatus(st)
	return nil
}

func runDaemon() error {
	if os.Geteuid() != 0 {
		return errors.New("the daemon needs root to reach the embedded controller")
	}
	if err := supported(); err != nil && os.Getenv("OMAFAN_ALLOW_UNSUPPORTED") != "1" {
		return err
	}
	dev, err := ec.Open("")
	if err != nil {
		return err
	}
	defer dev.Close()

	stateDir := os.Getenv("STATE_DIRECTORY")
	if stateDir == "" {
		stateDir = "/var/lib/omafan"
	}
	d := &daemon.Daemon{
		Version:   version,
		StatePath: filepath.Join(stateDir, "settings.json"),
		Fans:      dev,
		Sensors:   sensors.Source{},
		Log:       log.New(os.Stderr, "", 0),
		Authorize: daemon.WheelOrRoot,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return d.Run(ctx, "")
}

// supported checks for a Framework machine; the EC commands are Framework-specific.
func supported() error {
	vendor, _ := os.ReadFile("/sys/class/dmi/id/sys_vendor")
	if strings.TrimSpace(string(vendor)) != "Framework" {
		return errors.New("OmaFan only supports Framework computers (set OMAFAN_ALLOW_UNSUPPORTED=1 to override)")
	}
	return nil
}

func printStatus(st *ipc.Status) {
	if st == nil {
		return
	}
	state := "off · firmware controls the fans"
	switch {
	case st.Controller == ipc.ControllerEmergency:
		state = "on · full speed (critical temperature)"
	case st.Enabled && st.Controller == ipc.ControllerOmaFan:
		state = "on · " + st.Profile + " curve"
	case st.Enabled:
		state = "on · waiting (firmware in control)"
	}
	fmt.Println("OmaFan:", state)
	fmt.Printf("Temperature: %s (CPU %s, GPU %s, EC %s)\n", temp(st.Temp), temp(st.CPU), temp(st.GPU), temp(st.APU))
	fan := fmt.Sprintf("%d RPM", st.FanRPM)
	if st.Duty != nil {
		fan += fmt.Sprintf(" at %d%%", *st.Duty)
	}
	fmt.Println("Fan:", fan)
	fmt.Println("Curve:", curve.Format(st.Points))
	if st.Error != "" {
		fmt.Println("Error:", st.Error)
	}
}

func temp(v *float64) string {
	if v == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f°C", *v)
}
