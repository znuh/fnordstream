package main

import (
	"fmt"
	"time"
)

type Geometry struct {
	X   int   `json:"x" mapstructure:"x"`
	Y   int   `json:"y" mapstructure:"y"`
	W   int   `json:"w" mapstructure:"w"`
	H   int   `json:"h" mapstructure:"h"`
}

type Viewport struct {
	Id  int   `json:"id" mapstructure:"id"`
	X   int   `json:"x" mapstructure:"x"`
	Y   int   `json:"y" mapstructure:"y"`
	W   int   `json:"w" mapstructure:"w"`
	H   int   `json:"h" mapstructure:"h"`
	Display_id   int  `json:"display_id,omitempty" mapstructure:"display_id"`  // display index (for client)
	Host_id      int  `json:"host_id,omitempty" mapstructure:"host_id"`        // host_id from display (from/for client)
}

func (vp *Viewport) String() string {
        return fmt.Sprintf("%dx%d+%d+%d",vp.W,vp.H,vp.X,vp.Y)
}

type PlayerConfig struct {
	location              string

	ipc_pipe              string
	mpv_args            []string

	use_streamlink        bool
	streamlink_args     []string

	restart_user_quit     bool
	restart_error_delay   time.Duration
}

type PlayerStatus struct {
	Status              string    `json:"status"`
	Exit_code           *int      `json:"exit_code,omitempty"`
	Error               string    `json:"error,omitempty"`
}

type PlayerEvent struct {
	Event               string         `json:"event" mapstructure:"event"`
	Name                string         `json:"name" mapstructure:"name"`
	Data                interface{}    `json:"data" mapstructure:"data"`
}
