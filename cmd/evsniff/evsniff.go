package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/omakoto/evsniff-go/evutil"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/holoplot/go-evdev"
	"github.com/jhenstridge/go-inotify"
	"github.com/maruel/natural"
	"github.com/mattn/go-isatty"
	"github.com/omakoto/go-common/src/common"
	"github.com/omakoto/go-common/src/must"
	"github.com/omakoto/go-common/src/utils"
	"github.com/pborman/getopt/v2"
)

const (
	devInput = "/dev/input"
)

var (
	help          = getopt.BoolLong("help", 'h', "show this help message")
	forceColor    = getopt.BoolLong("color", 'c', "force colored output even when stdout is not a terminal")
	noColor       = getopt.BoolLong("no-color", 0, "disable colored output")
	verbose       = getopt.BoolLong("verbose", 'v', "show detailed device capabilities and properties")
	infoOnly      = getopt.BoolLong("info", 'i', "print device info and quit (no event monitoring)")
	showSynReport = getopt.BoolLong("show-syn", 'V', "show SYN_REPORT events (hidden by default)")
	showScan      = getopt.BoolLong("show-scan", 'S', "show MSC_SCAN events (hidden by default)")
	noRel         = getopt.BoolLong("no-rel", 'R', "suppress EV_REL (relative axis) events")
	noAbs         = getopt.BoolLong("no-abs", 'A', "suppress EV_ABS (absolute axis) events")
	showHz        = getopt.BoolLong("show-hz", 'H', "show event rate in Hz")
	grab          = getopt.BoolLong("grab", 'g', "grab device for exclusive access")
	simple        = getopt.BoolLong("simple", 's', "key-press events only, with modifier key state (for scripting)")
	activeKeys    = getopt.BoolLong("active-keys", 'a', "find all active keys from the selected devices, print their names, and exit")
	keyRegex      = getopt.StringLong("key-regex", 'r', "", "regular expression to match active keys when -a is passed")
)

func main() {
	common.RunAndExit(realMain)
}

func parseArgs(args []string) (col colorizer, sel evutil.Selector) {
	getopt.SetParameters("[FILTER...]")
	getopt.SetUsage(func() {
		getopt.PrintUsage(os.Stderr)
		fmt.Fprintf(os.Stderr, "\n"+
			"Monitor Linux input and MIDI devices in real time. Watches all matching /dev/input/event*\n"+
			"and /dev/snd/midi* devices simultaneously and prints events with color-coded output by event type.\n"+
			"New devices plugged in after startup are picked up automatically.\n"+
			"\n"+
			"  FILTER  A regex matched against the device name (case-insensitive), or a full\n"+
			"          /dev/input/... or /dev/snd/... path. Prepend ! to exclude matching devices.\n"+
			"          Without any FILTER, all devices are monitored.\n"+
			"\n"+
			"  Examples:\n"+
			"    evsniff                         monitor all input and MIDI devices\n"+
			"    evsniff keyboard                monitor devices with \"keyboard\" in their name\n"+
			"    evsniff logitech '!mouse'        Logitech devices, excluding mice\n"+
			"    evsniff /dev/input/event3        monitor a specific input device by path\n"+
			"    evsniff /dev/snd/midiC1D0        monitor a specific MIDI device by path\n"+
			"    evsniff -iv                      list devices and quit\n"+
			"    evsniff -s keyboard              simple mode: one line per key-press (for scripting)\n"+
			"    evsniff -s donner                simple mode: one line per MIDI event (for scripting)\n"+
			"    evsniff -g keyboard              grab keyboard for exclusive access\n"+
			"    evsniff -a keyboard              print active keys on keyboard devices and quit\n"+
			"    evsniff -a -r KEY_A              check if KEY_A is pressed and exit 0 if so\n"+
			"\n"+
			"https://github.com/omakoto/evsniff-go\n"+
			"\n")
	})
	getopt.CommandLine.Parse(args)

	if *help {
		getopt.Usage()
		os.Exit(0)
	}

	// Build color filter
	useColors := false
	if *forceColor {
		useColors = true
	} else if *noColor {
		useColors = false
	} else {
		useColors = isatty.IsTerminal(os.Stdout.Fd())
	}
	if useColors {
		col = &basicColorizer{}
	} else {
		col = &noColorizer{}
	}

	// Build device selector
	or := evutil.NewCombinedSelector()

	for _, arg := range getopt.CommandLine.Args() {
		var s evutil.Selector
		negate := false

		if strings.HasPrefix(arg, "!") {
			negate = true
			arg = arg[1:]
		}

		if strings.HasPrefix(arg, "/dev/input/") || strings.HasPrefix(arg, "/dev/snd/") {
			s = evutil.NewPathSelector(arg)
		} else {
			s = evutil.NewReSelector(arg)
		}

		if negate {
			s = evutil.NewNegativeSelector(s)
		}
		or.Add(s)
	}
	sel = or

	return
}

