//go:build !linux

package ant

import (
	"errors"
	"io"
)

// OpenSerial is only implemented on Linux, where usb_serial_simple exposes
// the stick as a tty. Other platforms will need a libusb transport.
func OpenSerial(path string) (io.ReadWriteCloser, error) {
	return nil, errors.New("ant: serial transport is only supported on linux")
}
