# evsniff-go

A colorized, multi-device Linux input and MIDI event monitor — like `evtest` and `aseqdump`, but watching all devices at once.

`evsniff` reads from `/dev/input/event*` (evdev) and `/dev/snd/midi*` (ALSA raw MIDI) devices, printing incoming events in real time with color-coded output by event type (keys, relative/absolute axes, note events, control changes, pitch bends, etc.). It detects hot-plugged devices automatically and can filter which devices to watch.

## Install

```bash
go install -v github.com/omakoto/evsniff-go/cmd/evsniff@latest
```

You typically need `sudo` or membership in the `input` group to read from `/dev/input/` devices, and membership in the `audio` group to read from `/dev/snd/` MIDI devices.

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

# Monitor a specific input or MIDI device by path
sudo evsniff /dev/input/event3
sudo evsniff /dev/snd/midiC1D0

# Monitor MIDI controllers matching "donner" in their name
sudo evsniff donner

# List all devices with their capabilities, then quit
sudo evsniff -iv

# Simple mode: one line per key-press, useful for scripting or keybinding tools
sudo evsniff -s keyboard

# Grab a device exclusively (prevents other processes from reading it)
sudo evsniff -g keyboard

# Show only key events, suppressing noisy relative/absolute axis events
sudo evsniff -RA

# Print all currently pressed/active keys across selected devices, then quit
sudo evsniff -a keyboard
```

### Simple mode output

`--simple` (`-s`) prints one compact line per key-press or MIDI event, ideal for scripts.

For evdev keyboards:

```
# s=0 c=0 a=0 m=0 type=0x01:EV_KEY code=0x1E:KEY_A value=1 vendor=046D product=C31C path=/dev/input/event3 # Logitech USB Keyboard
```

Fields: `s`=Shift, `c`=Ctrl, `a`=Alt, `m`=Meta/Super (1 = pressed, 0 = not pressed).

For MIDI controllers:

```
# channel=1 type=NoteOn note=60 velocity=100 path=/dev/snd/midiC1D0 # DONNER DMK25Pro
# channel=1 type=ControlChange controller=1 value=64 path=/dev/snd/midiC1D0 # DONNER DMK25Pro
```

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
| `--active-keys` | `-a` | Find all active keys from the selected devices, print their names sorted and unique, and quit |
| `--key-regex` | `-r` | Regular expression to filter active key names when `-a` is specified (case-insensitive). If provided, exits with `0` if any key matches, and `1` otherwise |

## FILTER syntax

Each positional argument selects which devices (`/dev/input/event*` or `/dev/snd/midi*`) to monitor:

- **Regex** — matched against the device name (case-insensitive): `logitech`, `keyboard`, `donner`
- **Path** — selects a specific device: `/dev/input/event3`, `/dev/snd/midiC1D0`
- **Negation** — prefix `!` to exclude: `!mouse`, `!/dev/snd/midiC0D0`

Multiple filters are combined: positive filters use OR logic (any match is included), negative filters (`!`) exclude regardless of other matches. With no filters, all devices are monitored.