func realMain() int {
	col, sel := parseArgs(os.Args)

	if *keyRegex != "" && !*activeKeys {
		fmt.Fprintln(os.Stderr, "Error: --key-regex can only be used with -a / --active-keys")
		return 2
	}

	if *activeKeys {
		var re *regexp.Regexp
		if *keyRegex != "" {
			var err error
			re, err = regexp.Compile("(?i)" + *keyRegex)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: invalid regular expression %q: %v\n", *keyRegex, err)
				return 2
			}
		}
		if printActiveKeysFast(sel, re) {
			return 0
		}
		return 1
	}

	devs := listDevices(sel)
	midiDevs := listMidiDevices(sel)
	if *infoOnly {
		return 0
	}
	if len(devs) == 0 && len(midiDevs) == 0 {
		fmt.Println("No devices selected.")
		return 1
	}

	wg := sync.WaitGroup{}
	for _, d := range devs {
		wg.Add(1)
		idev := d
		go func() {
			defer wg.Done()
			testDevice(idev, col)
		}()
	}
	for _, d := range midiDevs {
		wg.Add(1)
		idev := d
		go func() {
			defer wg.Done()
			testMidiDevice(idev, col)
		}()
	}

	// Watch for new devices.
	waitForNewDevices(col, sel, func(idev *evdev.InputDevice) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testDevice(idev, col)
		}()
	}, func(idev *MidiDevice) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testMidiDevice(idev, col)
		}()
	})

	wg.Wait()
	return 0
}

func listDevices(sel evutil.Selector) []*evdev.InputDevice {
	ret := make([]*evdev.InputDevice, 0)
	devices, err := evdev.ListDevicePaths()
	common.Checkf(err, "Cannot list device paths")

	sortDevices(devices)

	for _, idev := range devices {
		d := must.Must2(evdev.Open(idev.Path))

		if !evutil.Matches(sel, d) {
			d.Close()
			continue
		}

		dumpDevice(d, "    ")
		if *grab {
			err = d.Grab()
			if err != nil {
				fmt.Printf("Error grabbing device %s: %s\n", d.Path(), err)
			}
		}

		ret = append(ret, d)
	}
	return ret
}

func sortDevices(devices []evdev.InputPath) {
	slices.SortFunc(devices, func(a, b evdev.InputPath) int {
		return utils.LessToCmp(natural.Less)(a.Path, b.Path)
	})
}



func dumpDevice(d *evdev.InputDevice, prefix string) {
	id, err := d.InputID()
	if err != nil {
		fmt.Printf("Error obtaining device info %s: %s", d.Path(), err)
		return
	}

	fmt.Printf("%-20s [v%04X p%04X]:\t%s\n", d.Path(), id.Vendor, id.Product, must.Must2(d.Name()))
	if !*verbose {
		return
	}

	types := d.CapableTypes()
	slices.Sort(types)
	for _, t := range types {
		fmt.Printf("%sEvent type %d (%s)\n", prefix, t, evdev.TypeName(t))

		state, err := d.State(t)
		if err == nil {
			for code, value := range utils.SortedMap(state) {
				// value := state[code]
				fmt.Printf("%s  Event code %d (%s) state %v\n", prefix, code, evdev.CodeName(t, code), value)
			}
		}

		if t != evdev.EV_ABS {
			continue
		}
		absInfos, err := d.AbsInfos()
		if err != nil {
			continue
		}
		for code, absInfo := range utils.SortedMap(absInfos) {
			fmt.Printf("%s  Event code %d (%s)\n", prefix, code, evdev.CodeName(t, code))
			fmt.Printf("%s    Value: %d\n", prefix, absInfo.Value)
			fmt.Printf("%s    Min: %d\n", prefix, absInfo.Minimum)
			fmt.Printf("%s    Max: %d\n", prefix, absInfo.Maximum)

			if absInfo.Fuzz != 0 {
				fmt.Printf("%s    Fuzz: %d\n", prefix, absInfo.Fuzz)
			}
			if absInfo.Flat != 0 {
				fmt.Printf("%s    Flat: %d\n", prefix, absInfo.Flat)
			}
			if absInfo.Resolution != 0 {
				fmt.Printf("%s    Resolution: %d\n", prefix, absInfo.Resolution)
			}
		}
	}

	props := d.Properties()
	if len(props) > 0 {
		fmt.Printf("%sProperties:\n", prefix)

		for _, p := range props {
			fmt.Printf("%s  Property type %d (%s)\n", prefix, p, evdev.PropName(p))
		}
	}
}

