package main

import (
	"net"
	"fmt"
	"time"
	"bufio"
	//"sync"
	"strings"
	"encoding/json"
	"github.com/go-cmd/cmd"
)

type StreamState int
const (
	ST_Stopped           StreamState = iota
	ST_Started
	ST_IPC_Connected
)

type StreamCtl struct {
	cmd       string
	val       string
}

type Stream struct {
	notifications            chan<- *Notification
	stream_idx               int
	ipc_pipe                 string

	player_cfg              *PlayerConfig

	state                    StreamState

	ctl_chan                 chan *StreamCtl
	//shutdown                 chan struct{}
	user_shutdown            bool

	// Player stuff
	player_cmd              *cmd.Cmd
	cmd_status             <-chan cmd.Status   // player cmd.Status
	restart_pending          bool

	ticker_ch              <-chan time.Time
	ticker                  *time.Ticker

	ipc_conn                 net.Conn
	player_events          <-chan *Notification
}

/* user interface */
func NewStream(notifications chan<- *Notification, stream_idx int, player_cfg *PlayerConfig) *Stream {
	stream := &Stream{
		notifications : notifications,
		stream_idx    : stream_idx,

		player_cfg    : player_cfg,

		ctl_chan      : make(chan *StreamCtl, 16),
		//shutdown      : make(chan struct{}),
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
	// TODO: wait? (blocking read on shutdown channel)
}

/* internal stuff
 * all internal functions are called from stream.run() goroutine */

/* started in goroutine from NewStream() */
func (stream * Stream) run() {

	for stream.ctl_chan != nil {

		select {

			// control channel from StreamHub
			case ctl, ok := <- stream.ctl_chan:
				if !ok {
					stream.ctl_chan = nil
					break;
				}

				switch ctl.cmd {
					case "start" : stream.player_start()
					case "stop"  : stream.player_stop()
					default      : stream.player_ctl(ctl)
				}

			// command status channel for player command (fires on player exit)
			case cmd_status := <- stream.cmd_status:
				stream.cmd_status = nil
				stream.player_stopped(&cmd_status)

			// timer
			case _ = <-stream.ticker_ch:
				stream.ticker_evt()

			/* use an extra channel and forward player events here
			 * this is done to prevent player events arriving after
			 * the player has been stopped */
			case player_evt, ok := <-stream.player_events:
				if ok { 
					stream.notifications <- player_evt 
				} else {
					//fmt.Println("player_evt channel closed", stream.stream_idx)
					stream.ipc_shutdown()
				}

		} // select
	 } // for loop
	stream.player_stop()
}

func (stream *Stream) ticker_evt() {
	if stream.state == ST_Started {
		stream.player_events, _ = stream.ipc_start()
	} else if stream.state == ST_Stopped {
		stream.player_start()
	} else {
		// shouldn't happend
		fmt.Println("stream: spurious ticker evt!", "state:", stream.state)
		stream.ticker_stop()
	}
}

func (stream *Stream) ticker_stop() {
	if stream.ticker_ch != nil {
		stream.ticker.Stop()
		stream.ticker    = nil
		stream.ticker_ch = nil
	}
}

func (stream * Stream) state_change(new_state StreamState) {

	if stream.state == new_state { return }   // nothing to do

	stream.ticker_stop()  // stop ticker first, restart if necessary

	// TODO: restarting state?
	stream.state = new_state

	delay := time.Duration(-1)

	switch stream.state {
	case ST_Started:
		delay = time.Millisecond * 100    // IPC reconnect ticker
	case ST_Stopped:
		// TODO: optional restart, delay depending on exit reason
		// restart = time.Millisecond
		delay = stream.player_cfg.restart_error_delay
		// TODO: cleanup IPC connection here?
	default:
	}

	// TODO: restarting state?

	if delay > 0 {
		stream.ticker    = time.NewTicker(delay)
		stream.ticker_ch = stream.ticker.C
	}

	/* TODO: send player status update */
	// TODO: player_status
/*
	status := &Notification{
		stream_idx   : stream.stream_idx,
		notification : "player_status",
		payload      : player_status,
		json_message : json.Marshal(status),
	}
	stream.notifications <- status
*/
	//player.Status <- &PlayerStatus{Status : "started"}
}

/*
 * player exit codes:
 * - streamlink twitch user offline: ................. 1
 * - streamlink mpv twitch play until user quits mpv:  0
 * - mpv twitch user offline: ........................ 2
 * - mpv twitch play until user quits mpv:             0
 * - bash command not found: ....................... 127
 */
func (stream * Stream) player_stopped(cmd_status *cmd.Status) {
	// TBD: cmd_status.Exit, etc.
	stream.state_change(ST_Stopped)
}

func (stream * Stream) player_start() {

	// restart?
	if stream.state != ST_Stopped {
		stream.player_stop()
		stream.restart_pending = true
		return
	}
	stream.restart_pending = false

	config          := stream.player_cfg
	player_cmd      := "streamlink"
	var player_args  = []string{}
	mpv_args        := config.mpv_args[:]

	if len(config.ipc_pipe) > 0 {
		mpv_args = append(mpv_args, "--input-ipc-server=" + config.ipc_pipe)
	}

	if config.use_streamlink {
		player_args = append(player_args, config.streamlink_args...)
		player_args = append(player_args, "-a", strings.Join(mpv_args," "), config.location, "best")
	} else {
		player_cmd  = "mpv"
		player_args = append(player_args, mpv_args...)
		player_args = append(player_args, config.location)
	}

	//fmt.Println(config.mpv_args)
	//fmt.Println(player_cmd, "\""+strings.Join(player_args,"\" \"")+"\"")

	cmdOptions        := cmd.Options{ Buffered:  false, Streaming: false }
	stream.player_cmd  = cmd.NewCmdOptions(cmdOptions, player_cmd, player_args...)
	stream.cmd_status  = stream.player_cmd.Start()

	// schedule IPC (re)connect
	stream.state_change(ST_Started)
}

func (stream * Stream) player_stop() {
	if stream.state == ST_Stopped { return }
	stream.player_ctl(&StreamCtl{cmd:"quit"})
	stream.ipc_shutdown()
	if stream.player_cmd != nil {
		stream.player_cmd.Stop()
		stream.player_cmd = nil
	}
	stream.state_change(ST_Stopped)
	stream.restart_pending = false // TODO: ???
}

func (stream * Stream) player_ctl(ctl *StreamCtl) {
	var str string
	if stream.ipc_conn == nil { return }
	switch ctl.cmd {
	case "quit" : str = `{"command":["quit"]}`+"\n"
	case "seek" : str = fmt.Sprintf(`{"command":["osd-msg-bar","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	default     : str = fmt.Sprintf(`{"command":["osd-msg-bar","set","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	}
	stream.ipc_conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	stream.ipc_conn.Write([]byte(str))
}

/*
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
*/

func (stream *Stream) ipc_shutdown() {
	if stream.ipc_conn != nil {
		stream.ipc_conn.Close()
		stream.ipc_conn      = nil
		stream.player_events = nil
	}
}

func (stream *Stream) ipc_start() (<-chan *Notification, error) {
	ipc_conn, err := dial_pipe(stream.player_cfg.ipc_pipe)
	stream.ipc_conn = ipc_conn
	if err != nil { return nil, err }

	// TODO: move to goroutine?
	err = player_observe_properties(&ipc_conn)
	if err != nil {
		ipc_conn.Close()
		stream.ipc_conn = nil
		//log.Println(err)
		return nil, err
	}
	ipc_conn.SetWriteDeadline(time.Time{})

	notes := make(chan *Notification, 8)

	// state update
	stream.state_change(ST_IPC_Connected)

	go func() {

		defer func() {
			ipc_conn.Close()           // Multiple goroutines may invoke methods on a Conn simultaneously.
			close(notes)
		}()

		const ignore = `"request_id":0,"error":"success"}`

		scanner := bufio.NewScanner(ipc_conn)
		for scanner.Scan() {
			json_message := scanner.Bytes()
			if strings.Contains(string(json_message), ignore) { continue }
			//fmt.Println(string(data),"#")

			var payload interface{}
			_ = json.Unmarshal(json_message, &payload)
			status := &Notification{
				stream_idx   : stream.stream_idx,
				notification : "player_event",
				payload      : payload,
				json_message : json_message,
			}
			/* non-blocking send to avoid non-responsive goroutine
			 * once the channel receiver in stream.run() is gone */
			select {
				case notes <- status: // drop if channel full
				default:
			}
		}

		//if err := scanner.Err(); err != nil {
			//log.Println(err)
		//}
	}() // receiver goroutine

	return notes, err
}

func player_observe_properties(conn *net.Conn) error {
	mpv_properties := [...]string{
		"mute", "volume",
		//"time-pos",
		"media-title",
		"video-format", "video-codec",
		"width", "height",

		/* Approximate time of video buffered in the demuxer, in seconds.
		Same as demuxer-cache-duration but returns the last timestamp of buffered data in demuxer.
		* unsuitable: 10 13932.024222 map[name:demuxer-cache-time]} for ~10s cache delay */
		//"demuxer-cache-time",

		/* 3073 property updates during reference run
		 * much more precise than demuxer-cache-duration */
		//"time-remaining",

		/* 240 property updates during reference run
		 * less precise than time-remaining */
		"demuxer-cache-duration",

		"paused-for-cache",

		/* this is false for streaming */
		// "partially-seekable",

		"video-bitrate",
	}
	var msg = make([]byte, 0, 1024)
	for _, p := range mpv_properties {
		msg = append(msg, "{\"command\":[\"observe_property\",0,\""+p+"\"]}\n"...)
	}
	(*conn).SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	_, err := (*conn).Write(msg)
	return err
}
