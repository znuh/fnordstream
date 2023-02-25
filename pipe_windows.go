//go:build windows

package main

import (
	"net"
	"time"
	"gopkg.in/natefinch/npipe.v2"
)

func dial_pipe(path string) (net.Conn, error) {
	return npipe.DialTimeout(path,time.Duration(time.Millisecond * 100))
}