type colorizer interface {
	reset() string
	time() string
	deviceLine() string
	deviceId() string
	deviceName() string
	synReport() string
	keyEvent() string
	relEvent() string
	absEvent() string
	otherEvent() string
	failure() string
	inotify() string
	inotifyPath() string
	inotifyDelete() string
	inotifyCreate() string

	midiNoteOn() string
	midiNoteOff() string
	midiControlChange() string
	midiPitchBend() string
	midiOther() string
}

type noColorizer struct {
}

// time implements colorizer.
func (n *noColorizer) time() string {
	return ""
}

var _ colorizer = (*noColorizer)(nil)

// absEvent implements colorizer.
func (n *noColorizer) absEvent() string {
	return ""
}

// deviceId implements colorizer.
func (n *noColorizer) deviceId() string {
	return ""
}

// deviceLine implements colorizer.
func (n *noColorizer) deviceLine() string {
	return ""
}

// deviceName implements colorizer.
func (n *noColorizer) deviceName() string {
	return ""
}

// failure implements colorizer.
func (n *noColorizer) failure() string {
	return ""
}

// keyEvent implements colorizer.
func (n *noColorizer) keyEvent() string {
	return ""
}

// otherEvent implements colorizer.
func (n *noColorizer) otherEvent() string {
	return ""
}

// relEvent implements colorizer.
func (n *noColorizer) relEvent() string {
	return ""
}

// reset implements colorizer.
func (n *noColorizer) reset() string {
	return ""
}

// synReport implements colorizer.
func (n *noColorizer) synReport() string {
	return ""
}

// inotify implements colorizer.
func (n *noColorizer) inotify() string {
	return ""
}

// inotifyCreate implements colorizer.
func (n *noColorizer) inotifyCreate() string {
	return ""
}

// inotifyDelete implements colorizer.
func (n *noColorizer) inotifyDelete() string {
	return ""
}

// inotifyPath implements colorizer.
func (n *noColorizer) inotifyPath() string {
	return ""
}

func (n *noColorizer) midiNoteOn() string {
	return ""
}

func (n *noColorizer) midiNoteOff() string {
	return ""
}

func (n *noColorizer) midiControlChange() string {
	return ""
}

func (n *noColorizer) midiPitchBend() string {
	return ""
}

func (n *noColorizer) midiOther() string {
	return ""
}

type basicColorizer struct {
}

var _ colorizer = (*basicColorizer)(nil)

// time implements colorizer.
func (n *basicColorizer) time() string {
	return "\x1b[38;5;2m"
}

// absEvent implements colorizer.
func (n *basicColorizer) absEvent() string {
	return "\x1b[95;1m"
}

// deviceId implements colorizer.
func (n *basicColorizer) deviceId() string {
	return "\x1b[93m"
}

// deviceLine implements colorizer.
func (n *basicColorizer) deviceLine() string {
	return "\x1b[36m"
}

// deviceName implements colorizer.
func (n *basicColorizer) deviceName() string {
	return "\x1b[92m"
}

// failure implements colorizer.
func (n *basicColorizer) failure() string {
	return "\x1b[38;5;9m"
}

// keyEvent implements colorizer.
func (n *basicColorizer) keyEvent() string {
	return "\x1b[96;1m"
}

// otherEvent implements colorizer.
func (n *basicColorizer) otherEvent() string {
	return "\x1b[36m"
}

// relEvent implements colorizer.
func (n *basicColorizer) relEvent() string {
	return "\x1b[93;1m"
}

// reset implements colorizer.
func (n *basicColorizer) reset() string {
	return "\x1b[0m"
}

// synReport implements colorizer.
func (n *basicColorizer) synReport() string {
	return "\x1b[90m"
}

// inotify implements colorizer.
func (n *basicColorizer) inotify() string {
	return "\x1b[38;5;11m"
}

// inotifyCreate implements colorizer.
func (n *basicColorizer) inotifyCreate() string {
	return "\x1b[38;5;10m"
}

// inotifyDelete implements colorizer.
func (n *basicColorizer) inotifyDelete() string {
	return "\x1b[38;5;9m"
}

// inotifyPath implements colorizer.
func (n *basicColorizer) inotifyPath() string {
	return "\x1b[38;5;15m"
}

