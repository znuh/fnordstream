
let ws = null;

let displays         = [{"name":"Default","use":true,"geo":{"x":0,"y":0,"w":0,"h":0}}];

let stream_profiles  = {};
let selected_profile = null;

let stream_locations = {};
let stream_nodes     = [];
let viewports        = [];

let streams_active   = undefined;

let tooltipList      = undefined;

function ws_closed() {
  console.log("websock closed");
  ws = null;
}

function append_option(select,val,txt,selected) {
	var opt = document.createElement("option");
	opt.value       = val;
	opt.textContent = txt;
	opt.selected    = selected;
	select.appendChild(opt);
}

function update_stream_profiles(data) {
	if (data)
		stream_profiles = data;
	const profile_select = document.getElementById('profile_select');
	profile_select.textContent="";
	append_option(profile_select,-1,"New...", selected_profile == null);
	let idx=0;
	for (var key in stream_profiles) {
		append_option(profile_select, idx, key, selected_profile == key);
		idx++;
	}
	profile_select.dispatchEvent(new Event("change"));
}

function gather_options() {
	const gather_list = [
		"use_streamlink",
		"twitch-disable-ads",
		"start_muted",
		"restart_error",
		"restart_user_quit"
	];
	let res = {};
	for (let i=0;i<gather_list.length;i++) {
		const id = gather_list[i];
		const val = document.getElementById(id).checked;
		res[id]=val;
	}
	return res;
}

