//go:build !windows

package main

import "net"

func dial_pipe(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}
