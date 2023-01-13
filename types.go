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

func (geo *Geometry) String() string {
        return fmt.Sprintf("%dx%d+%d+%d",geo.W,geo.H,geo.X,geo.Y)
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
	Exit_code           int       `json:"exit_code"`
	Error               string    `json:"error,omitempty"`

//	json_message        []byte    // JSON message from IPC channel if IPC event
}
