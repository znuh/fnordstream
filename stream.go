package main

import (
	"fmt"
	//"strconv"
	//"encoding/json"
)

type StreamCtl struct {
	cmd       string
	val       string
}

type Stream struct {
	notifications     chan<- *Notification
	stream_idx        int
	ipc_pipe          string

	//location          string
	//viewport         *Geometry
	//options           map[string]bool

	player_cfg       *PlayerConfig

	ctl_chan          chan *StreamCtl
	user_shutdown     bool

	//player_started    bool
	player           *Player
	restart_pending   bool
}

/* user interface */
//hub.pipe_prefix + strconv.Itoa(stream.stream_idx),
func NewStream(notifications chan<- *Notification, stream_idx int, player_cfg *PlayerConfig) *Stream {
//ipc_pipe string, location string, viewport *Geometry, options map[string]bool) *Stream {
	stream := &Stream{
		notifications : notifications,
		stream_idx    : stream_idx,
//		ipc_pipe      : ipc_pipe,

//		location      : location,
//		viewport      : viewport,
//		options       : options,
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

		} // select
	 } // for loop

}

func (stream * Stream) player_start() {
	// restart?
	if stream.player != nil {
		stream.restart_pending = true
		stream.player_stop()
		return
	}
	stream.restart_pending = false

	stream.player = NewPlayer(stream.player_cfg)
//	go mux_player(hub.notifications, player)

}

func (stream * Stream) player_stop() {
	if stream.player == nil { return }
}

func (stream * Stream) player_ctl(ctl *StreamCtl) {
  	var str string
	if ctl.cmd == "seek" {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	} else {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","set","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	}
	str = str
	// TBD
}

/*

// stream (re)start triggered via webui
// OR triggered via stream_status with restart_pending == true
func stream_start(hub *StreamHub, idx int) {
	location := hub.stream_locations[idx]
	viewport := hub.viewports[idx]
	options  := hub.playback_options

	// restart player if already started
	if hub.player_by_idx[idx] != nil {
		stream_stop(hub, idx)
		hub.restart_pending[idx] = true
		return
	}

	hub.restart_pending[idx] = false

	hub.player_by_idx[idx]    = player
	hub.idx_by_player[player] = idx
}

func mux_player(send chan<- *Notification, player *Player) {
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
			src           : player,
			notification  : note_type,
			payload       : payload,
			json_message  : json_message,
		}
		//fmt.Println(note)
		send <- &note
	} // for !closed
	//fmt.Println("mux_player done")
}
*/
