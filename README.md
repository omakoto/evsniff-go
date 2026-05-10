# evsniff-go

A colorized, multi-device Linux input event monitor — like `evtest`, but watching all devices at once.

`evsniff` reads from `/dev/input/event*` and prints incoming input events in real time with color-coded output by event type (key, relative, absolute, sync). It detects hot-plugged devices automatically and can filter which devices to watch.

## Install

```bash
go install -v github.com/omakoto/evsniff-go/cmd/evsniff@latest
```

You typically need `sudo` or membership in the `input` group to read from `/dev/input/` devices.

## Usage

```
evsniff [OPTIONS] [FILTER...]
```

### Examples

```bash
# Monitor all input devices
sudo evsniff

# Monitor devices matching a name pattern (case-insensitive regex)
sudo evsniff keyboard
sudo evsniff logitech

# Exclude devices matching a pattern (prefix with !)
sudo evsniff logitech '!mouse'

# Monitor a specific device by path
sudo evsniff /dev/input/event3

# List all devices with their capabilities, then quit
sudo evsniff -iv

# Simple mode: one line per key-press, useful for scripting or keybinding tools
sudo evsniff -s keyboard

# Grab a device exclusively (prevents other processes from reading it)
sudo evsniff -g keyboard

# Show only key events, suppressing noisy relative/absolute axis events
sudo evsniff -RA
```

### Simple mode output

`--simple` (`-s`) prints one compact line per key-press event, including modifier key state:

```
# s=0 c=0 a=0 m=0 type=0x01:EV_KEY code=0x1E:KEY_A value=1 vendor=046D product=C31C path=/dev/input/event3 # Logitech USB Keyboard
```

Fields: `s`=Shift, `c`=Ctrl, `a`=Alt, `m`=Meta/Super (1 = pressed, 0 = not pressed).

## Options

| Flag | Short | Description |
|------|-------|-------------|
| `--color` | `-c` | Force colored output even when stdout is not a terminal |
| `--no-color` | | Disable colored output |
| `--verbose` | `-v` | Show detailed device capabilities and properties |
| `--info` | `-i` | Print device info and quit (no event monitoring) |
| `--show-syn` | `-V` | Show `SYN_REPORT` events (hidden by default) |
| `--show-scan` | `-S` | Show `MSC_SCAN` events (hidden by default) |
| `--no-rel` | `-R` | Suppress `EV_REL` (relative axis) events |
| `--no-abs` | `-A` | Suppress `EV_ABS` (absolute axis) events |
| `--show-hz` | `-H` | Show event rate in Hz |
| `--grab` | `-g` | Grab device for exclusive access |
| `--simple` | `-s` | Key-press events only, with modifier key state (for scripting) |

## FILTER syntax

Each positional argument selects which `/dev/input/event*` devices to monitor:

- **Regex** — matched against the device name (case-insensitive): `logitech`, `keyboard`, `touch`
- **Path** — selects a specific device: `/dev/input/event3`
- **Negation** — prefix `!` to exclude: `!mouse`, `!/dev/input/event0`

Multiple filters are combined: positive filters use OR logic (any match is included), negative filters (`!`) exclude regardless of other matches. With no filters, all devices are monitored.
