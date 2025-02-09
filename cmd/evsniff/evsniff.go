package main

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

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
	forceColor = getopt.BoolLong("color", 'c', "force colors")
	noColor    = getopt.BoolLong("no-color", 0, "disable colors")
	verbose    = getopt.BoolLong("verbose", 'v', "make verbose (show detailed device info)")
	infoOnly   = getopt.BoolLong("info", 'i', "print device info and quit")
	useInotify = getopt.BoolLong("inotify", 'n', "use inotify to watch for new devices")
)

func main() {
	common.RunAndExit(realMain)
}

func realMain() int {
	getopt.Parse()

	useColors := false
	if *forceColor {
		useColors = true
	} else if *noColor {
		useColors = false
	} else {
		useColors = isatty.IsTerminal(os.Stdout.Fd())
	}

	devs := listDevices()
	if *infoOnly {
		return 0
	}
	wg := sync.WaitGroup{}
	for _, d := range devs {
		wg.Add(1)
		d2 := d
		go func() {
			defer wg.Done()
			testDevice(d2, useColors)
		}()
	}

	// Watch for new devices.
	if *useInotify {
		w := must.Must2(inotify.NewWatcher())
		must.Must2(w.AddWatch(devInput, inotify.IN_CREATE|inotify.IN_DELETE))

		// Start listening for events.
		go func() {
			for ev := range w.Event {
				fmt.Printf("[inotify] %v\n", ev)
				if (ev.Mask & inotify.IN_CREATE) != 0 {
					time.Sleep(500)
					// Added
					path := devInput + "/" + ev.Name
					d, err := evdev.Open(path)
					if err != nil {
						fmt.Printf("Error opening device %s: %s\n", path, err)
					} else {
						dumpDevice(d, "    ")
					}
				}
			}
		}()
	}

	wg.Wait()
	return 0
}

func listDevices() []*evdev.InputDevice {
	ret := make([]*evdev.InputDevice, 0)
	devices, err := evdev.ListDevicePaths()
	common.Checkf(err, "Cannot list device paths")

	sortDevices(devices)

	for _, idev := range devices {
		// d, err := evdev.Open(idev.Path)
		// if err != nil {
		// 	fmt.Printf("Error opening device %s: %s", d.Path(), err)
		// 	continue
		// }

		d := must.Must2(evdev.Open(idev.Path))

		dumpDevice(d, "    ")

		// id, err := i.InputID()
		// if err != nil {
		// 	fmt.Printf("Error obtaining device info %s: %s", d.Path, err)
		// 	continue
		// }
		// fmt.Printf("%-20s [v%04X p%04X]:\t%s\n", d.Path, id.Vendor, id.Product, d.Name)
		// if *verbose {
		// 	dumpDevice(i, "    ")
		// }
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

var _ colorizer = (*basicColorizer)(nil)

var mu = &sync.Mutex{}

var lastPath string

func testDevice(d *evdev.InputDevice, color bool) {
	var col colorizer
	if color {
		col = &basicColorizer{}
	} else {
		col = &noColorizer{}
	}
	var lastTime time.Time = time.Time{}

	id, err := d.InputID()
	common.CheckPanic(err, "Unable to get device info")
	name, err := d.Name()
	common.CheckPanic(err, "Unable to get device name")
	path := d.Path()

	for {
		if *verbose {
			fmt.Printf("Waiting for input (%s)...\n", name)
		}
		e, err := d.ReadOne()
		if err != nil {
			fmt.Printf("Error reading from device: %v\n", err)
			return
		}

		ts := fmt.Sprintf("[%s%d.%06d%s]", col.time(), e.Time.Sec, e.Time.Usec, col.reset())

		now := time.Now()
		mu.Lock()
		if now.Sub(lastTime) > time.Second*3 || lastPath != path {
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
			fmt.Printf("%s %s-------------- %s ------------%s\n", ts, c, e.CodeName(), col.reset())
		default:
			c := col.otherEvent()
			switch e.Type {
			case evdev.EV_KEY:
				c = col.keyEvent()
			case evdev.EV_REL:
				c = col.relEvent()
			case evdev.EV_ABS:
				c = col.absEvent()
			}

			fmt.Printf("%s %s%s%s\n", ts, c, e.String(), col.reset())
		}
		mu.Unlock()
	}
}
