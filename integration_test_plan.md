# Integration Test Plan for evsniff

This document outlines the design and implementation plan for establishing a robust, easy-to-maintain integration test suite for `evsniff`.

## 1. Goal

Create a regression test suite that:
*   Tests CLI flags and option combinations (e.g., `-a`, `-r`, `-i`, device name filters).
*   Runs in-process using standard `go test`.
*   Requires no physical hardware, no `root`/`sudo` privileges, and no actual `/dev/input` access.
*   Executes extremely fast (< 100ms) by using an abstraction layer for OS/device interactions.

## 2. Refactoring for Testability

To make `evsniff` testable without spawning subprocesses or modifying the user's environment, we will abstract the console output and the device listing/querying mechanisms.

### A. Output Redirection

We will update the entry point to accept standard output/error streams instead of writing directly to `os.Stdout` and `os.Stderr`:

```go
func run(args []string, stdout, stderr io.Writer) int
```

All functions printing to stdout/stderr (e.g., `fmt.Printf`, `fmt.Println`) will be refactored to write to these injected streams.

### B. Device & OS Abstraction Layer

We will isolate the functions that query `/dev/input` and perform low-level `ioctl` calls. In `dumpkeys.go` and `evsniff.go`, we will wrap OS-level calls in mockable variables:

```go
var (
    readDirFunc           = os.ReadDir
    openRawDeviceFunc     = syscall.Open
    closeRawDeviceFunc    = syscall.Close
    getRawDeviceNameFunc  = getRawDeviceName
    getSupportedKeysFunc  = getSupportedKeys
    getActiveKeysRawFunc  = getActiveKeysRaw
)
```

For standard event monitoring tests, we can abstract/wrap the opening of `evdev.InputDevice` and listing of device paths:

```go
type deviceListProvider interface {
    listDevicePaths() ([]evdev.InputPath, error)
    openDevice(path string) (*evdev.InputDevice, error)
}
```

During testing, we will swap these implementations with mock structures.

## 3. Table-Driven Test Architecture

We will implement `cmd/evsniff/integration_test.go` using a table-driven structure.

### A. Mock Device definition

Each test case can define a list of mock devices present in the mock system:

```go
type mockDeviceSpec struct {
    path          string
    name          string
    vendor        uint16
    product       uint16
    supportedKeys []byte
    activeKeys    []byte
    events        []evdev.InputEvent // for event-loop tests
}
```

### B. Test Case Structure

```go
type testCase struct {
    name           string
    args           []string
    mockDevices    []mockDeviceSpec
    expectedExit   int
    expectedStdout string // substring or regex matching stdout
    expectedStderr string // substring or regex matching stderr
}
```

## 4. Test Scenarios to Cover

The suite will verify the following scenarios:

| Scenario ID | Arguments | Mock Setup | Expected Outcome |
|---|---|---|---|
| **TC-01** | `[]string{"-h"}` | N/A | Prints usage, exits with `0` |
| **TC-02** | `[]string{"-r", "KEY_A"}` | N/A | Stderr error (regex requires `-a`), exits with `2` |
| **TC-03** | `[]string{"-a", "-r", "invalid["}` | N/A | Stderr invalid regex error, exits with `2` |
| **TC-04** | `[]string{"-a"}` | 1 Keyboard with `KEY_A` active | Prints `KEY_A` to stdout, exits with `0` |
| **TC-05** | `[]string{"-a", "-r", "key_a"}` | 1 Keyboard with `KEY_A` active | Prints `KEY_A` to stdout (case-insensitive), exits with `0` |
| **TC-06** | `[]string{"-a", "-r", "KEY_B"}` | 1 Keyboard with `KEY_A` active | Outputs nothing, exits with `1` (no match) |
| **TC-07** | `[]string{"-a", "mouse"}` | 1 Mouse (active keys), 1 Keyboard (no active keys) | Filters active keys only from matching "mouse" device, exits `0` |
| **TC-08** | `[]string{"-i"}` | 1 Keyboard, 1 Mouse | Prints device vendor/product/name info, exits with `0` |

## 5. Next Steps

1.  **Refactor**: Introduce stream injection (`stdout`, `stderr`) and getopt environment reset.
2.  **OS/Device abstraction**: Wrap syscall and evdev paths in a test-friendly layer.
3.  **Implement Integration Tests**: Write `integration_test.go` and add the test cases.
4.  **Validate**: Run `go test ./...` and verify test coverage.
