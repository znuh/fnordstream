package main

import (
	"fmt"
	"strconv"
	"regexp"
	"encoding/json"
)

func stream_status(hub *StreamHub, idx int, status *PlayerStatus) {
	player := hub.player_by_idx[idx]
	fmt.Println("player_status", idx, status)
	if status.Status == "stopped" {
		// do postponed player deletion
		delete(hub.idx_by_player, player)
		delete(hub.player_by_idx, idx)
		if hub.restart_pending[idx] {
			stream_start(hub, idx)
		}
	}
}

var allowed_ctls = map[string]bool{
	"volume" : true,
	"seek"   : true,
	"mute"   : true,
}

func stream_ctl(hub *StreamHub, idx int, ctl string, value interface{}) {
	player, ok  := hub.player_by_idx[idx]
	if !ok { return }
	allowed, ok := allowed_ctls[ctl]
	if (!ok) || (!allowed) { return }
	// sanitize val
	re  := regexp.MustCompile(`[^a-zA-Z0-9]`)
	val := re.ReplaceAllString(fmt.Sprint(value),"")
	var str string
	if ctl == "seek" {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","%s","%s"]}`+"\n",ctl,val)
	} else {
		str = fmt.Sprintf(`{"command":["osd-msg-bar","set","%s","%s"]}`+"\n",ctl,val)
	}
	select {
		case player.Control <- []byte(str):
		default:
	}
	//fmt.Print("stream_ctl", idx,ctl,value,str)
}

/* stream stopped via webui */
func stream_stop(hub *StreamHub, idx int) {
	player := hub.player_by_idx[idx]
	if player != nil {
		close(player.Control)
	}
	hub.restart_pending[idx] = false
	//fmt.Println("stream_stop", player)
}

/* stream (re)start triggered via webui
 * OR triggered via stream_status with restart_pending == true */
func stream_start(hub *StreamHub, idx int) {
	location := hub.stream_locations[idx]
	viewport := hub.viewports[idx]
	options  := hub.playback_options

	/* restart player if already started */
	if hub.player_by_idx[idx] != nil {
		stream_stop(hub, idx)
		hub.restart_pending[idx] = true
		return
	}

	hub.restart_pending[idx] = false

	mpv_args := []string{
		"--mute=yes",
		"--border=no",
		"--really-quiet",
		"--geometry=" + viewport.String(),
	}
	streamlink_args := []string{
		"--player=mpv",
		"--player-fifo",
		//"-v", // verbose player
	}

	if !options["start_muted"] {
		mpv_args[0] = "--mute=no"
	}

	// sanitize location
	re := regexp.MustCompile(`[^a-zA-Z0-9-_/:.,?&@=#%]`)
	location = re.ReplaceAllString(location, "")

	config := PlayerConfig{
		mpv_args            : mpv_args,
		location            : location,
		ipc_pipe            : hub.pipe_prefix + strconv.Itoa(idx),
		restart_error_delay : -1,
	}

	if options["restart_error"] {
		config.restart_error_delay = hub.restart_error_delay
	}
	config.restart_user_quit = options["restart_user_quit"]
	config.use_streamlink    = options["use_streamlink"]
	if config.use_streamlink {
		if options["twitch-disable-ads"] {
			streamlink_args = append(streamlink_args, "--twitch-disable-ads")
		}
		config.streamlink_args = streamlink_args
	}

	player := NewPlayer(hub.notifications, &config)
	go mux_player(hub.notifications, player)
	hub.player_by_idx[idx]    = player
	hub.idx_by_player[player] = idx
}

func mux_player(send chan *Notification, player *Player) {
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