function register_handlers() {
	
	/* profile stuff */
	
	const profile_select       = document.getElementById('profile_select');
	const profile_delete       = document.getElementById('profile_delete');
	const profile_name         = document.getElementById('profile_name');
	const profile_save         = document.getElementById('profile_save');
	const profile_viewports_en = document.getElementById('profile_viewports_en');

	profile_delete.addEventListener('click', (event) => {
	  if(ws)
		ws.send(JSON.stringify(
			{
				request:"profile_delete",
				profile_name  : profile_name.value,
			}));
		delete stream_profiles[profile_name.value];
		selected_profile = undefined;
		update_stream_profiles();
	})

	/* save profile */
	profile_save.addEventListener('click', (event) => {
	  if(!ws) return;
	    let profile = {
			stream_locations : stream_locations,
		}
		if (profile_viewports_en.checked)
			profile.viewports = viewports;
		ws.send(JSON.stringify(
			{
				request       : "profile_save",
				profile_name  : profile_name.value,
				profile       : profile,
			}));
		stream_profiles[profile_name.value] = profile;
		selected_profile = profile_name.value;
		update_stream_profiles();
	})

	profile_name.addEventListener('input', (event) => {
	  profile_save.disabled = profile_name.value == "";
	})

	// save/use viewports from profile?
	profile_viewports_en.addEventListener('change', (event) => {
		if (!event.currentTarget.checked)
			stream_locations = []; // trigger viewports update in stream_urls.input
		stream_urls.dispatchEvent(new Event("input"));
	})

	/* streams */

	/* start stream */
	const streams_start = document.getElementById('streams_start');
	streams_start.addEventListener('click', (event) => {
	  if(ws)
		ws.send(JSON.stringify(
			{
				request   : "start_streams",
				streams   : stream_locations,
				viewports : viewports,
				options   : gather_options(),
			}));
		setup_stream_controls();   // TODO: invoke from server response
		streams_playing(true);
	})

	/* stream URLs changed */
	const stream_urls = document.getElementById('stream_urls');
	stream_urls.addEventListener('input', (event) => {
		let vals = event.target.value.split(/\s+/);
		let last = vals.pop();
		if (last.length > 0)
			vals.push(last);
		const viewports_update = vals.length != stream_locations.length;
		stream_locations = vals;
		streams_start.disabled = !(stream_locations.length > 0);
		// profile_viewports_en.checked?
		if (profile_viewports_en.checked && selected_profile && (stream_profiles[selected_profile].viewports) &&
			stream_profiles[selected_profile].viewports.length >= stream_profiles[selected_profile].stream_locations.length) {
			viewports = stream_profiles[selected_profile].viewports;
			draw_viewports();
		} else if (viewports_update && (ws))
			ws.send(JSON.stringify({request:"suggest_viewports",n_streams:stream_locations.length}));
		//console.log(stream_locations);
	})

	/* display stuff */

	const disp_refresh = document.getElementById('refresh_displays');
	disp_refresh.addEventListener('click', (event) => {
		if(ws) {
			ws.send(JSON.stringify({request:"detect_displays"}));
		}
	})

	/* options */

	const streamlink = document.getElementById('use_streamlink');
	const twitch_noads = document.getElementById('twitch-disable-ads');

	streamlink.addEventListener('change', (event) => {
	  if (event.currentTarget.checked) {
		twitch_noads.disabled = false;
	  } else {
		twitch_noads.disabled = true;
		twitch_noads.checked = false;
	  }
	})

	/* Profile select */
	profile_select.addEventListener('change', (event) => {
		let val = profile_select.options[profile_select.selectedIndex].text;
		if (val == "New...")
			val = "";
		//console.log(val);
		profile_delete.disabled = val == "";
		profile_name.disabled   = val != "";
		profile_name.value      = val;
		profile_save.disabled   = profile_name.value == "";
		// profile selected? apply values
		if (!stream_profiles[val]) return;
		selected_profile = val;
		let locs = stream_profiles[selected_profile].stream_locations;
		stream_urls.value = locs.join("\n") + "\n";
		stream_urls.dispatchEvent(new Event("input"));
	})

	/* ********* control pane *************** */

	const streams_mute_all    = document.getElementById('streams-mute-all');
	streams_mute_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "mute", "yes");
	})

	const streams_stop_all    = document.getElementById('streams-stop-all');
	streams_stop_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stop_stream");
	})

	const streams_play_all    = document.getElementById('streams-play-all');
	streams_play_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "start_stream");
	})

	const streams_ffwd_all    = document.getElementById('streams-ffwd-all');
	streams_ffwd_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "stream_ctl", "seek", "1");
	})

	const streams_restart_all = document.getElementById('streams-restart-all');
	streams_restart_all.addEventListener('click', (event) => {
		ws_sendmulti(undefined, "start_stream");
	})

	const streams_quit = document.getElementById('streams_quit');
	streams_quit.addEventListener('click', (event) => {
	  if(ws)
		ws.send(JSON.stringify({request : "stop_streams"}));
		streams_playing(false);
	})

	const tooltips_en = document.getElementById('tooltips_enable');
	tooltips_en.addEventListener('change', (event) => {
	  if (!tooltipList) {
		const tooltipTriggerList = document.querySelectorAll('[data-bs-toggle="tooltip"]');
		tooltipList = [...tooltipTriggerList].map(tooltipTriggerEl => new bootstrap.Tooltip(tooltipTriggerEl));
	  }
	  if (event.currentTarget.checked)
		tooltipList.map(tt => tt.enable());
	  else
		tooltipList.map(tt => tt.disable());
	})

}

function replace_child(list,id,ext) {
	if ((!list) || (list.length < 1))
		return;

	let res = null;
	for (let i=0;i<list.length;i++) {
		let n = list[i];

		/* replace for-tags as well */
		if (n.attributes && n.attributes["for"] && (n.attributes["for"].value == id)) {
			n.attributes["for"].value += ext;
		}

		if (n.id == id) {
			n.id += ext;
			res = n;
		}

		let res2 = replace_child(n.childNodes, id, ext);
		if (res2)
			res = res2;
	}
	return res;
}

