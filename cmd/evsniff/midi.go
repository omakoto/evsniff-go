package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omakoto/evsniff-go/evutil"
)

type MidiDevice struct {
	path    string
	name    string
	card    int
	device  int
	vendor  uint16
	product uint16
	file    *os.File
}

var _ evutil.Device = (*MidiDevice)(nil)

func (m *MidiDevice) Path() string {
	return m.path
}

func (m *MidiDevice) Name() (string, error) {
	return m.name, nil
}

func listMidiDevices(sel evutil.Selector) []*MidiDevice {
	ret := make([]*MidiDevice, 0)

	files, err := filepath.Glob("/dev/snd/midiC*D*")
	if err != nil {
		return ret
	}

	cardNames := getCardNames()

	for _, path := range files {
		card, device, err := parseMidiPath(path)
		if err != nil {
			continue
		}

		name := cardNames[card]
		if name == "" {
			name = fmt.Sprintf("MIDI Card %d Device %d", card, device)
		}

		vendor, product := getMidiUsbIds(card, device)

		d := &MidiDevice{
			path:    path,
			name:    name,
			card:    card,
			device:  device,
			vendor:  vendor,
			product: product,
		}

		if !evutil.Matches(sel, d) {
			continue
		}

		f, err := os.Open(path)
		if err != nil {
			fmt.Printf("Error opening MIDI device %s: %s\n", path, err)
			continue
		}
		d.file = f

		dumpMidiDevice(d, "    ")

		ret = append(ret, d)
	}

	return ret
}

func parseMidiPath(path string) (card, device int, err error) {
	base := filepath.Base(path)
	_, err = fmt.Sscanf(base, "midiC%dD%d", &card, &device)
	return
}

func getCardNames() map[int]string {
	names := make(map[int]string)
	content, err := os.ReadFile("/proc/asound/cards")
	if err != nil {
		return names
	}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idxBracketOpen := strings.Index(line, "[")
		idxBracketClose := strings.Index(line, "]")
		idxColon := strings.Index(line, ":")
		if idxBracketOpen > 0 && idxBracketClose > idxBracketOpen && idxColon > idxBracketClose {
			var cardNum int
			_, err := fmt.Sscanf(line[:idxBracketOpen], "%d", &cardNum)
			if err == nil {
				cardName := strings.TrimSpace(line[idxColon+1:])
				names[cardNum] = cardName
			}
		}
	}
	return names
}

func getMidiUsbIds(card, device int) (uint16, uint16) {
	sysPath := fmt.Sprintf("/sys/class/sound/midiC%dD%d/device", card, device)
	absPath, err := filepath.EvalSymlinks(sysPath)
	if err != nil {
		return 0, 0
	}

	curr := absPath
	for {
		vendorFile := filepath.Join(curr, "idVendor")
		productFile := filepath.Join(curr, "idProduct")

		_, errV := os.Stat(vendorFile)
		_, errP := os.Stat(productFile)
		if errV == nil && errP == nil {
			vBytes, _ := os.ReadFile(vendorFile)
			pBytes, _ := os.ReadFile(productFile)

			var v, p uint32
			fmt.Sscanf(strings.TrimSpace(string(vBytes)), "%x", &v)
			fmt.Sscanf(strings.TrimSpace(string(pBytes)), "%x", &p)
			return uint16(v), uint16(p)
		}

		parent := filepath.Dir(curr)
		if parent == curr || parent == "/" || parent == "." {
			break
		}
		curr = parent
	}
	return 0, 0
}

func dumpMidiDevice(d *MidiDevice, prefix string) {
	fmt.Printf("%-20s [v%04X p%04X]:\t%s\n", d.path, d.vendor, d.product, d.name)
}

type MidiEvent struct {
	Timestamp time.Time
	Status    byte
	Channel   byte
	Data1     byte
	Data2     byte
	SysEx     []byte
	Type      string
}

type MidiParser struct {
	runningStatus byte
	expectedLen   int
	buffer        []byte
	onEvent       func(MidiEvent)
}

func NewMidiParser(onEvent func(MidiEvent)) *MidiParser {
	return &MidiParser{
		buffer:  make([]byte, 0, 3),
		onEvent: onEvent,
	}
}

