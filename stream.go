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
	ST_Starting
	ST_IPC_Connected
	ST_Stopping
	//ST_None              // don't switch stream state - used for user requests
)

/*
type UserRequest int
const (
	UR_Stop              UserRequest = iota
	UR_Start
	UR_Play
	UR_Restart
)
*/

type StreamCtl struct {
	cmd       string
	val       string
}

type Stream struct {
	notifications            chan<- *Notification
	stream_id                int

	player_cfg              *PlayerConfig
	state                    StreamState
	//user_state               string
	//target_state             UserRequest
	//play                     bool              // play request from user -> true, stop request -> false
	//user_restart             bool              // user triggered restart (overrides auto-restart conditions)
	//user_stopped             bool              // player stop triggered by user (stop or restart)

	// stuff used by public methods
	ctl_chan                 chan *StreamCtl
	//shutdown                 chan struct{}
	user_shutdown            bool

	// Player stuff
	player_cmd              *cmd.Cmd
	//cmd_status              *cmd.Status        // last player cmd.Status
	cmd_status             <-chan cmd.Status

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

func (stream * Stream) Play() {
	stream.Control(&StreamCtl{cmd:"play",val:"yes"})
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

	for (stream.ctl_chan != nil) || (stream.state != ST_Stopped) {

		select {

			// control channel from StreamHub
			case ctl, ok := <- stream.ctl_chan:
				if !ok {
					stream.ctl_chan = nil
					stream.request_state("stop")
					break;
				}

				if ctl.cmd == "play" {
					stream.request_state(ctl.val)
				} else { stream.player_ctl(ctl)	}

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
					stream.ipc_shutdown()
				}

		} // select
	} // for loop
}

func (stream * Stream) send_status_note(status string, cmd_status *cmd.Status) {
	player_status := &PlayerStatus{Status:status}

	if cmd_status != nil {
		exit_code := cmd_status.Exit
		player_status.Exit_code = &exit_code
		if cmd_status.Error != nil {
			player_status.Error = cmd_status.Error.Error()
		}
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

func schedule_restart(cmd_status *cmd.Status) time.Duration {
	res := time.Duration(-1) // TBD
	return res
}

func (stream * Stream) player_stopped(cmd_status *cmd.Status) {
	stream.state = ST_Stopped
	note        := "stopped"

	delay := schedule_restart(cmd_status) // TBD
	if delay == 0 {
		stream.player_start()
		delay = time.Millisecond * 100  // set IPC reconnect timer
	}

	if delay > 0 {
		stream.ticker    = time.NewTicker(delay)
		stream.ticker_ch = stream.ticker.C
		stream.state     = ST_Starting
		note             = "starting"
	}
	stream.send_status_note(note, cmd_status)
}

func (stream * Stream) request_state(new_state string) {
	// TBD
}

/* sends state change notifications & sets ticker for player start / IPC reconnect
 * doesn NOT invoke player_stop/_start (the latter is triggered via ticker)
 *
 * actions:
 * - ST_IPC_Connected    : send playing note
 * - ST_Starting         : set IPC reconnect ticker, send starting notification
 * - ST_Stopping         : send stopping/restarting notification
 * - ST_Stopped          : decide on restart, set ticker, send stopped/restarting notification
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
/*
func (stream * Stream) state_update(new_state StreamState) {

	if stream.state == new_state { return }   // state didn't change - nothing to do

	stream.ticker_stop()  // stop ticker first, restart if necessary

	if new_state != ST_None {
		stream.state = new_state
	}

	player_status := &PlayerStatus{}
	delay         := time.Duration(-1)

	switch stream.state {

		case ST_Stopped:

			// enforce (re)start if user issued (Re)Start request
			// otherwise make restart decision based on exit code and restart config
			start_player := (stream.target_state == UR_Restart) || (stream.target_state == UR_Start)

			// clear (Re)Start request, set to Play
			if stream.target_state != UR_Stop {
				stream.target_state = UR_Play
			}

			// copy player exit status to notification
			cmd_status        := stream.cmd_status
			exit_code         := 4 // default
			if cmd_status != nil {
				exit_code = cmd_status.Exit
				player_status.Exit_code = &exit_code
				if cmd_status.Error != nil {
					player_status.Error = cmd_status.Error.Error()
				}
			}
			stream.cmd_status  = nil

			config := stream.player_cfg
			start_player = start_player || ((exit_code == 0) && config.restart_user_quit)
			if stream.target_state == UR_Stop {
				start_player = false
			} else if start_player {
				delay        = time.Millisecond * 10
			} else if (cmd_status.Exit > 0) && (cmd_status.Exit < 127) {
				start_player = true
				delay        = config.restart_error_delay
			}

			// supplement player status notification
			if delay > 0 {
				player_status.Status = "starting"
			} else {
				player_status.Status = "stopped"
			}

		case ST_Starting:
			delay                = time.Millisecond * 100    // IPC reconnect ticker

		case ST_IPC_Connected:
			player_status.Status = "playing"

		case ST_Stopping:
			if stream.user_restart {
				player_status.Status = "restarting"
			} else {
				player_status.Status = "stopping"
			}

	} // switch stream.state

	if delay > 0 {
		stream.ticker    = time.NewTicker(delay)
		stream.ticker_ch = stream.ticker.C
	}

	if player_status.Status == "" { return }

	json_msg, _ := json.Marshal(player_status)
	note := &Notification{
		stream_id    : stream.stream_id,
		notification : "player_status",
		payload      : player_status,
		json_message : json_msg,
	}
	stream.notifications <- note
}
*/

/* start player or IPC reconnect depending on state */
func (stream *Stream) ticker_evt() {

	if stream.state != ST_Starting {
		// shouldn't happend
		fmt.Println("stream: spurious ticker evt!", "state:", stream.state)
		stream.ticker_stop()
		return
	}

	if stream.player_cmd == nil {
		stream.ticker_stop()
		stream.player_start()
		// setup IPC connect ticker
		stream.ticker    = time.NewTicker(time.Millisecond * 100)
		stream.ticker_ch = stream.ticker.C
	} else {
		stream.player_events, _ = stream.ipc_start()
		// TODO: stop/keep ticker depending on success & target_state
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

func (stream * Stream) player_stop() {
	stream.player_ctl(&StreamCtl{cmd:"quit"})
	stream.ipc_shutdown()

	if stream.player_cmd != nil {
		stream.player_cmd.Stop()
		stream.player_cmd = nil
	}
}

/* - triggered by user          : start a stopped stream
 * - triggered by player stopped: restart stream */
func (stream * Stream) player_start() {

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
	if !stream.ipc_good { return }
	stream.ipc_good      = false
	stream.ipc_conn      = nil
	stream.player_events = nil
}

/* only called by timer_evt when stream.state == ST_Starting
 *
 * start IPC connection to player
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

	stream.ipc_good = true

	/* TBD: abort if user requested stop/restart */
	/*
	if stream.target_state != UR_Play {
		stream.player_ctl(&StreamCtl{cmd:"quit"})
		stream.ipc_shutdown()
		stream.state_update(ST_Stopping)
		return nil, nil
	}
	*/

	ipc_conn.SetWriteDeadline(time.Time{})
	notes := make(chan *Notification, 8)

	stream.state = ST_IPC_Connected
	stream.send_status_note("playing", nil)

	// receiver goroutine
	go func() {

		defer func() {
			close(notes)
			ipc_conn.Close()
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
