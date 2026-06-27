package main

// We use raw syscalls instead of Go's os.File and go-evdev wrappers
// for the -a/--active-keys query path for performance reasons. 
// Standard os.File.Fd() calls register file descriptors with the Go
// runtime netpoller, introducing thread synchronization and scheduler
// delay (around 180ms). Raw syscalls bypass this completely, bringing
// execution time down to ~50ms.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/holoplot/go-evdev"
	"github.com/omakoto/evsniff-go/evutil"
)

type rawDevice struct {
	path string
	name string
}

func (r *rawDevice) Path() string {
	return r.path
}

func (r *rawDevice) Name() (string, error) {
	return r.name, nil
}

func doRawIoctl(fd uintptr, code uint32, ptr unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(code), uintptr(ptr))
	if errno != 0 {
		return errno
	}
	return nil
}

func getRawDeviceName(fd uintptr) (string, error) {
	var nameBytes [256]byte
	code := (uint32(2) << 30) | (uint32(256) << 16) | (uint32('E') << 8) | uint32(0x06)
	err := doRawIoctl(fd, code, unsafe.Pointer(&nameBytes[0]))
	if err != nil {
		return "", err
	}
	name := string(nameBytes[:])
	if idx := strings.IndexByte(name, 0); idx != -1 {
		name = name[:idx]
	}
	return name, nil
}

func getSupportedKeys(fd uintptr) ([]byte, error) {
	var bits [767]byte
	code := (uint32(2) << 30) | (uint32(767) << 16) | (uint32('E') << 8) | uint32(0x21)
	err := doRawIoctl(fd, code, unsafe.Pointer(&bits[0]))
	if err != nil {
		return nil, err
	}
	return bits[:], nil
}

func getActiveKeysRaw(fd uintptr) ([]byte, error) {
	var bits [767]byte
	code := (uint32(2) << 30) | (uint32(767) << 16) | (uint32('E') << 8) | uint32(0x18)
	err := doRawIoctl(fd, code, unsafe.Pointer(&bits[0]))
	if err != nil {
		return nil, err
	}
	return bits[:], nil
}

func bitIsSet(bits []byte, bit int) bool {
	return (bits[bit/8] & (1 << (bit % 8))) != 0
}

func printActiveKeysFast(sel evutil.Selector) {
	basePath := "/dev/input"
	files, err := os.ReadDir(basePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot read %s: %v\n", basePath, err)
		return
	}

	activeSet := make(map[string]bool)

	for _, entry := range files {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "event") {
			continue
		}

		path := basePath + "/" + entry.Name()
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}

		name, err := getRawDeviceName(uintptr(fd))
		if err != nil {
			syscall.Close(fd)
			continue
		}

		if !evutil.Matches(sel, &rawDevice{path: path, name: name}) {
			syscall.Close(fd)
			continue
		}

		supported, err := getSupportedKeys(uintptr(fd))
		if err == nil {
			active, err := getActiveKeysRaw(uintptr(fd))
			if err == nil {
				for code := 0; code < 767; code++ {
					if bitIsSet(supported, code) && bitIsSet(active, code) {
						keyName := evdev.CodeName(evdev.EV_KEY, evdev.EvCode(code))
						if keyName != "" && keyName != "UNKNOWN" {
							activeSet[keyName] = true
						}
					}
				}
			}
		}
		syscall.Close(fd)
	}

	keys := make([]string, 0, len(activeSet))
	for k := range activeSet {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	for _, k := range keys {
		fmt.Println(k)
	}
}