func (p *MidiParser) ParseByte(b byte, ts time.Time) {
	if b >= 0xF8 {
		p.onEvent(MidiEvent{
			Timestamp: ts,
			Status:    b,
			Type:      "RealTime",
		})
		return
	}

	if b >= 0x80 {
		p.runningStatus = b
		p.buffer = p.buffer[:0]

		if b >= 0xF0 {
			p.runningStatus = 0

			if b == 0xF0 {
				p.buffer = append(p.buffer, b)
				p.expectedLen = -1
			} else {
				p.expectedLen = getSystemCommonLen(b)
				if p.expectedLen == 0 {
					p.onEvent(MidiEvent{
						Timestamp: ts,
						Status:    b,
						Type:      getSystemCommonType(b),
					})
				}
			}
		} else {
			p.expectedLen = getChannelMessageLen(b)
		}
		return
	}

	if p.expectedLen == -1 {
		p.buffer = append(p.buffer, b)
		if b == 0xF7 {
			sysex := make([]byte, len(p.buffer))
			copy(sysex, p.buffer)
			p.onEvent(MidiEvent{
				Timestamp: ts,
				Status:    0xF0,
				SysEx:     sysex,
				Type:      "SysEx",
			})
			p.buffer = p.buffer[:0]
			p.expectedLen = 0
		}
		return
	}

	status := p.runningStatus
	if status == 0 {
		return
	}

	p.buffer = append(p.buffer, b)
	if len(p.buffer) == p.expectedLen {
		var ev MidiEvent
		ev.Timestamp = ts
		ev.Status = status

		if status >= 0xF0 {
			ev.Type = getSystemCommonType(status)
			if p.expectedLen >= 1 {
				ev.Data1 = p.buffer[0]
			}
			if p.expectedLen >= 2 {
				ev.Data2 = p.buffer[1]
			}
		} else {
			ev.Channel = (status & 0x0F) + 1
			ev.Type = getChannelMessageType(status)
			if p.expectedLen >= 1 {
				ev.Data1 = p.buffer[0]
			}
			if p.expectedLen >= 2 {
				ev.Data2 = p.buffer[1]
			}
		}

		p.onEvent(ev)
		p.buffer = p.buffer[:0]
	}
}

func getChannelMessageLen(status byte) int {
	switch status & 0xF0 {
	case 0x80, 0x90, 0xA0, 0xB0, 0xE0:
		return 2
	case 0x10, 0xC0, 0xD0:
		return 1
	}
	return 0
}

func getChannelMessageType(status byte) string {
	switch status & 0xF0 {
	case 0x80:
		return "NoteOff"
	case 0x90:
		return "NoteOn"
	case 0xA0:
		return "PolyPressure"
	case 0xB0:
		return "ControlChange"
	case 0xC0:
		return "ProgramChange"
	case 0xD0:
		return "ChannelPressure"
	case 0xE0:
		return "PitchBend"
	}
	return "Unknown"
}

func getSystemCommonLen(status byte) int {
	switch status {
	case 0xF1, 0xF3:
		return 1
	case 0xF2:
		return 2
	}
	return 0
}

func getSystemCommonType(status byte) string {
	switch status {
	case 0xF1:
		return "MTCQuarterFrame"
	case 0xF2:
		return "SongPositionPointer"
	case 0xF3:
		return "SongSelect"
	case 0xF6:
		return "TuneRequest"
	case 0xF7:
		return "SysExEnd"
	}
	return "SystemCommon"
}

var noteNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

func noteName(note byte) string {
	octave := int(note)/12 - 1
	name := noteNames[note%12]
	return fmt.Sprintf("%s%d", name, octave)
}

func testMidiDevice(d *MidiDevice, col colorizer) {
	path := d.Path()
	name := d.name

	if *verbose {
		fmt.Printf("Waiting for MIDI input (%s)...\n", name)
	}

	parser := NewMidiParser(func(ev MidiEvent) {
		printMidiEvent(ev, d, col)
	})

	buf := make([]byte, 256)
	for {
		n, err := d.file.Read(buf)
		if err != nil {
			fmt.Printf("Error reading from MIDI device %s: %v\n", path, err)
			break
		}
		ts := time.Now()
		for i := 0; i < n; i++ {
			parser.ParseByte(buf[i], ts)
		}
	}
}

