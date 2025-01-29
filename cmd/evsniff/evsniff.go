package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/holoplot/go-evdev"
	"github.com/mattn/go-isatty"
	"github.com/omakoto/go-common/src/common"
	"github.com/pborman/getopt/v2"
)

var (
	forceColor = getopt.BoolLong("color", 'c', "force colors")
	noColor    = getopt.BoolLong("no-color", 0, "disable colors")
	verbose    = getopt.BoolLong("verbose", 'v', "make verbose")
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
	wg := sync.WaitGroup{}
	for _, d := range devs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testDevice(d, useColors)
		}()
	}
	wg.Wait()
	return 0
}

func listDevices() []*evdev.InputDevice {
	ret := make([]*evdev.InputDevice, 0)
	paths, err := evdev.ListDevicePaths()
	common.Checkf(err, "Cannot list device paths")
	for _, p := range paths {
		d, err := evdev.Open(p.Path)
		if err != nil {
			fmt.Printf("Error opening device %s: %s", p.Path, err)
			continue
		}
		id, err := d.InputID()
		if err != nil {
			fmt.Printf("Error obtaining device info %s: %s", p.Path, err)
			continue
		}
		fmt.Printf("%-20s [v%04x p%04x]:\t%s\n", p.Path, id.Vendor, id.Product, p.Name)
		if *verbose {
			dumpDevice(d, "    ")
		}
		ret = append(ret, d)
	}
	return ret
}

func dumpDevice(d *evdev.InputDevice, prefix string) {
	for _, t := range d.CapableTypes() {
		fmt.Printf("%sEvent type %d (%s)\n", prefix, t, evdev.TypeName(t))

		state, err := d.State(t)
		if err == nil {
			for code, value := range state {
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

		for code, absInfo := range absInfos {
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
	return "\x1b[38;5;15m"
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

func testDevice(d *evdev.InputDevice, color bool) {
	var col colorizer
	if color {
		col = &basicColorizer{}
	} else {
		col = &noColorizer{}
	}
	// write("%sOK%s -- xx\n", col.failure(), col.reset())
	for {
		e, err := d.ReadOne()
		if err != nil {
			fmt.Printf("Error reading from device: %v\n", err)
			return
		}

		ts := fmt.Sprintf("%s%d.%06d%s ", col.time(), e.Time.Sec, e.Time.Usec, col.reset())

		mu.Lock()
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
