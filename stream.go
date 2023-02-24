package main

import (
	"net"
	"fmt"
	"time"
	"math"
	"bufio"
	"strings"
	"regexp"
	"runtime"
	"encoding/json"
	"github.com/go-cmd/cmd"
	"github.com/mitchellh/mapstructure"
)

const stream_debug = false

type StreamState int
const (
	ST_Stopped           StreamState = iota
	ST_Starting
	ST_IPC_Connected
	ST_Stopping
)
func (ss StreamState) String() string {
    return [...]string{
		"ST_Stopped",
		"ST_Starting",
		"ST_IPC_Connected",
		"ST_Stopping",
		}[ss]
}

type UserRequest int
const (
	UR_Stop              UserRequest = iota
	UR_Start
	UR_Play
	UR_Restart
)
func (ur UserRequest) String() string {
    return [...]string{
		"UR_Stop",
		"UR_Start",
		"UR_Play",
		"UR_Restart",
		}[ur]
}

type TickerTarget int
const (
	TT_None              TickerTarget = iota
	TT_Player_Start
	TT_IPC_Start
)
func (tt TickerTarget) String() string {
    return [...]string{
		"TT_None",
		"TT_Player_Start",
		"TT_IPC_Start",
		}[tt]
}

type StreamCtl struct {
	cmd       string
	val       string
}

type BufSync struct {
	min_duration    float64
	start_ts        time.Time
}

type Stream struct {
	notifications            chan<- *Notification
	stream_id                int

	player_cfg              *PlayerConfig
	state                    StreamState
	target_state             UserRequest
	last_status_note         string

	// stuff used by public methods
	ctl_chan                 chan *StreamCtl
	//shutdown                 chan struct{}
	user_shutdown            bool

	// synchronization stuff for buffer-duration
	buf_sync                 BufSync

	// Player stuff
	player_cmd              *cmd.Cmd
	//cmd_status              *cmd.Status        // last player cmd.Status
	cmd_status             <-chan cmd.Status

	// ticker for player/IPC restart
	ticker_ch              <-chan time.Time
	ticker                  *time.Ticker
	ticker_target            TickerTarget

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

func (stream * Stream) debug() {
	if !stream_debug { return }
	pc, _, _, ok := runtime.Caller(1)
	details := runtime.FuncForPC(pc)
	if !ok || (details == nil) { return }
	fn := details.Name()
	re := regexp.MustCompile(`^.+\.`)
	fn = re.ReplaceAllString(fn,"")
	fmt.Printf("stream[%d].%-16s %-16s %-10s %-16s\n",
		stream.stream_id, fn, stream.state, stream.target_state, stream.ticker_target)
}

/* started in goroutine from NewStream() */
func (stream * Stream) run() {

	for (stream.ctl_chan != nil) || (stream.state != ST_Stopped) {

		select {

			// control channel from StreamHub
			case ctl, ok := <- stream.ctl_chan:
				if !ok {
					stream.ctl_chan = nil
					stream.request_state("no")
					break
				}

				if ctl.cmd == "play" {
					stream.request_state(ctl.val)
				} else { stream.player_ctl(ctl)	}

			// command status channel for player command (fires on player exit)
			case cmd_status := <- stream.cmd_status:
				stream.cmd_status = nil
				if stream_debug {
					fmt.Println("cmd_status",stream.stream_id,cmd_status.Exit,stream.ticker_target)
				}
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
					// change to playing status?
					evt := PlayerEvent{}
					mapstructure.Decode(player_evt.payload, &evt)
					if evt.Event == "playback-restart" {
						stream.send_status_note("playing", nil)
					} else if (evt.Event == "property-change") && (evt.Name == "demuxer-cache-duration") {
						dur, ok := evt.Data.(float64)
						if ok { stream.buffer_duration(dur) }
						// TODO: forward min. demuxer-cache-duration to client
					}
				} else {
					stream.ipc_shutdown()
				}

		} // select
	} // for loop
}

/* stopping   : stop in progress
 * stopped    : player stopped w/o pending restart
 * starting   : player startup triggered
 * restarting : restart triggered by user - info for user(s) only
 *              starting note will be sent when new player starts
 * playing    : mpv started playing (triggered by playback-restart event received) */
