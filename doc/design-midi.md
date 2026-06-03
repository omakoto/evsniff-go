# Design: CGO-Free MIDI Support in evsniff

This document details the design and implementation of real-time MIDI monitoring in `evsniff` under the `cmd/evsniff` and `evutil` packages. 

---

## 1. Objectives & Constraints

1. **Zero-CGO & Pure Go**: The implementation must interface directly with Linux ALSA RawMIDI device files without wrapping `libasound` or requiring CGO headers.
2. **Unified Filtering**: MIDI controllers and evdev devices must share the same CLI matching patterns (`evutil.Selector`), regex filtering, and exclusion matching.
3. **Hotplug Support**: Plugging in new MIDI controllers must trigger automatic discovery and attachment via `inotify`.
4. **Scripting support**: Output must support standard CLI modifiers like simple output mode (`-s`) for easy piping.

---

## 2. Structural Abstraction

Previously, `evutil.Selector` matched directly against `*evdev.InputDevice`. To support MIDI devices, we decoupled selection by introducing a generic `Device` interface in [evutil/selector.go](file:///home/omakoto/src/evsniff-go/evutil/selector.go):

```go
type Device interface {
	Path() string
	Name() (string, error)
}
```

Both `*evdev.InputDevice` (from `go-evdev`) and our new `*MidiDevice` implement this interface.

```
       +-----------------------+
       |   evutil.Selector     |
       +-----------+-----------+
                   |
             Matches(Device)
                   |
         +---------+---------+
         |                   |
+--------v--------+ +--------v--------+
|   EvdevDevice   | |   MidiDevice    |
| (evdev.InputDev)| |   (midi.go)     |
+-----------------+ +-----------------+
```

---

## 3. Metadata Extraction & Discovery

To maintain a pure Go environment, `evsniff` queries Linux device metadata from the filesystem structure:

### A. Device Enumeration
ALSA raw MIDI devices are represented in the kernel as `/dev/snd/midiC<card>D<device>` (e.g., `/dev/snd/midiC1D0`). We glob `/dev/snd/midiC*D*` to discover active interfaces.

### B. Device Names (`procfs`)
Instead of issuing control ioctls, `evsniff` parses `/proc/asound/cards` on startup. 
We map card indexes to device names by matching lines with the format:
```
 1 [DMK25Pro       ]: USB-Audio - DONNER DMK25Pro
```
This is parsed using a zero-regex string scanner in [cmd/evsniff/midi.go](file:///home/omakoto/src/evsniff-go/cmd/evsniff/midi.go).

### C. USB Vendor & Product IDs (`sysfs`)
For USB controllers, we walk up parent links to grab hardware IDs:
1. `sysPath` starts at `/sys/class/sound/midiC<card>D<device>/device`.
2. We follow the symlink to its absolute path in `/sys/devices/`.
3. We walk up the directory tree looking for `idVendor` and `idProduct` files.
4. When found, we read their hexadecimal contents. If the device is not USB (e.g., a PCI card), we fall back to `0000`.

---

## 4. Pure Go MIDI Decoder (State Machine)

To consume raw streams from `/dev/snd/midiC*D*`, we implement a state machine that tracks **running status** (omitting the status byte for subsequent identical event types).

### State Transition Diagram

```
                 +-----------------------+
                 |       Read Byte       |
                 +-----------+-----------+
                             |
             Is status byte? (>= 0x80)
             /                       \
           YES                        NO
           /                           \
  Update Running Status         Is running status valid?
  Clear buffer                  /                      \
  Lookup message length        YES                      NO
  /                 \          /                          \
Var-Len (SysEx)   Fixed-Len  Append byte to buffer      Discard byte
                     |       Buffer full?
                     |       /          \
                    YES    YES           NO
                     |      |             \
            Dispatch Event  Dispatch   Wait for next byte
                            & Clear buf
```

### Supported Messages

- **Note On / Note Off**: Decodes the channel, velocity, and note number. The note number is translated into octave representation (e.g., `60` $\rightarrow$ `C4`). For Channel 10 (reserved for percussion in General MIDI), standard drum instrument names are also resolved and displayed alongside the note (e.g., `C4 / Hi Bongo`).
- **Control Change**: Decodes the controller index, resolves its standard name (e.g. Modulation Wheel, Sustain Pedal) if known, and displays the controller value.
- **Pitch Bend**: Aggregates the 7-bit LSB and MSB data bytes into a single value range.
- **Program Change & Pressure**: Decodes program index or channel pressure level.
- **System Exclusive (SysEx)**: Captures variable-length byte streams starting with `0xF0` and ending with `0xF7`.
- **System Real-Time**: Interleaved 1-byte events (e.g., `0xF8` Clock) processed immediately without breaking the running status stream. Only displayed under `--verbose`.

---

## 5. Hotplugging & inotify Watcher

The hotplug watcher in [cmd/evsniff/evsniff.go](file:///home/omakoto/src/evsniff-go/cmd/evsniff/evsniff.go) registers watches on both `/dev/input` and `/dev/snd` using a single `inotify.Watcher`:

1. When a `CREATE` event fires, we check the prefix:
   - Starts with `event` $\rightarrow$ Queue as evdev path.
   - Starts with `midiC` $\rightarrow$ Queue as MIDI path.
2. The event is debounced into a `pending` set to allow the driver to fully create the device nodes and configure permissions.
3. During processing, MIDI paths are initialized, checked against selectors, and attached to a parser loop, matching the behaviour of regular input events.
