package main

import "flag"

/* TODOs:
 * - add detection for missing mpv/streamlink/yt-dlp/etc.
 * - web: add stream status indicator
 * - go: extend global status on connect (include stream info, etc.)
 * - go: OSX monitor detection
 * - layout customisation (incl. multi-monitor)
 * - auto-sync for stream buffers
 * - go: rewrite Stream/Player mess
 * - web: add support for remote displays
 * - go: add support for remote clients (w/ IP/user auth?)
 */
func main() {
	no_web := flag.Bool("no-web", false, "disable webui")
	flag.Parse()

	shub := NewStreamHub()
	go shub.Run()

	if len(flag.Args()) > 0 {
		console_client(shub, flag.Args()[0], *no_web)
	}
	if !(*no_web) {
		run_webui(shub)
	}
}