func (n *basicColorizer) midiNoteOn() string {
	return "\x1b[92;1m"
}

func (n *basicColorizer) midiNoteOff() string {
	return "\x1b[90m"
}

func (n *basicColorizer) midiControlChange() string {
	return "\x1b[95;1m"
}

func (n *basicColorizer) midiPitchBend() string {
	return "\x1b[93;1m"
}

func (n *basicColorizer) midiOther() string {
	return "\x1b[36m"
}

var _ colorizer = (*basicColorizer)(nil)

var mu = &sync.Mutex{}

var lastPath string
var lastTime time.Time = time.Time{}
var lastEvTime float64

func testDevice(d *evdev.InputDevice, col colorizer) {
	id, err := d.InputID()
	common.CheckPanic(err, "Unable to get device info")
	name, err := d.Name()
	common.CheckPanic(err, "Unable to get device name")
	path := d.Path()

	if *verbose {
		fmt.Printf("Waiting for input (%s)...\n", name)
	}
	for {
		err := handleOneEvent(d, col, path, id, name)
		if err != nil {
			fmt.Printf("Error reading from device: %v\n", err)
			break
		}
	}
}

// path -> keyCode -> value
var keyStates map[string]map[evdev.EvCode]int32 = make(map[string]map[evdev.EvCode]int32)

func getKeyState(path string, key1, key2 evdev.EvCode) int32 {
	var keys = keyStates[path]
	var v1 = keys[key1]
	var v2 = keys[key2]
	if v1+v2 > 0 {
		return 1
	}
	return 0
}

func handleOneEvent(d *evdev.InputDevice, col colorizer, path string, id evdev.InputID, name string) error {
	e, err := d.ReadOne()
	if err != nil {
		return err
	}
	if !*showSynReport && e.Type == evdev.EV_SYN && e.Code == evdev.SYN_REPORT {
		return nil
	}
	if !*showScan && e.Type == evdev.EV_MSC && e.Code == evdev.MSC_SCAN {
		return nil
	}
	if *noRel && e.Type == evdev.EV_REL {
		return nil
	}
	if *noAbs && e.Type == evdev.EV_ABS {
		return nil
	}

	ts := fmt.Sprintf("[%s%d.%06d%s]", col.time(), e.Time.Sec, e.Time.Usec, col.reset())

	hzStr := ""
	evTime := (float64)(e.Time.Sec*1_000_000+e.Time.Usec) / 1_000_000.0
	if *showHz && lastEvTime != 0 && evTime != lastEvTime {
		delta := evTime - lastEvTime
		if delta < 1 {
			hz := (int64)(1 / delta)
			hzStr = fmt.Sprintf(" (%dhz)", hz)
		}
	}
	lastEvTime = evTime

	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if !*simple && (now.Sub(lastTime) > time.Second*3 || lastPath != path) {
		// show device name
		fmt.Printf("%s# From device [%sv%04X p%04X%s]: %s%s%s (%s)%s\n",
			col.deviceLine(),
			col.deviceId(),
			id.Vendor,
			id.Product,
			col.deviceLine(),
			col.deviceName(),
			name,
			col.deviceLine(),
			path,
			col.reset(),
		)
	}
	lastTime = now
	lastPath = path

	switch e.Type {
	case evdev.EV_SYN:
		c := col.otherEvent()
		switch e.Code {
		case evdev.SYN_REPORT:
			c = col.synReport()
		case evdev.SYN_DROPPED:
			c = col.failure()
		}
		if !*simple {
			fmt.Printf("%s %s-------------- %s ------------%s\n", ts, c, e.CodeName(), col.reset())
		}
	default:
		c := col.otherEvent()
		switch e.Type {
		case evdev.EV_KEY:
			c = col.keyEvent()

			// Remember the pressed keys
			if keyStates[path] == nil {
				keyStates[path] = make(map[evdev.EvCode]int32)
			}
			keyStates[path][e.Code] = e.Value
		case evdev.EV_REL:
			c = col.relEvent()
		case evdev.EV_ABS:
			c = col.absEvent()
		}

		if !*simple {
			fmt.Printf("%s %s%s%s%s\n", ts, c, e.String(), col.reset(), hzStr)
		} else if e.Type == evdev.EV_KEY && e.Value > 0 {
			fmt.Printf("# s=%d c=%d a=%d m=%d type=0x%02X:%s code=0x%02X:%s value=%d vendor=%04X product=%04X path=%s # %s\n",
				getKeyState(path, evdev.KEY_LEFTSHIFT, evdev.KEY_RIGHTSHIFT),
				getKeyState(path, evdev.KEY_LEFTCTRL, evdev.KEY_RIGHTCTRL),
				getKeyState(path, evdev.KEY_LEFTALT, evdev.KEY_RIGHTALT),
				getKeyState(path, evdev.KEY_LEFTMETA, evdev.KEY_RIGHTMETA),
				e.Type, e.TypeName(),
				e.Code, e.CodeName(),
				e.Value,
				id.Vendor, id.Product,
				path,
				name,
			)
		}
	}
	return nil
}

