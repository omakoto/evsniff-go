package main

import (
	"fmt"

	evdev "github.com/holoplot/go-evdev"
	"github.com/omakoto/go-common/src/common"
	"github.com/pborman/getopt/v2"
)

var (
	color   = getopt.BoolLong("color", 'c', "force colors")
	verbose = getopt.BoolLong("verbose", 'v', "make verbose")
)

func main() {
	common.RunAndExit(realMain)
}

func realMain() int {
	getopt.Parse()

	_ = listDevices()
	return 1
}

func listDevices() []evdev.InputPath {
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
		if *verbose || true {
			dumpDevice(d, "    ")
		}
	}
	return nil
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