func (stream * Stream) send_status_note(status string, cmd_status *cmd.Status) {
	if stream.last_status_note == status { return }
	stream.last_status_note = status

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

/* player exit codes:
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
func (stream * Stream) schedule_restart(cmd_status *cmd.Status) time.Duration {

	if stream.target_state == UR_Stop {
		// do not restart
		return time.Duration(-1)
	} else if (stream.target_state != UR_Play) || (cmd_status == nil) {
		// immediate restart (user requested)
		stream.target_state = UR_Play            // change from (Re)Start to Play
		return time.Duration(0)
	}

	// target_state == UR_Play
	// check cmd_status for restart condition

	config := stream.player_cfg

	if (cmd_status.Exit == 0) && config.restart_user_quit {
		// player quit by user
		return time.Duration(0)
	} else if (cmd_status.Exit > 0) && (cmd_status.Exit < 127) {
		// player quit due to (non-severe) error
		return config.restart_error_delay
	} else {
		// do not restart
		return time.Duration(-1)
	}
}

func (stream * Stream) player_stopped(cmd_status *cmd.Status) {
	stream.debug()
	stream.ticker_stop()
	stream.state         = ST_Stopped
	note                := "stopped"

	delay := stream.schedule_restart(cmd_status)
	if delay == 0 {                     // immediate (re)start
		stream.player_start()
		delay = time.Millisecond * 100  // set IPC reconnect timer
		stream.ticker_target = TT_IPC_Start
	} else if delay>0 {                 // honor restart delay
		stream.ticker_target = TT_Player_Start
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
	target_state := stream.target_state
	switch new_state {
		case "yes" :
			if stream.state == ST_Stopped {
				target_state = UR_Start   // force (Re)start if stopped atm
			} else {
				target_state = UR_Play    // keep playing
			}
		case "no"      : target_state = UR_Stop
		case "restart" : target_state = UR_Restart  // force Stop then (Re)Start
	}

	if stream.target_state == target_state { return }  // no change
	stream.target_state = target_state

	stream.debug()

	// send state change notification
	switch stream.target_state {
		case UR_Stop    : stream.send_status_note("stopping", nil)
		case UR_Restart : stream.send_status_note("restarting", nil)
		// start note will be sent in stream.player_stopped(nil) (in case of restart)
	}

	// current state: Stopped - check if (Re)Start
	if stream.state == ST_Stopped {
		if stream.target_state != UR_Stop {
			// trigger restart now
			stream.player_stopped(nil)
		}
	} else if (stream.state != ST_Stopping) &&
		((stream.target_state == UR_Stop) || (stream.target_state == UR_Restart)) {
		// stream not stopped - Stop or Restart requested
		stream.player_stop()
	}
}

/* start player or IPC reconnect depending on state */
func (stream *Stream) ticker_evt() {
	stream.debug()
	switch stream.ticker_target {
		case TT_Player_Start:
			stream.ticker_stop()
			stream.player_start()
			// setup IPC connect ticker
			stream.ticker        = time.NewTicker(time.Millisecond * 100)
			stream.ticker_ch     = stream.ticker.C
			stream.ticker_target = TT_IPC_Start
		case TT_IPC_Start:
			stream.player_events, _ = stream.ipc_start()
			if stream.player_events != nil { // stop timer if successful
				stream.ticker_stop()
			}
		default:
			// shouldn't happend
			fmt.Println("stream: spurious ticker evt!", "state:", stream.state)
			stream.ticker_stop()
	}
}

/* shutdown ticker (if not yet done) */
func (stream *Stream) ticker_stop() {
	if stream.ticker_ch != nil {
		stream.ticker.Stop()
		stream.ticker        = nil
		stream.ticker_ch     = nil
		stream.ticker_target = TT_None
	}
}

func (stream * Stream) player_stop() {
	stream.debug()
	// nothing to do?
	if (stream.state == ST_Stopped) || (stream.state == ST_Stopping) { return }

	// player not yet started? -> abort before starting player
	if stream.ticker_target == TT_Player_Start {
		// trigger (fake) player_stopped (will stop ticker)
		stream.player_stopped(nil)
		return
	}

	// ST_Starting     : try player_cmd.Stop()
	// ST_IPC_Connected: issue stop command

	stream.state = ST_Stopping

	stream.player_ctl(&StreamCtl{cmd:"quit"})
	stream.ipc_shutdown()

	config := stream.player_cfg

	// mpv won't shutdown properly with cmd.Stop() on windows
	// therefor we must send a quit command via IPC instead
	postpone_stop := (!config.use_streamlink) && (runtime.GOOS == "windows")
	if (stream.player_cmd != nil) && (!postpone_stop) {
		stream.player_cmd.Stop()
		stream.player_cmd = nil
	}
}

func (stream * Stream) player_start() {
	stream.debug()
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

	stream.buf_sync.start_ts = time.Time{}

	// TODO: move to goroutine?
	err = player_observe_properties(&ipc_conn)
	if err != nil {
		ipc_conn.Close()
		stream.ipc_conn = nil
		//log.Println(err)
		return nil, err
	}

	stream.ipc_good = true

	ipc_conn.SetWriteDeadline(time.Time{})
	notes := make(chan *Notification, 8)

	// abort if user requested stop/restart
	// TODO: move before player_observe_properties ?
	if stream.target_state != UR_Play {
		stream.player_ctl(&StreamCtl{cmd:"quit"})
		stream.ipc_shutdown()
		close(notes)
		return notes, nil   // signal success with valid but closed channel
	}

	stream.state = ST_IPC_Connected

	// receiver goroutine
	go func() {

		defer func() {
			close(notes)
			ipc_conn.Close()
		}()

		const ignore = `"request_id":0,"error":"success"}`

		scanner := bufio.NewScanner(ipc_conn)
		for scanner.Scan() {
			msg := scanner.Bytes()
			if strings.Contains(string(msg), ignore) { continue }
			// need to make a copy of the read data to prevent data race
			// when slice data changes in scanner.Bytes()
			json_message := make([]byte, len(msg))
			copy(json_message, msg)

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

func (stream *Stream) buffer_duration(current_duration float64) {
	bs  := &stream.buf_sync
	now := time.Now()

	if(!bs.start_ts.IsZero()) {
		delta_t         := now.Sub(bs.start_ts).Minutes()
		bs.min_duration  = math.Min(current_duration, bs.min_duration)
		//fmt.Println(current_duration, bs.min_duration)
		if(delta_t < 1.0) { return }
		//fmt.Println("========")
		fmt.Println("min buffer duration >=",bs.min_duration,"s")
	}
	bs.start_ts     = now
	bs.min_duration = current_duration
}
