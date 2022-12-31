// +build windows

package main

import (
	"gopkg.in/natefinch/npipe.v2"
	"net"
)

func dial_pipe(path string) (net.Conn, error) {
	return npipe.Dial(path)
}