function draw_viewports() {
	const cv      = document.getElementById("viewports");
	const ctx     = cv.getContext("2d");
	ctx.textAlign = 'center';

	/* clear canvas */
	ctx.fillStyle='#ffffff';
	ctx.fillRect(0, 0, cv.width, cv.height);

	for (let i=0;i<viewports.length;i++) {
		const geo = viewports[i];

		//ctx.fillStyle='#aaaaaa';
		//ctx.fillRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.strokeStyle = "#000000";
		ctx.strokeRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);

		const font_h  = geo.h/16;
		ctx.fillStyle = "#000000";
		ctx.font      = font_h+'px sans-serif';
		ctx.fillText(i, 3.5+geo.x/8 + geo.w/16, (geo.y/8)+(geo.h/16)+(font_h/2)-2, geo.w/8);
	}
}

function draw_displays() {

	let w_ext=0, h_ext=0
	for (let i=0;i<displays.length;i++) {
		const geo = displays[i].geo;
		w_ext = Math.max(w_ext, geo.x + geo.w);
		h_ext = Math.max(h_ext, geo.y + geo.h);
	}

	//console.log("extents:",w_ext,h_ext);
	w_ext/=8; h_ext/=8;

	const font_h = 20;
	const cv = document.getElementById("displays");
	cv.width  = w_ext+8;
	cv.height = h_ext+font_h*2;

	const ctx = cv.getContext("2d");
	ctx.font = font_h+'px sans-serif';
	ctx.textAlign = 'center';

	ctx.fillStyle='#ffffff';
	ctx.fillRect(0, 0, cv.width, cv.height);

	for (let i=0;i<displays.length;i++) {
		const geo = displays[i].geo;
		ctx.fillStyle='#dddddd';
		ctx.fillRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.strokeStyle="#000000";
		ctx.strokeRect(3.5+geo.x/8, 0.5+geo.y/8, geo.w/8, geo.h/8);
		ctx.fillStyle='#000000';
		ctx.fillText(displays[i].name, 3.5+geo.x/8 + geo.w/16, (geo.y/8)+(geo.h/8)+font_h-2, geo.w/8);
	}

	const cv2 = document.getElementById("viewports");
	cv2.width  = w_ext+8;
	cv2.height = h_ext+20;

	draw_viewports();
}

function setup_stream_controls() {
	let template = document.getElementById('stream-');
	let parent   = template.parentNode;
	parent.replaceChildren(template);

	stream_nodes = [];

	for (let i=0;i<stream_locations.length;i++) {
		const url = stream_locations[i];
		let n = template.cloneNode(true);
		let children = n.childNodes;
		n.id += i;
		n.hidden = false;

		let idx = replace_child(children,"stream-idx-",i);
		idx.textContent = i;

		let title = replace_child(children,"stream-title-",i);
		title.textContent = url;

		let vol = replace_child(children,"stream-volume-",i);
		vol.addEventListener('input', (event) => {
			const val = event.target.value;
			ws.send(JSON.stringify(
			{
				request     : "stream_ctl",
				stream      : i,
				ctl         : "volume",
				value       : val
			}));
		})

		let buffer = replace_child(children,"stream-buffer-",i);

		let muting = replace_child(children,"stream-muting-",i);
		muting.addEventListener('change', (event) => {
			const val = event.target.checked ? "no" : "yes";
			ws.send(JSON.stringify(
			{
				request     : "stream_ctl",
				stream      : i,
				ctl         : "mute",
				value       : val
			}));
		})

		let exc_unmute = replace_child(children,"stream-exclusive-unmute-",i);
		exc_unmute.addEventListener('click', (event) => {
			ws_sendmulti(i, "stream_ctl", "mute", "yes");
			ws.send(JSON.stringify(
			{
				request     : "stream_ctl",
				stream      : i,
				ctl         : "mute",
				value       : "no",
			}));
		})

		let stop = replace_child(children,"stream-stop-",i);
		stop.addEventListener('click', (event) => {
			if(ws)
				ws.send(JSON.stringify({request:"stop_stream",stream:i}));
		})

		let play = replace_child(children,"stream-play-",i);
		play.addEventListener('click', (event) => {
			if(ws)
				ws.send(JSON.stringify({request:"start_stream",stream:i}));
		})

		let restart = replace_child(children,"stream-restart-",i,true);
		restart.addEventListener('click', (event) => {
			if(ws)
				ws.send(JSON.stringify({request:"start_stream",stream:i}));
		})

		let ffwd = replace_child(children,"stream-ffwd-",i);
		ffwd.addEventListener('click', (event) => {
			ws.send(JSON.stringify(
			{
				request     : "stream_ctl",
				stream      : i,
				ctl         : "seek",
				value       : "1"
			}));
		})

		stream_nodes[i] = {
			idx              : idx,
			title            : title,
			volume           : vol,
			buffer           : buffer,
			muting           : muting,
			exclusive_unmute : exc_unmute,
			stop             : stop,
			play             : play,
			ffwd             : ffwd,
			restart          : restart,
		}

		parent.appendChild(n);
	}
}

