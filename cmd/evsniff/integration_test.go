package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"syscall"
	"testing"

	"github.com/holoplot/go-evdev"
	"github.com/omakoto/evsniff-go/evutil"
)

type mockDirEntry struct {
	name string
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return false }
func (m *mockDirEntry) Type() os.FileMode          { return 0 }
func (m *mockDirEntry) Info() (os.FileInfo, error) { return nil, nil }

type mockDeviceSpec struct {
	path          string
	name          string
	supportedKeys []int
	activeKeys    []int
}

func setupMockDevices(devices []mockDeviceSpec) func() {
	origDevInputPath := devInputPath
	origReadDirFn := readDirFn
	origOpenRawDeviceFn := openRawDeviceFn
	origCloseRawDeviceFn := closeRawDeviceFn
	origGetRawDeviceNameFn := getRawDeviceNameFn
	origGetSupportedKeysFn := getSupportedKeysFn
	origGetActiveKeysRawFn := getActiveKeysRawFn

	devInputPath = "/dev/input"

	readDirFn = func(path string) ([]os.DirEntry, error) {
		if path != "/dev/input" {
			return nil, os.ErrNotExist
		}
		var entries []os.DirEntry
		for _, d := range devices {
			parts := strings.Split(d.path, "/")
			name := parts[len(parts)-1]
			entries = append(entries, &mockDirEntry{name: name})
		}
		return entries, nil
	}

	pathToFd := make(map[string]uintptr)
	fdToDevice := make(map[uintptr]mockDeviceSpec)
	var nextFd uintptr = 100

	for _, d := range devices {
		pathToFd[d.path] = nextFd
		fdToDevice[nextFd] = d
		nextFd++
	}

	openRawDeviceFn = func(path string) (uintptr, error) {
		fd, ok := pathToFd[path]
		if !ok {
			return 0, syscall.ENOENT
		}
		return fd, nil
	}

	closeRawDeviceFn = func(fd uintptr) {}

	getRawDeviceNameFn = func(fd uintptr) (string, error) {
		d, ok := fdToDevice[fd]
		if !ok {
			return "", syscall.EBADF
		}
		return d.name, nil
	}

	getSupportedKeysFn = func(fd uintptr) ([]byte, error) {
		d, ok := fdToDevice[fd]
		if !ok {
			return nil, syscall.EBADF
		}
		bits := make([]byte, 767)
		for _, key := range d.supportedKeys {
			bits[key/8] |= 1 << (key % 8)
		}
		return bits, nil
	}

	getActiveKeysRawFn = func(fd uintptr) ([]byte, error) {
		d, ok := fdToDevice[fd]
		if !ok {
			return nil, syscall.EBADF
		}
		bits := make([]byte, 767)
		for _, key := range d.activeKeys {
			bits[key/8] |= 1 << (key % 8)
		}
		return bits, nil
	}

	return func() {
		devInputPath = origDevInputPath
		readDirFn = origReadDirFn
		openRawDeviceFn = origOpenRawDeviceFn
		closeRawDeviceFn = origCloseRawDeviceFn
		getRawDeviceNameFn = origGetRawDeviceNameFn
		getSupportedKeysFn = origGetSupportedKeysFn
		getActiveKeysRawFn = origGetActiveKeysRawFn
	}
}

type exitError int

func captureOutput(f func()) (stdoutStr, stderrStr string, panicVal any) {
	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()

	os.Stdout = wOut
	os.Stderr = wErr

	outChan := make(chan string)
	errChan := make(chan string)

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rOut)
		outChan <- buf.String()
	}()

	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, rErr)
		errChan <- buf.String()
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				panicVal = r
			}
		}()
		f()
	}()

	wOut.Close()
	wErr.Close()

	stdoutStr = <-outChan
	stderrStr = <-errChan
	return
}

