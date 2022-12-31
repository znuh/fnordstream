package main

import (
	"fmt"
	//"log"
	"regexp"
)

func (n *Notification) str() string {
	rexp := regexp.MustCompile(`\[[\d+ ]+\]\}$`)
	s    := fmt.Sprint(n)
	json := ""
	//json = string(n.json_message)
	s = rexp.ReplaceAllString(s,json)
	return s+" }"
}

type NotificationHandler func(*StreamHub, *Notification)

func displays_update(hub *StreamHub, note *Notification) {
	hub.displays = note.payload.([]Display)
}

func player_status_update(hub *StreamHub, note *Notification) {
	player     := note.src
	idx, ok    := hub.idx_by_player[player]
	if !ok { return }
	status, ok := note.payload.(*PlayerStatus)
	if !ok { return }
	stream_status(hub, idx, status)
}

var note_handlers = map[string]NotificationHandler{
	"displays"      : displays_update,
	"player_status" : player_status_update,
}

func notification(hub *StreamHub, note *Notification) {
	handler, ok := note_handlers[note.notification]
	//fmt.Println("note: ", note.str())
	if ok {
		handler(hub, note)
	} else {
		//log.Println("note: ", note.str())
	}
}
