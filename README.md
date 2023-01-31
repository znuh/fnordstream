# fnordstream - multiple streams, zero leaks

**fnordstream** is a tool for simultaneous playback of multiple streams from Youtube/Twitch/etc.

**Note:** fnordstream is **NOT** a video player. It needs the [mpv player](https://mpv.io/) and [yt-dlp](https://github.com/yt-dlp/yt-dlp) (or [streamlink](https://streamlink.github.io/)) to work.

* fnordstream has been tested on Linux and Windows. (For OSX display detection is not (yet) implemented.)
* fnordstream comes with a web based user interface.
* There's also a basic console mode which allows starting playback of streams playback without the web UI.<br>(Advanced features such as stopping, (re)starting streams and volume control are only available through the web UI. Web UI can still be used in console mode or disabled if not needed.)
* Communication between web UI and fnordstream is done through a websocket with JSON requests and replies.<br>(Basically you can put together your own tool to communicate with fnordstream through the websock. However at the moment there is no documentation for the JSON requests and responses so you have to inspect the communication in your browser and have a look at webui.js if you want to do this.)

## Screenshots
![Streams setup](https://user-images.githubusercontent.com/198567/215343043-ff044190-a479-40fe-94d5-bd92153e75dc.png)
![Streams playback](https://user-images.githubusercontent.com/198567/215343045-5aa71bbc-ccca-4573-aafe-654017a7c7eb.jpg)

## Usage
* For normal web UI mode just start fnordstream and open http://localhost:8090 in your browser
* Use *fnordstream -h* for help
* You can allow remote clients by changing the listen address with the **-listen-addr** option. (Default is localhost only.)<br>e.g. *fnordstream -listen-addr=:8090* will make fnordstream listen on ALL interfaces.
* If you set the listen address to something other than localhost you **MUST** provide a comma separated whitelist of allowed clients with **-allowed-ips**. Web UI access will be restricted to clients given in this list.
* **-allowed-ips** may contain single IPs, from-to ranges and IP ranges in CIDR notation.<br>
e.g. *-allowed-ips=127.0.0.1,::1,192.168.1.0/24,192.168.2.3,192.168.3.1-192.168.3.23*
* Console mode can be invoked by supplying a profile name or an extra file as last argument.<br>
e.g. *fnordstream Demo*
* The web UI can be disabled with **-no-web** for console-only mode.