function ws_sendmulti(exempt, request, ctl, value) {
	if(!ws) return;
	let msg = "";
	for (let i=0;i<stream_locations.length;i++) {
		if(i==exempt) continue;
		msg += JSON.stringify(
			{
				request     : request,
				stream      : i,
				ctl         : ctl,
				value       : value,
			});
	}
	ws.send(msg);
}

function set_displays() {
	if(!ws) return;
	ws.send(JSON.stringify({request : "set_displays", displays : displays}));
}

function update_displays() {
	let template = document.getElementById('display-');
	let parent   = template.parentNode;
	parent.replaceChildren(template);

	for (let i=0;i<displays.length;i++) {
		const d = displays[i];
		let n = template.cloneNode(true);
		let children = n.childNodes;
		n.id += i;
		n.hidden = false;

		let name = replace_child(children,"display-name-",i);
		name.textContent = d.name;

		let pos = replace_child(children,"display-pos-",i);
		pos.textContent = d.geo.x + "," + d.geo.y;

		let res = replace_child(children,"display-res-",i);
		res.value = d.geo.w + "x" + d.geo.h;
		res.addEventListener('change', (event) => {
			let disp_id = parseInt(event.target.id.match(/(\d+)$/)[0]);
			let d       = displays[disp_id];
			let val     = event.target.value;
			let wxh     = val.match(/(\d+)x(\d+)$/);
			let short   = val.match(/(\d+)p$/);
			if (wxh) {
				d.geo.w = parseInt(wxh[1]);
				d.geo.h = parseInt(wxh[2]);
			} else if (short) {
				let h   = parseInt(short[1]);
				let w   = h * 16;
				if (w%9 == 0) {
					d.geo.h = h;
					d.geo.w = Math.floor(w/9);
				}
			}
			res.value = d.geo.w + "x" + d.geo.h;
			draw_displays();
			set_displays();
		})

		let use = replace_child(children,"display-use-",i);
		use.checked = d.use;
		use.addEventListener('change', (event) => {
			let disp_id = parseInt(event.target.id.match(/(\d+)$/)[0]);
			let d = displays[disp_id];
			d.use = event.target.checked;
			set_displays();
		})

		parent.appendChild(n);
	} /* foreach display */

	draw_displays();
}

function displays_notification(msg) {
	if (ws)
		ws.send(JSON.stringify({request : "suggest_viewports", n_streams : stream_locations.length}));
	v = msg.payload
	if ((!v) || (v.length < 1)) {
		const toast = new bootstrap.Toast(document.getElementById('displaydetect_failed'));
		toast.show();
		displays = [{"name":"Default","use":true,"geo":{"x":0,"y":0,"w":window.screen.width,"h":window.screen.height}}];
		set_displays();
		return;
	}
	displays = v;
	update_displays();
}

function viewports_notification(msg) {
	v = msg.payload
	if ((!v) || (v.length < 1))
		v = [];
	viewports = v;
	draw_viewports();
}