func TestIntegration(t *testing.T) {
	// Setup custom exit mechanism
	origOsExit := osExit
	defer func() { osExit = origOsExit }()

	osExit = func(code int) {
		panic(exitError(code))
	}

	tests := []struct {
		name           string
		args           []string
		mockDevices    []mockDeviceSpec
		mockInfoOnly   bool
		expectedExit   int
		expectedStdout string // regex pattern
		expectedStderr string // regex pattern
	}{
		{
			name:           "TC-01 Help command",
			args:           []string{"evsniff", "-h"},
			expectedExit:   0,
			expectedStderr: `(?s)Monitor Linux input and MIDI devices.*Examples:.*`,
		},
		{
			name:           "TC-02 Key-regex without active-keys",
			args:           []string{"evsniff", "-r", "KEY_A"},
			expectedExit:   2,
			expectedStderr: `(?s)Error: --key-regex can only be used with -a / --active-keys.*`,
		},
		{
			name:           "TC-03 Invalid regex argument",
			args:           []string{"evsniff", "-a", "-r", "invalid["},
			expectedExit:   2,
			expectedStderr: `(?s)Error: invalid regular expression "invalid\[".*`,
		},
		{
			name: "TC-04 Active-keys list matches expected active keys",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 48}, // KEY_A, KEY_B
					activeKeys:    []int{30},     // KEY_A
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^KEY_A\n#KEY_A\n$`,
		},
		{
			name: "TC-05 Key-regex match case-insensitive",
			args: []string{"evsniff", "-a", "-r", "key_a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 48},
					activeKeys:    []int{30},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^KEY_A\n#KEY_A\n$`,
		},
		{
			name: "TC-06 Key-regex no match",
			args: []string{"evsniff", "-a", "-r", "KEY_B"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 48},
					activeKeys:    []int{30}, // KEY_A active, KEY_B inactive
				},
			},
			expectedExit:   1,
			expectedStdout: `(?s)^$`,
		},
		{
			name: "TC-07 Device filtering",
			args: []string{"evsniff", "-a", "keyboard"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Mouse",
					supportedKeys: []int{30},
					activeKeys:    []int{30},
				},
				{
					path:          "/dev/input/event1",
					name:          "Mock Keyboard",
					supportedKeys: []int{48},
					activeKeys:    []int{48},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^KEY_B\n#KEY_B\n$`, // Only Mock Keyboard key matched
		},
		{
			name:           "TC-08 Info option",
			args:           []string{"evsniff", "-i"},
			mockInfoOnly:   true,
			expectedExit:   0,
			expectedStdout: `(?s)/dev/input/event0\s+\[v0001 p0002\]:\tMock Keyboard.*`,
		},
		{
			name: "TC-09 Negation filter",
			args: []string{"evsniff", "-a", "mock", "!mouse"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Mouse",
					supportedKeys: []int{30},
					activeKeys:    []int{30},
				},
				{
					path:          "/dev/input/event1",
					name:          "Mock Keyboard",
					supportedKeys: []int{48},
					activeKeys:    []int{48},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^KEY_B\n#KEY_B\n$`,
		},
		{
			name: "TC-10 Path selector",
			args: []string{"evsniff", "-a", "/dev/input/event1"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard 1",
					supportedKeys: []int{30},
					activeKeys:    []int{30},
				},
				{
					path:          "/dev/input/event1",
					name:          "Mock Keyboard 2",
					supportedKeys: []int{48},
					activeKeys:    []int{48},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^KEY_B\n#KEY_B\n$`,
		},
		{
			name:           "TC-11 Verbose capabilities and Info option (-iv)",
			args:           []string{"evsniff", "-i", "-v"},
			mockInfoOnly:   true,
			expectedExit:   0,
			expectedStdout: `(?s)/dev/input/event0\s+\[v0001 p0002\]:\tMock Keyboard\s+Event type 1 \(EV_KEY\)\s+Event code 30 \(KEY_A\) state 0.*`,
		},
		{
			name:           "TC-12 Flag validation (simple mode, grab, axis suppression)",
			args:           []string{"evsniff", "-s", "-g", "-R", "-A"},
			expectedExit:   1,
			expectedStdout: `(?s)FLAGS OK\nNo devices selected\..*`,
		},
		{
			name: "TC-13 Active-keys list with Shift and Alt modifiers",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 42, 56}, // KEY_A, KEY_LEFTSHIFT, KEY_LEFTALT
					activeKeys:    []int{30, 42, 56},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_LEFTALT\nKEY_LEFTSHIFT\n#a-s-KEY_A\n`,
		},
		{
			name: "TC-14 Regex match on summary line (positive)",
			args: []string{"evsniff", "-a", "-r", "a-s-KEY_A"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 42, 56}, // KEY_A, KEY_LEFTSHIFT, KEY_LEFTALT
					activeKeys:    []int{30, 42, 56},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^#a-s-KEY_A\n$`,
		},
		{
			name: "TC-15 Regex match on summary line (negative)",
			args: []string{"evsniff", "-a", "-r", "c-KEY_A"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 42, 56}, // KEY_A, KEY_LEFTSHIFT, KEY_LEFTALT
					activeKeys:    []int{30, 42, 56},
				},
			},
			expectedExit:   1,
			expectedStdout: `(?s)^$`,
		},
		{
			name: "TC-16 Active-keys list with all modifiers (Shift, Ctrl, Alt, Win)",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 29, 42, 56, 125}, // KEY_A, KEY_LEFTCTRL, KEY_LEFTSHIFT, KEY_LEFTALT, KEY_LEFTMETA
					activeKeys:    []int{30, 29, 42, 56, 125},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_LEFTALT\nKEY_LEFTCTRL\nKEY_LEFTMETA\nKEY_LEFTSHIFT\n#a-c-s-w-KEY_A\n`,
		},
		{
			name: "TC-17 Active-keys list with Shift and Ctrl modifiers",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 29, 42}, // KEY_A, KEY_LEFTCTRL, KEY_LEFTSHIFT
					activeKeys:    []int{30, 29, 42},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_LEFTCTRL\nKEY_LEFTSHIFT\n#c-s-KEY_A\n`,
		},
		{
			name: "TC-18 Active-keys list with Ctrl and Alt modifiers",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 29, 56}, // KEY_A, KEY_LEFTCTRL, KEY_LEFTALT
					activeKeys:    []int{30, 29, 56},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_LEFTALT\nKEY_LEFTCTRL\n#a-c-KEY_A\n`,
		},
		{
			name: "TC-19 Active-keys list with Alt and Win modifiers",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 56, 125}, // KEY_A, KEY_LEFTALT, KEY_LEFTMETA
					activeKeys:    []int{30, 56, 125},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_LEFTALT\nKEY_LEFTMETA\n#a-w-KEY_A\n`,
		},
		{
			name: "TC-20 Active-keys list with Right modifiers (Shift, Ctrl, Alt, Win)",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{30, 54, 97, 100, 126}, // KEY_A, KEY_RIGHTSHIFT, KEY_RIGHTCTRL, KEY_RIGHTALT, KEY_RIGHTMETA
					activeKeys:    []int{30, 54, 97, 100, 126},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)KEY_A\nKEY_RIGHTALT\nKEY_RIGHTCTRL\nKEY_RIGHTMETA\nKEY_RIGHTSHIFT\n#a-c-s-w-KEY_A\n`,
		},
		{
			name: "TC-21 Active-keys slash name splitting (e.g. BTN_MOUSE/BTN_LEFT)",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Mouse",
					supportedKeys: []int{272}, // BTN_MOUSE/BTN_LEFT
					activeKeys:    []int{272},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)BTN_LEFT\nBTN_MOUSE\n#BTN_LEFT\n#BTN_MOUSE\n`,
		},
		{
			name: "TC-22 Regex match on first split name (positive)",
			args: []string{"evsniff", "-a", "-r", "BTN_LEFT"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Mouse",
					supportedKeys: []int{272},
					activeKeys:    []int{272},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^BTN_LEFT\n#BTN_LEFT\n$`,
		},
		{
			name: "TC-23 Regex match on second split name (positive)",
			args: []string{"evsniff", "-a", "-r", "BTN_MOUSE"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Mouse",
					supportedKeys: []int{272},
					activeKeys:    []int{272},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^BTN_MOUSE\n#BTN_MOUSE\n$`,
		},
		{
			name: "TC-24 Active-keys list with BTN_MISC/BTN_0 and Shift + Win modifiers",
			args: []string{"evsniff", "-a"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{256, 42, 125}, // BTN_MISC/BTN_0, KEY_LEFTSHIFT, KEY_LEFTMETA
					activeKeys:    []int{256, 42, 125},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)BTN_0\nBTN_MISC\nKEY_LEFTMETA\nKEY_LEFTSHIFT\n#s-w-BTN_0\n#s-w-BTN_MISC\n`,
		},
		{
			name: "TC-25 Regex match on split name summary with Shift + Win modifiers",
			args: []string{"evsniff", "-a", "-r", "s-w-BTN_0"},
			mockDevices: []mockDeviceSpec{
				{
					path:          "/dev/input/event0",
					name:          "Mock Keyboard",
					supportedKeys: []int{256, 42, 125}, // BTN_MISC/BTN_0, KEY_LEFTSHIFT, KEY_LEFTMETA
					activeKeys:    []int{256, 42, 125},
				},
			},
			expectedExit:   0,
			expectedStdout: `(?s)^#s-w-BTN_0\n$`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := setupMockDevices(tc.mockDevices)
			defer cleanup()

			// Mock device listing/dumping for infoOnly testing
			origListDevicesFn := listDevicesFn
			origListMidiDevicesFn := listMidiDevicesFn
			defer func() {
				listDevicesFn = origListDevicesFn
				listMidiDevicesFn = origListMidiDevicesFn
			}()

			if tc.mockInfoOnly {
				if tc.name == "TC-11 Verbose capabilities and Info option (-iv)" {
					listDevicesFn = func(sel evutil.Selector) []*evdev.InputDevice {
						fmt.Println("/dev/input/event0    [v0001 p0002]:\tMock Keyboard")
						fmt.Println("    Event type 1 (EV_KEY)")
						fmt.Println("      Event code 30 (KEY_A) state 0")
						return nil
					}
				} else {
					listDevicesFn = func(sel evutil.Selector) []*evdev.InputDevice {
						fmt.Println("/dev/input/event0    [v0001 p0002]:\tMock Keyboard")
						return nil
					}
				}
				listMidiDevicesFn = func(sel evutil.Selector) []*MidiDevice {
					return nil
				}
			}

			if tc.name == "TC-12 Flag validation (simple mode, grab, axis suppression)" {
				listDevicesFn = func(sel evutil.Selector) []*evdev.InputDevice {
					if *simple && *grab && *noRel && *noAbs {
						fmt.Println("FLAGS OK")
					} else {
						fmt.Printf("FLAGS ERROR: simple=%t grab=%t noRel=%t noAbs=%t\n", *simple, *grab, *noRel, *noAbs)
					}
					return nil
				}
				listMidiDevicesFn = func(sel evutil.Selector) []*MidiDevice {
					return nil
				}
			}

			var exitCode int
			stdoutStr, stderrStr, panicVal := captureOutput(func() {
				// Prepare args
				os.Args = tc.args
				exitCode = realMain()
			})

			if panicVal != nil {
				if err, ok := panicVal.(exitError); ok {
					exitCode = int(err)
				} else {
					t.Fatalf("unexpected panic: %v", panicVal)
				}
			}

			if exitCode != tc.expectedExit {
				t.Errorf("expected exit code %d, got %d. Stderr: %q, Stdout: %q", tc.expectedExit, exitCode, stderrStr, stdoutStr)
			}

			if tc.expectedStdout != "" {
				re := regexp.MustCompile(tc.expectedStdout)
				if !re.MatchString(stdoutStr) {
					t.Errorf("stdout %q did not match pattern %q", stdoutStr, tc.expectedStdout)
				}
			}

			if tc.expectedStderr != "" {
				re := regexp.MustCompile(tc.expectedStderr)
				if !re.MatchString(stderrStr) {
					t.Errorf("stderr %q did not match pattern %q", stderrStr, tc.expectedStderr)
				}
			}
		})
	}
}
