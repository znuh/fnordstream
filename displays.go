package main

import (
	"fmt"
	"strings"
	"runtime"

	"github.com/go-cmd/cmd"
)

type Display struct {
	Name     string      `json:"name"`
	Geo      Geometry    `json:"geo"`
	Use      bool        `json:"use"`
	Host_id  int         `json:"host_id"`
}

func pshell_read() []Display {
	ps     := "Add-Type -AssemblyName System.Windows.Forms\n[System.Windows.Forms.Screen]::AllScreens\n"
	ctx    := cmd.NewCmd("powershell")
	status := <-ctx.StartWithStdin(strings.NewReader(ps))

	var displays  []Display
	disp := Display{Use:true}

    for _, line := range status.Stdout {
        var k, v string
        n, _ := fmt.Sscanf(line,"%s : %s", &k,&v)
        if n == 2 {
			//fmt.Println("#"+k+"#","#"+v+"#")
			if k == "DeviceName" { 
				disp.Name = v
			} else if k == "WorkingArea" {
				geo := &disp.Geo
				fmt.Sscanf(v,"{X=%d,Y=%d,Width=%d,Height=%d}",&geo.X,&geo.Y,&geo.W,&geo.H)
			}
		} else {
			if disp.Name != "" && disp.Geo.W > 0 && disp.Geo.H > 0 {
				displays = append(displays, disp)
			}
			disp.Geo.W, disp.Geo.H = 0, 0
			disp.Name = ""
		}
    }
    return displays
}

func xrandr_read() []Display {
	ctx    := cmd.NewCmd("xrandr", "--listactivemonitors")
	status := <-ctx.Start()

	var displays  []Display

    for _, line := range status.Stdout {
		disp := Display{Use:true}
		geo := &disp.Geo
		name1 := ""
		idx, phys_w, phys_h := 0, 0, 0
        //fmt.Println(line)
        n, _ := fmt.Sscanf(line,"%d: %s %d/%dx%d/%d+%d+%d %s", &idx, &name1, &geo.W, &phys_w, &geo.H, &phys_h, &geo.X, &geo.Y, &disp.Name)
        //fmt.Println(n,res)
        if n == 9 {
			displays = append(displays, disp)
		}
    }
    return displays
}

func displays_detect() []Display {
	res := []Display{}
	switch runtime.GOOS {
	case "windows":
		res = pshell_read()
	//case "darwin":
	case "linux":
		res = xrandr_read()
	default:
		fmt.Println("no display detection for OS:",runtime.GOOS,"!")
	}
	return res
}
