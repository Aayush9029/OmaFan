// Package ec sends fan commands to the Framework embedded controller.
package ec

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// DefaultDevice is the cros_ec character device.
const DefaultDevice = "/dev/cros_ec"

// Host commands from ChromeOS EC include/ec_commands.h.
const (
	cmdPWMSetFanDuty     = 0x0024 // v0: { uint32 percent } for every fan
	cmdThermalAutoFanCtl = 0x0052 // v0: no params, every fan back to EC control
)

// CROS_EC_DEV_IOCXCMD_V2 = _IOWR(0xEC, 0, struct cros_ec_command_v2), a 20-byte header.
const ioctlXcmdV2 = 0xC014EC00

const headerSize = 20

// Fans is what the daemon needs from the EC; tests use a fake.
type Fans interface {
	SetDuty(percent int) error
	Auto() error
	Close() error
}

// Device talks to the EC through /dev/cros_ec.
type Device struct {
	mu sync.Mutex
	f  *os.File
}

// Open opens the EC device.
func Open(path string) (*Device, error) {
	if path == "" {
		path = DefaultDevice
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &Device{f: f}, nil
}

// SetDuty sets every fan to a fixed duty and takes it off EC auto control.
func (d *Device) SetDuty(percent int) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("fan duty %d%% out of range", percent)
	}
	params := make([]byte, 4)
	binary.LittleEndian.PutUint32(params, uint32(percent))
	return d.command(cmdPWMSetFanDuty, 0, params)
}

// Auto returns every fan to the EC's own thermal control.
func (d *Device) Auto() error {
	return d.command(cmdThermalAutoFanCtl, 0, nil)
}

// Close releases the device.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.f.Close()
}

// command sends a host command with no response payload.
func (d *Device) command(cmd, version uint32, params []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	buf := make([]byte, headerSize+len(params))
	binary.LittleEndian.PutUint32(buf[0:], version)
	binary.LittleEndian.PutUint32(buf[4:], cmd)
	binary.LittleEndian.PutUint32(buf[8:], uint32(len(params))) // outsize
	binary.LittleEndian.PutUint32(buf[12:], 0)                  // insize
	copy(buf[headerSize:], params)

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, d.f.Fd(), ioctlXcmdV2, uintptr(unsafe.Pointer(&buf[0])))
	if errno != 0 {
		return fmt.Errorf("EC command 0x%04x: %w", cmd, errno)
	}
	if result := binary.LittleEndian.Uint32(buf[16:]); result != 0 {
		return fmt.Errorf("EC command 0x%04x failed with result %d", cmd, result)
	}
	return nil
}