func waitForNewDevices(col colorizer, sel evutil.Selector, starter func(idev *evdev.InputDevice), midiStarter func(idev *MidiDevice)) {
	w := must.Must2(inotify.NewWatcher())
	must.Must2(w.AddWatch(devInput, inotify.IN_CREATE|inotify.IN_DELETE))
	_, _ = w.AddWatch("/dev/snd", inotify.IN_CREATE|inotify.IN_DELETE)

	// Start listening for events.
	go func() {
		pending := mapset.NewSet[string]()
		// for ev := range w.Event {
		updater := make(chan bool, 16)

		updatePending := false

		updaterInvoker := func() {
			if updatePending {
				return
			}
			updatePending = true
			timer := time.NewTimer(1000 * time.Millisecond)

			<-timer.C

			updatePending = false
			updater <- true
		}

		for {
			select {
			case ev := <-w.Event:
				create := false
				evCol := ""
				evName := ""

				if (ev.Mask & inotify.IN_DELETE) != 0 {
					evCol = col.inotifyDelete()
					evName = "DELETE"
				} else if (ev.Mask & inotify.IN_CREATE) != 0 {
					evCol = col.inotifyCreate()
					evName = "CREATE"
					create = true
				} else {
					break // Shouldn't happen
				}

				var path string
				isMidi := false
				if strings.HasPrefix(ev.Name, "event") {
					path = devInput + "/" + ev.Name
				} else if strings.HasPrefix(ev.Name, "midiC") {
					path = "/dev/snd/" + ev.Name
					isMidi = true
				} else {
					continue
				}

				fmt.Printf("[%sinotify%s] %s%s%s: %s%s%s\n",
					col.inotify(),
					col.reset(),
					evCol,
					evName,
					col.reset(),
					col.inotifyPath(),
					path,
					col.reset(),
				)

				_ = isMidi
				if create {
					pending.Add(path)

					go updaterInvoker()
				} else {
					pending.Remove(path)
				}
			case <-updater:
				if pending.IsEmpty() {
					break
				}
				if *verbose {
					fmt.Printf("[updater] %v\n", pending)
				}
				retries := mapset.NewSet[string]()
				for path := range pending.Iter() {
					if *verbose {
						fmt.Printf("%s\n", path)
					}

					if strings.HasPrefix(path, "/dev/snd/") {
						card, device, err := parseMidiPath(path)
						if err != nil {
							continue
						}
						f, err := os.Open(path)
						if err != nil {
							if os.IsPermission(err) {
								fmt.Fprintf(os.Stderr, "%s not ready to open yet...\n", path)
								retries.Add(path)
								continue
							}
							fmt.Fprintf(os.Stderr, "Failed to open %s: '%s'\n", path, err.Error())
							continue
						}
						cardNames := getCardNames()
						name := cardNames[card]
						if name == "" {
							name = fmt.Sprintf("MIDI Card %d Device %d", card, device)
						}
						vendor, product := getMidiUsbIds(card, device)
						idev := &MidiDevice{
							path:    path,
							name:    name,
							card:    card,
							device:  device,
							vendor:  vendor,
							product: product,
							file:    f,
						}
						if !evutil.Matches(sel, idev) {
							idev.file.Close()
							continue
						}
						dumpMidiDevice(idev, "    ")
						midiStarter(idev)
					} else {
						idev, err := evdev.Open(path)

						if err != nil {
							if os.IsPermission(err) {
								fmt.Fprintf(os.Stderr, "%s not ready to open yet...\n", path)
								retries.Add(path)
								continue
							}
							fmt.Fprintf(os.Stderr, "Failed to open %s: '%s'\n", path, err.Error())
							continue
						}
						if !evutil.Matches(sel, idev) {
							idev.Close()
							continue
						}
						dumpDevice(idev, "    ")
						starter(idev)
					}
				}
				pending = retries
				if !pending.IsEmpty() {
					go updaterInvoker()
				}
			}
		}
	}()
}
