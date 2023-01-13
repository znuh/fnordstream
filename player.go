package main

import (
	//"fmt"
	//"log"
	"net"
	"time"
	"bufio"
	"sync"
	"strings"
	"github.com/go-cmd/cmd"
)

type Player struct {
	/* control & status channels */
	Control                  chan []byte
	Status                   chan *PlayerStatus
	status_wg                sync.WaitGroup

	config                  *PlayerConfig

	running                  bool
	quit                     bool

	player_restart         <-chan time.Time
	player_restart_timer    *time.Timer

	ipc_reconnect          <-chan time.Time
	ipc_reconnect_ticker    *time.Ticker

	/* go-cmd stuff */
	cmd                     *cmd.Cmd
	cmd_chan               <-chan cmd.Status

	ipc_conn                 net.Conn
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

	json_message        []byte    // JSON message from IPC channel if IPC event
}

/*
 * exit codes:
 * - streamlink twitch user offline: ................. 1
 * - streamlink mpv twitch play until user quits mpv:  0
 * - mpv twitch user offline: ........................ 2
 * - mpv twitch play until user quits mpv:             0
 * - bash command not found: ....................... 127
 */

func PlayerStart(config *PlayerConfig) <-chan cmd.Status {
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

	cmdOptions := cmd.Options{ Buffered:  false, Streaming: false }
	cmd        := cmd.NewCmdOptions(cmdOptions, player_cmd, player_args...)
	return cmd.Start()
}

func (player *Player) control(msg []byte) {
	if (player.ipc_conn == nil) { return }
	// TODO: add write timeout?
	// TODO: mutex for ipc closed status? (start_ipc)
	player.ipc_conn.Write(msg)
}

func player_quit_event(player *Player, cmd_status *cmd.Status) {

	if !player.running { return }

	player.running = false
	player.cancel_ipc_reconnect()

	//log.Println("player quit - exit code: ", cmd_status.Exit)

	restart := time.Duration(-1)

	config := player.config

	if cmd_status.Exit == 0 {  // user quit
		if config.restart_user_quit {
			restart = time.Millisecond
		}
	} else if (cmd_status.Exit > 0) && (cmd_status.Exit < 127) {
		// error (stream offline, location not found, etc.)
		restart = config.restart_error_delay
	} else {
		 // some serious error (usually command not found)
		//log.Println(cmd_status.Error)
	}

	player.quit = !(restart > 0)
	/* schedule player restart */
	if !player.quit {
		player.player_restart_timer = time.NewTimer(restart)
		player.player_restart       = player.player_restart_timer.C
	}

	/* send player status update */
	status := &PlayerStatus{
		Status     : "stopped",
		Exit_code  : cmd_status.Exit,
	}
	if cmd_status.Error != nil {
		status.Error = cmd_status.Error.Error()
	}
	if !player.quit {
		status.Status = "restarting"
	}
	player.Status <- status
}

func (player *Player) schedule_ipc_reconnect() {
	player.cancel_ipc_reconnect() // cancel if already running
	player.ipc_reconnect_ticker = time.NewTicker(time.Millisecond * 100)
	player.ipc_reconnect        = player.ipc_reconnect_ticker.C
}

func (player *Player) cancel_ipc_reconnect() {
	if player.ipc_reconnect_ticker != nil {
		player.ipc_reconnect_ticker.Stop()
		player.ipc_reconnect_ticker = nil
	}
}

func (player *Player) run() {
	player.start()

	send_exitnote := false
	for !player.quit {

		select {

			case ctl, ok := <-player.Control:          // control channel
				if !ok {                               // channel closed -> stop player
					player.control([]byte(`{"command":["quit"]}`+"\n"))
					player.quit   = true
					send_exitnote = true               // need to send note on exit
				} else {
					player.control(ctl)                // command for player
				}

			case cmd_status, ok := <- player.cmd_chan:     // message from cmd channel -> player no longer running
				if ok {
					player_quit_event(player, &cmd_status)
				}

			case _ = <-player.ipc_reconnect:           // try connecting to IPC
				player.start_ipc()

			case _ = <-player.player_restart:          // restart player
				player.start()

		} // select
	} // for loop

	/* shutdown */

	/* stop player */
	if player.running {
		player.cmd.Stop()
		player.running = false
	}

	/* shutdown IPC connection */
	if player.ipc_conn != nil {
		player.ipc_conn.Close()
		player.ipc_conn = nil
	}

	player.cancel_ipc_reconnect()

	if player.player_restart_timer != nil {
		player.player_restart_timer.Stop()
		player.player_restart_timer = nil
	}

	/* send player status update */
	if send_exitnote {
		player.Status <- &PlayerStatus{Status:"stopped"}
	}

	/* wait until IPC reader goroutine releases Status channel,
	 * then close Status channel */
	player.status_wg.Wait()
	close(player.Status)
	//fmt.Println("player_quit", player)
}

func (player *Player) start() {
	config := player.config

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

	cmdOptions       := cmd.Options{ Buffered:  false, Streaming: false }
	player.cmd        = cmd.NewCmdOptions(cmdOptions, player_cmd, player_args...)
	player.cmd_chan   = player.cmd.Start()

	player.running    = true

	player.schedule_ipc_reconnect()

	/* send player status update */
	player.Status <- &PlayerStatus{Status : "started"}
}

func observe_properties(conn *net.Conn) error {
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
	_, err := (*conn).Write(msg)
	return err
}

func (player *Player) start_ipc() error {
	ipc_conn, err := dial_pipe(player.config.ipc_pipe)
	player.ipc_conn = ipc_conn
	if err != nil { return err }

	err = observe_properties(&ipc_conn)
	if err != nil {
		ipc_conn.Close()
		player.ipc_conn = nil
		//log.Println(err)
		return err
	}

	player.cancel_ipc_reconnect()

	player.status_wg.Add(1)            // prevent close() of Status channel while we're using it

	go func() {

		defer func() {
			ipc_conn.Close()           // Multiple goroutines may invoke methods on a Conn simultaneously.
			player.status_wg.Done()    // allow close() of Status channel
		}()

		const ignore = `"request_id":0,"error":"success"}`

		scanner := bufio.NewScanner(ipc_conn)
		for scanner.Scan() {
			data := scanner.Bytes()
			if strings.Contains(string(data), ignore) { continue }
			//fmt.Println(string(data),"#")
			status := &PlayerStatus{
				json_message : data,
			}
			player.Status <- status
		}

		//if err := scanner.Err(); err != nil {
			//log.Println(err)
		//}
	}() // receiver goroutine

	return err
}
