package main

import (
	"net"
	"fmt"
	"time"
	"bufio"
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
	stream_id                int
	ipc_pipe                 string

	player_cfg              *PlayerConfig

	state                    StreamState

	// stuff used by public Control/Start/Stop/Shutdown methods
	ctl_chan                 chan *StreamCtl
	//shutdown                 chan struct{}
	user_shutdown            bool

	// Player stuff
	player_cmd              *cmd.Cmd
	cmd_status             <-chan cmd.Status   // player cmd.Status
	user_restart             bool              // user triggered restart (overrides auto-restart conditions)
	user_stopped             bool              // user stopped -> disable restarts until user starts stream again

	// ticker for player/IPC restart
	ticker_ch              <-chan time.Time
	ticker                  *time.Ticker

	// IPC connection to player
	ipc_conn                 net.Conn
	ipc_good                 bool
	player_events          <-chan *Notification
}

/* user interface */
func NewStream(notifications chan<- *Notification, stream_id int, player_cfg *PlayerConfig) *Stream {
	stream := &Stream{
		notifications : notifications,
		stream_id     : stream_id,

		player_cfg    : player_cfg,

		ctl_chan      : make(chan *StreamCtl, 16),
		//shutdown      : make(chan struct{}),
	}
	go stream.run()
	return stream
}