function mpv_property_changed(property, stream_idx) {
	const property_map = { /* property -> node_name map */
		"media-title"            : "title",
		"demuxer-cache-duration" : "buffer",
		"mute"                   : "muting",
	};
	const update_funcs = { /* node_name -> update function map */
		"title"  : (n, v) => n.textContent = v,
		"buffer" : (n, v) => n.textContent = "Buffer: " + Math.round(v*10)/10 + "s",
		"volume" : (n, v) => n.value       = v,
		"muting" : (n, v) => n.checked     = !v,
	};

	let target = property_map[property.name];
	if (!target) {
		//console.log("no node name", stream_idx, target, property);
		target = property.name;
		//return;
	}

	node = stream_nodes[stream_idx][target];
	if (!node) {
		//console.log("no target node", stream_idx, target, property);
		return;
	}

	let val = property.data;
	if ((val == null) || (val == undefined))
		return;

	let update_func = update_funcs[target];
	if (!update_func) {
		console.log("no update func", stream_idx, target, property);
		return;
	}
	update_func(node,val);

}

function player_event(msg) {
	const event = msg.payload;

	if ((!event) || (event.length < 1))
		return;

	stream_idx = parseInt(msg.stream_idx);
	if (isNaN(stream_idx) || (stream_idx >= stream_nodes.length) || (stream_idx < 0))
		return;

	if (event.event == "property-change") {
		//console.log(stream_idx, event);
		mpv_property_changed(event, stream_idx);
	}
	//else
		//console.log(stream_idx, event, msg);
}

function player_status(msg) {
	const payload = msg.payload;

	if ((!payload) || (payload.length < 1))
		return;

	stream_idx = parseInt(msg.stream_idx);
	if (isNaN(stream_idx) || (stream_idx >= stream_nodes.length) || (stream_idx < 0))
		return;

	const status = payload.status;

	stream_nodes[stream_idx].volume.disabled              = status != "started";
	stream_nodes[stream_idx].buffer.disabled              = status != "started";
	stream_nodes[stream_idx].muting.disabled              = status != "started";
	stream_nodes[stream_idx].exclusive_unmute.disabled    = status != "started";
	stream_nodes[stream_idx].stop.disabled                = status == "stopped";
	stream_nodes[stream_idx].play.disabled                = status == "started";
	stream_nodes[stream_idx].ffwd.disabled                = status != "started";

	//console.log("player_status", stream_idx, status);
}

function streams_playing(active) {
	if (active == streams_active) return;
	streams_active = active;

	document.getElementById('setup-tab').disabled   = false;
	document.getElementById('control-tab').disabled = false;

	if (active)
		document.getElementById('control-tab').dispatchEvent(new Event("click"));
	else
		document.getElementById('setup-tab').dispatchEvent(new Event("click"));

	document.getElementById('setup-tab').disabled   =  active;
	document.getElementById('control-tab').disabled = !active;
}

function global_status(msg) {
	const status = msg.payload;

	if ((!status) || (status.length < 1))
		return;
	streams_playing(status.playing);
}

function mklink(ref) {
	const refs = {
		"mpv"        : "https://mpv.io/installation/",
		"yt-dlp"     : "https://github.com/yt-dlp/yt-dlp#installation",
		"streamlink" : "https://streamlink.github.io/install.html",
		"xrandr"     : "https://xorg-team.pages.debian.net/xorg/howto/use-xrandr.html",
	};
	return refs[ref] ? ('<a href="'+refs[ref]+'">'+ref+'</a>') : ref;
}