func printMidiEvent(ev MidiEvent, d *MidiDevice, col colorizer) {
	if *simple {
		switch ev.Type {
		case "NoteOn":
			fmt.Printf("# channel=%d type=NoteOn note=%d velocity=%d path=%s # %s\n",
				ev.Channel, ev.Data1, ev.Data2, d.path, d.name)
		case "NoteOff":
			fmt.Printf("# channel=%d type=NoteOff note=%d velocity=%d path=%s # %s\n",
				ev.Channel, ev.Data1, ev.Data2, d.path, d.name)
		case "ControlChange":
			fmt.Printf("# channel=%d type=ControlChange controller=%d value=%d path=%s # %s\n",
				ev.Channel, ev.Data1, ev.Data2, d.path, d.name)
		case "PitchBend":
			val := int(ev.Data1) | (int(ev.Data2) << 7)
			fmt.Printf("# channel=%d type=PitchBend value=%d path=%s # %s\n",
				ev.Channel, val, d.path, d.name)
		}
		return
	}

	ts := fmt.Sprintf("[%s%d.%06d%s]", col.time(), ev.Timestamp.Unix(), ev.Timestamp.Nanosecond()/1000, col.reset())

	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if now.Sub(lastTime) > time.Second*3 || lastPath != d.path {
		fmt.Printf("%s# From device [%sv%04X p%04X%s]: %s%s%s (%s)%s\n",
			col.deviceLine(),
			col.deviceId(),
			d.vendor,
			d.product,
			col.deviceLine(),
			col.deviceName(),
			d.name,
			col.deviceLine(),
			d.path,
			col.reset(),
		)
	}
	lastTime = now
	lastPath = d.path

	switch ev.Type {
	case "NoteOn":
		color := col.midiNoteOn()
		fmt.Printf("%s %sMIDI: Note On (Ch %d) - Note %d (%s), Velocity %d%s\n",
			ts, color, ev.Channel, ev.Data1, noteName(ev.Data1), ev.Data2, col.reset())
	case "NoteOff":
		color := col.midiNoteOff()
		fmt.Printf("%s %sMIDI: Note Off (Ch %d) - Note %d (%s), Velocity %d%s\n",
			ts, color, ev.Channel, ev.Data1, noteName(ev.Data1), ev.Data2, col.reset())
	case "ControlChange":
		color := col.midiControlChange()
		fmt.Printf("%s %sMIDI: Control Change (Ch %d) - Controller %d, Value %d%s\n",
			ts, color, ev.Channel, ev.Data1, ev.Data2, col.reset())
	case "PitchBend":
		color := col.midiPitchBend()
		val := int(ev.Data1) | (int(ev.Data2) << 7)
		fmt.Printf("%s %sMIDI: Pitch Bend (Ch %d) - Value %d (0x%04X)%s\n",
			ts, color, ev.Channel, val, val, col.reset())
	case "ProgramChange":
		color := col.midiOther()
		fmt.Printf("%s %sMIDI: Program Change (Ch %d) - Program %d%s\n",
			ts, color, ev.Channel, ev.Data1, col.reset())
	case "ChannelPressure":
		color := col.midiOther()
		fmt.Printf("%s %sMIDI: Channel Pressure (Ch %d) - Pressure %d%s\n",
			ts, color, ev.Channel, ev.Data1, col.reset())
	case "PolyPressure":
		color := col.midiOther()
		fmt.Printf("%s %sMIDI: Polyphonic Pressure (Ch %d) - Note %d (%s), Pressure %d%s\n",
			ts, color, ev.Channel, ev.Data1, noteName(ev.Data1), ev.Data2, col.reset())
	case "SysEx":
		color := col.midiOther()
		fmt.Printf("%s %sMIDI: SysEx - Length %d bytes, Bytes: % x%s\n",
			ts, color, len(ev.SysEx), ev.SysEx, col.reset())
	case "RealTime":
		if *verbose {
			color := col.midiOther()
			fmt.Printf("%s %sMIDI: Real-Time Event (0x%02X)%s\n",
				ts, color, ev.Status, col.reset())
		}
	default:
		color := col.midiOther()
		fmt.Printf("%s %sMIDI: Event Type %s (Status 0x%02X), Data: 0x%02X 0x%02X%s\n",
			ts, color, ev.Type, ev.Status, ev.Data1, ev.Data2, col.reset())
	}
}