func (stream * Stream) Control(ctl *StreamCtl) {
	if stream.user_shutdown { return }
	select {
		/* send non-blocking so a slow/blocked Stream instance
		 * won't block the StreamHub */
		case stream.ctl_chan <- ctl:
		default:
	}
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

/* internal stuff follows
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
					case "stop"  : stream.player_stop(false, true)
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
					//fmt.Println("player_evt channel closed", stream.stream_id)
					stream.ipc_shutdown()
				}

		} // select
	 } // for loop
	stream.player_stop(false, true)
}

/* sends state change notifications & sets ticker for player start / IPC reconnect
 * doesn NOT invoke player_stop/_start (the latter is triggered via ticker)
 * 
 * actions:
 * - ST_IPC_Connected        : NOP
 * - ST_Started              : set IPC reconnect ticker, send started notification
 * - ST_Stopped              : send stopped/restarting notification
 * - ST_Stopped & cmd.Status : decide on restart, set ticker, send stopped/restarting notification
 * 
 * player exit codes:
 * - streamlink twitch user offline: ................. 1
 * - streamlink mpv twitch play until user quits mpv:  0
 * - mpv twitch user offline: ........................ 2
 * - mpv twitch play until user quits mpv:             0
 * - mpv play file until EOF ......................... 0
 * - mpv quit via IPC ................................ 4
 * - bash command not found: ....................... 127
 * 
 * restart if:
 * - user_restart OR
 * - cmd_status.Exit == 0 (player user_quit) && config.restart_user_quit OR
 * - (cmd_status.Exit > 0) && (cmd_status.Exit < 127) && config.restart_error_delay
 */
func (stream * Stream) state_change(new_state StreamState, cmd_status *cmd.Status) {

	if (stream.state == new_state) && (cmd_status == nil) { return }   // state didn't change - nothing to do

	stream.ticker_stop()  // stop ticker first, restart if necessary

	stream.state = new_state

	if (stream.state == ST_IPC_Connected) { return }   // ST_IPC_Connected: nothing to do

	player_status := &PlayerStatus{}
	delay         := time.Duration(-1)

	if stream.state == ST_Started {
		player_status.Status = "started"
		delay                = time.Millisecond * 100    // IPC reconnect ticker
	} else { // ST_Stopped

		// player stopped - setup ticker for restart if applicable
		if cmd_status != nil {

			// copy player exit status to notification
			exit_code := cmd_status.Exit
			player_status.Exit_code = &exit_code
			if cmd_status.Error != nil {
				player_status.Error = cmd_status.Error.Error()
			}

			// restart player?
			config := stream.player_cfg
			//fmt.Println("restart?", stream.user_restart, stream.user_stopped, cmd_status.Exit, config.restart_user_quit, config.restart_error_delay)
			if stream.user_stopped { // don't restart
			} else if stream.user_restart || ((cmd_status.Exit == 0) && config.restart_user_quit) {
				delay = time.Millisecond * 10
			} else if (cmd_status.Exit > 0) && (cmd_status.Exit < 127) {
				delay = config.restart_error_delay
			}
		} // cmd_status != nil (player stop complete)

		// supplement player status notification
		if (delay > 0) || (stream.user_restart) {
			player_status.Status = "restarting"
		} else {
			player_status.Status = "stopped"
		}
	}

	if delay > 0 {
		stream.ticker    = time.NewTicker(delay)
		stream.ticker_ch = stream.ticker.C
	}

	json_msg, _ := json.Marshal(player_status)
	note := &Notification{
		stream_id    : stream.stream_id,
		notification : "player_status",
		payload      : player_status,
		json_message : json_msg,
	}
	stream.notifications <- note
}

/* start player or IPC reconnect depending on state */
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

/* shutdown ticker (if not yet done) */
func (stream *Stream) ticker_stop() {
	if stream.ticker_ch != nil {
		stream.ticker.Stop()
		stream.ticker    = nil
		stream.ticker_ch = nil
	}
}

/* player has stopped with exit code */
func (stream * Stream) player_stopped(cmd_status *cmd.Status) {
	//stream.ipc_shutdown()
	stream.state_change(ST_Stopped, cmd_status)
}

/* only triggered by user action:
 * - explicit stop (don't restart)
 * - implicit stop for restart (invoked from player_start in latter case) */
func (stream * Stream) player_stop(user_restart bool, user_stopped bool) {
	stream.user_restart = user_restart
	stream.user_stopped = user_stopped

	if stream.state == ST_Stopped { return }

	stream.player_ctl(&StreamCtl{cmd:"quit"})
	//stream.ipc_good = false
	stream.ipc_shutdown()

	if stream.player_cmd != nil {
		stream.player_cmd.Stop()
		stream.player_cmd = nil
	}
	stream.state_change(ST_Stopped, nil)
}

/* - triggered by user          : start a stopped stream
 * - triggered by player stopped: restart stream */
func (stream * Stream) player_start() {

	// restart?
	if stream.state != ST_Stopped {
		stream.player_stop(true, false)
		return
	}

	// clear user_restart and user_stopped (if set)
	stream.user_restart = false
	stream.user_stopped = false

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

	stream.state_change(ST_Started, nil)
}

/* send control command to player via IPC connection */
func (stream * Stream) player_ctl(ctl *StreamCtl) {
	var str string
	if !stream.ipc_good { return }
	switch ctl.cmd {
	case "quit" : str = `{"command":["quit"]}`+"\n"
	case "seek" : str = fmt.Sprintf(`{"command":["osd-msg-bar","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	default     : str = fmt.Sprintf(`{"command":["osd-msg-bar","set","%s","%s"]}`+"\n",ctl.cmd,ctl.val)
	}
	stream.ipc_conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	stream.ipc_conn.Write([]byte(str))
}

/* shutdown IPC connection (if not yet done) */
func (stream *Stream) ipc_shutdown() {
	//if stream.ipc_conn != nil {
	if stream.ipc_good {
		stream.ipc_conn.Close()
		stream.ipc_good      = false
		stream.ipc_conn      = nil
		stream.player_events = nil
	}
}

/* start IPC connection to player
 * starts a goroutine for reading player events and sends them to
 * exclusive notification channel (closed before goroutine terminates) */
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
	stream.state_change(ST_IPC_Connected, nil)
	stream.ipc_good = true

	// receiver goroutine
	go func() {

		defer func() {
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
				stream_id    : stream.stream_id,
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
	}() // receiver goroutine

	return notes, err
}

/* register value change notifications for certain player properties via player IPC conn */
func player_observe_properties(conn *net.Conn) error {
	mpv_properties := [...]string{
		"mute", "volume",
		//"time-pos",
		"media-title",
		"video-format", "video-codec", "video-bitrate",
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
	}
	var msg = make([]byte, 0, 1024)
	for _, p := range mpv_properties {
		msg = append(msg, "{\"command\":[\"observe_property\",0,\""+p+"\"]}\n"...)
	}
	(*conn).SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	_, err := (*conn).Write(msg)
	return err
}