function commands_probed(msg) {
	const required_commands = {
		"mpv"        : true,
		"yt-dlp"     : true,
	};
	const results = msg.payload;
	if ((!results) || (results.length < 1))
		return;

	/* disable/enable use_streamlink switch */
	const use_streamlink = document.getElementById('use_streamlink');
	use_streamlink.disabled = results["streamlink"].exit_code != 0;
	const streamlink_note = document.getElementById('streamlink_note');
	streamlink_note.textContent = (results["streamlink"].exit_code != 0) ?
		"No working streamlink found." : "";

	/* walk through list to check mandatory & optional commands */
	let missing_required = "";
	let missing_optional = "";
	Object.keys(results).forEach(function(cmd, index) {
		const v = results[cmd];
		if (required_commands[cmd]) {
			if (v.exit_code != 0)
				missing_required += ", " + mklink(cmd);
		} else {
			if (v.exit_code != 0)
				missing_optional += ", " + mklink(cmd);
		}
		//console.log(cmd, v);
	});
	missing_required = missing_required.substring(2);
	missing_optional = missing_optional.substring(2);

	/* build status note */
	const cmd_missing_alert = document.getElementById('cmd_missing_alert');
	const alert = (message, type) => {
	  const wrapper = document.createElement('div');
	  wrapper.innerHTML = [
		`<div class="alert alert-${type}" role="alert">`,
		`   <div>${message}</div>`,
		'   <button type="button" class="btn btn-secondary btn-sm" id="commands_refresh"><i class="bi bi-arrow-clockwise"></i>&nbsp;Refresh</button>',
		'   <button type="button" class="btn btn-secondary btn-sm" id="commands_details" disabled><i class="bi bi-list-columns-reverse"></i>&nbsp;Details</button>',
		'</div>'
	  ].join('');
	  cmd_missing_alert.replaceChildren(wrapper);
	}
	let alert_type = 'success';
	let alert_msg = "All required &amp; optional commands found.";
	if (missing_required.length > 0) {
		alert_type = 'danger';
		alert_msg = "<b>Required commands failed: " + missing_required + "</b><br>";
	}
	if (missing_optional.length > 0) {
		if (alert_type == "success") {
			alert_type = 'warning';
			alert_msg = "Optional commands failed: " + missing_optional;
		}
		else
			alert_msg += "Optional commands failed: " + missing_optional;
	}
	alert(alert_msg, alert_type);

	/* button handlers for status note */
	const commands_refresh = document.getElementById('commands_refresh');
	commands_refresh.addEventListener('click', (event) => {
	  if(ws)
		ws.send(JSON.stringify({request : "probe_commands"}));
	})

	const commands_details = document.getElementById('commands_details');
	commands_details.addEventListener('click', (event) => {
	  console.log("TBD: command_details https://getbootstrap.com/docs/5.2/components/modal/#scrolling-long-content");
	})
}

function profiles_notification(msg) {
	const profiles = msg.payload;
	update_stream_profiles(profiles);
}

const ws_handlers = {
	"global_status"  : global_status,
	"probe_commands" : commands_probed,
	"profiles"       : profiles_notification,
	"displays"       : displays_notification,
	"viewports"      : viewports_notification,
	"player_event"   : player_event,
	"player_status"  : player_status,
};

function ws_rx(evt) {
	const msgs = evt.data.split('\n').map(s => s.length > 1 ? JSON.parse(s) : undefined);
	for (msg of msgs) {
		if (msg == undefined) continue;
		//console.log(msg);
		if (msg.notification && ws_handlers[msg.notification]) {
			ws_handlers[msg.notification](msg);
		}
		else {
			console.log(msg);
		}
	}
}

document.addEventListener("DOMContentLoaded", function() {
  displays[0].geo.w = window.screen.width;
  displays[0].geo.h = window.screen.height;
  update_displays();

  let websock = new WebSocket("ws://localhost:8090/ws");
  websock.onclose = ws_closed;
  websock.onmessage = ws_rx;
  
  websock.addEventListener('open', (event) => {
	ws = websock;
	ws.send(
		JSON.stringify({request : "global_status"})+
		JSON.stringify({request : "get_profiles"})+
		JSON.stringify({request : "probe_commands"})
		);
    document.getElementById('refresh_displays').dispatchEvent(new Event("click"));
    document.getElementById('stream_urls').dispatchEvent(new Event("input"));
    console.log("websock opened");
  });

  register_handlers();
  //window.setInterval(led_timer, refresh_delay);
});
