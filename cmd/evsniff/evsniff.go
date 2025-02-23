package main

import (
	"fmt"
	"os"
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
	forceColor    = getopt.BoolLong("color", 'c', "force colors")
	noColor       = getopt.BoolLong("no-color", 0, "disable colors")
	verbose       = getopt.BoolLong("verbose", 'v', "make verbose (show detailed device info)")
	infoOnly      = getopt.BoolLong("info", 'i', "print device info and quit")
	showSynReport = getopt.BoolLong("show-syn", 'V', "show SYN_REPORTs (default hidden)")
	showScan      = getopt.BoolLong("show-scan", 'S', "show MSC_SCAN (default hidden)")
	noRel         = getopt.BoolLong("no-rel", 'R', "do not show EV_REL")
	noAbs         = getopt.BoolLong("no-abs", 'A', "do not show EV_ABS")
)

func main() {
	common.RunAndExit(realMain)
}

func parseArgs(args []string) (col colorizer, sel evutil.Selector) {
	getopt.SetParameters("[FILTER...]")
	getopt.SetUsage(func() {
		getopt.PrintUsage(os.Stderr)
		fmt.Fprintf(os.Stderr, "\n"+
			"  FILTER  Either /dev/input/... or a regex for the device name. Prepend with a ! to negate it.\n"+
			"          Example:\n"+
			"            'logitech' selects all logitec devices\n"+
			"            '!mouse' selects devices that don't have the word \"mouse\" in it\n"+
			"\n")
	})
	getopt.CommandLine.Parse(args)

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

		if strings.HasPrefix(arg, "/dev/input/") {
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

	devs := listDevices(sel)
	if *infoOnly {
		return 0
	}
	if len(devs) == 0 {
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

	// Watch for new devices.
	waitForNewDevices(col, sel, func(idev *evdev.InputDevice) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testDevice(idev, col)
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

var _ colorizer = (*basicColorizer)(nil)

var mu = &sync.Mutex{}

var lastPath string
var lastTime time.Time = time.Time{}

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

	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
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
	return nil
}

func waitForNewDevices(col colorizer, sel evutil.Selector, starter func(idev *evdev.InputDevice)) {
	w := must.Must2(inotify.NewWatcher())
	must.Must2(w.AddWatch(devInput, inotify.IN_CREATE|inotify.IN_DELETE))

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

				path := devInput + "/" + ev.Name
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
				if !strings.HasPrefix(ev.Name, "event") {
					continue
				}
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
					// ls := exec.Command("ls", "-ls", path)
					// ls.Stdout = os.Stdout
					// ls.Stderr = os.Stdout
					// ls.Start()

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
				pending = retries
				if !pending.IsEmpty() {
					go updaterInvoker()
				}
			}
		}
	}()
}
