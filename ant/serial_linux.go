//go:build linux

package ant

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// OpenSerial opens an ANT USB stick exposed as a tty by the kernel's
// usb_serial_simple driver (ANTUSB2 0fcf:1008 and ANTUSB-m 0fcf:1009) and
// puts it in raw mode with exclusive access. No libusb or cgo needed.
func OpenSerial(path string) (io.ReadWriteCloser, error) {
	// O_NONBLOCK keeps open from waiting on carrier detect; Go's poller
	// handles the non-blocking fd, and Close unblocks a pending Read.
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	rc, err := f.SyscallConn()
	if err != nil {
		f.Close()
		return nil, err
	}
	var rawErr error
	if err := rc.Control(func(fd uintptr) { rawErr = makeRaw(fd) }); err != nil {
		f.Close()
		return nil, err
	}
	if rawErr != nil {
		f.Close()
		return nil, fmt.Errorf("ant: configure %s: %w", path, rawErr)
	}
	return f, nil
}

// Linux termios masks that package syscall does not export.
const (
	cbaud   = 0x100F
	crtscts = 0x80000000
)

func makeRaw(fd uintptr) error {
	var t syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&t)); err != nil {
		return err
	}
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON | syscall.IXOFF
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	// The baud rate is ignored by usb_serial_simple; set one anyway so the
	// tty state is well defined.
	t.Cflag &^= syscall.CSIZE | syscall.PARENB | cbaud | crtscts
	t.Cflag |= syscall.CS8 | syscall.CLOCAL | syscall.CREAD | syscall.B115200
	t.Ispeed, t.Ospeed = syscall.B115200, syscall.B115200
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return err
	}
	if err := ioctl(fd, syscall.TIOCEXCL, nil); err != nil {
		return fmt.Errorf("exclusive access: %w", err)
	}
	// Discard anything the stick sent before we opened it.
	const tcflsh, tciflush = 0x540B, 0
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, tcflsh, tciflush)
	if e != 0 {
		return fmt.Errorf("flush: %w", e)
	}
	return nil
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}
