package main

import (
	"fmt"
	"encoding/json"
)

type StreamCtl struct {
	cmd       string
	val       string
}

type Stream struct {
	notifications     chan<- *Notification
	stream_idx        int
	ipc_pipe          string

	player_cfg       *PlayerConfig

	ctl_chan          chan *StreamCtl
	user_shutdown     bool

	player           *Player
	restart_pending   bool
}

/* user interface */
func NewStream(notifications chan<- *Notification, stream_idx int, player_cfg *PlayerConfig) *Stream {
	stream := &Stream{
		notifications : notifications,
		stream_idx    : stream_idx,

		player_cfg    : player_cfg,

		ctl_chan      : make(chan *StreamCtl, 16),
	}
	go stream.run()
	return stream
}

func (stream * Stream) Control(ctl *StreamCtl) {
	if stream.user_shutdown { return }
	stream.ctl_chan <- ctl
}

func (stream * Stream) Start() {
	if stream.user_shutdown { return }
	stream.Control(&StreamCtl{cmd:"start"})
}

func (stream * Stream) Stop() {
	if stream.user_shutdown { return }
	stream.Control(&StreamCtl{cmd:"stop"})
}

func (stream * Stream) Shutdown() {
	if stream.user_shutdown { return }
	stream.user_shutdown = true
	close(stream.ctl_chan)
	// TODO: wait?
}

/* internal stuff */

/* started in goroutine */
func (stream * Stream) run() {
	shutdown       := false

	for !shutdown {

		select {

			/* control channel */
			case ctl, ok := <- stream.ctl_chan:
				if !ok {
					shutdown = true
					break;
				}

				switch ctl.cmd {
					case "start": stream.player_start()
					case "stop":  stream.player_stop()
					default:      stream.player_ctl(ctl)
				}

			// TODO: player status, IPC, reconnect timer?

		} // select
	 } // for loop
	stream.player_stop()
}

func (stream * Stream) player_start() {
	// restart?
	if stream.player != nil {
		stream.player_stop()
		stream.restart_pending = true
		return
	}
	stream.restart_pending = false

	stream.player = NewPlayer(stream.player_cfg)
	go mux_player(stream.notifications, stream.player, stream.stream_idx)
}

func (stream * Stream) player_stop() {
	if stream.player == nil { return }
	close(stream.player.Control)
	stream.player = nil
	stream.restart_pending = false
}

func (stream * Stream) player_ctl(ctl *StreamCtl) {
	var str string
	if stream.player == nil { return }
	if ctl.cmd == "seek" {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	} else {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","set","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	}
	select {
		case stream.player.Control <- []byte(str):
		default:
	}
}

func mux_player(send chan<- *Notification, player *Player, stream_idx int) {
	for {
		status, ok := <-player.Status
		if !ok { break }

		json_message := status.json_message
		note_type    := "player_event"
		var payload interface{}
		if json_message != nil {
			_ = json.Unmarshal(json_message, &payload)
		} else {
			note_type       = "player_status"
			json_message, _ = json.Marshal(status)
			payload         = status
		}
		note := Notification{
			stream_idx    : stream_idx,
			notification  : note_type,
			payload       : payload,
			json_message  : json_message,
		}
		//fmt.Println(note)
		send <- &note
	} // for !closed
	//fmt.Println("mux_player done")
}
