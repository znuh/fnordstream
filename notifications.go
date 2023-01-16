package main

import (
	"fmt"
	"regexp"
	"github.com/mitchellh/mapstructure"
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
	idx := note.stream_idx
	if (idx < 0) || (idx >= len(hub.streams)) { return }

	stream_status := hub.stream_status[idx]
	if stream_status == nil { return }

	status, ok := note.payload.(*PlayerStatus)
	if !ok { return }

	stream_status.Player_status = status.Status

	/* delete old properties */
	if status.Status == "stopped" {
		stream_status.Properties = nil
	} else {
		stream_status.Properties = make(map[string]interface{})
	}
}

func player_event(hub *StreamHub, note *Notification) {
	idx := note.stream_idx
	if (idx < 0) || (idx >= len(hub.streams)) { return }

	stream_status := hub.stream_status[idx]
	if (stream_status == nil) || (stream_status.Properties == nil) {
		return
	}

	evt := PlayerEvent{}
	mapstructure.Decode(note.payload, &evt)

	if evt.Event == "property-change" {
		stream_status.Properties[evt.Name] = evt.Data
	}
}

var note_handlers = map[string]NotificationHandler{
	"displays"      : displays_update,
	"player_status" : player_status_update,
	"player_event"  : player_event,
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
