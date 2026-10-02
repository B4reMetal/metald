settings.server.telnet := true
settings.server.telnet.bind_addr := "0.0.0.0"
settings.server.telnet.port := 1234
settings.log.stdout := true
settings.log.file := false
settings.log.level := 3

q = request.queue(id="q")
# Only queued tracks play; with the queue empty the stream is silent.
player = q

# Read before the crossfade: the track that comes out of it has lost its filename.
now_file = ref("")
now_started = ref(0.)
def track_started(m) =
  if m["filename"] != "" then
    now_file := m["filename"]
    now_started := time()
  end
end
player.on_track(track_started)

radio = mksafe(crossfade(duration=3., player))

# HLS for the listening page, with stamps.json beside it: as each segment ends, and wherever a track
# starts, the track (its library file name) and how far into it that point is, as an offset from the
# end of the last finished segment. The page places segments on its own timeline, so it times lyrics
# from the audio it plays rather than from a clock. /live on Icecast stays for players and apps.
hls_dir = "/hls"
track = ref("")
since = ref(0.)
segment = ref("")
ended = ref(0.)
marks = ref([])

# An empty id marks silence: the queue ran out.
def add_mark(offset) =
  if segment() != "" then
    m = {after=segment(), offset=offset, id=track(), pos=radio.time() - since()}
    l = list.append(marks(), [m])
    # 60 marks is about two minutes: more than the playlist keeps.
    marks := list.length(l) > 60 ? list.tl(l) : l
    file.write(data=json.stringify(marks()), hls_dir ^ "/stamps.json.tmp")
    ignore(file.move(hls_dir ^ "/stamps.json.tmp", hls_dir ^ "/stamps.json"))
  end
end

# After the crossfade, so a track counts from where its fade-in starts on the stream.
radio.on_metadata(fun (m) -> begin
  name = path.basename(m["filename"])
  if name != "" and name != track() then
    track := name
    since := radio.time()
    add_mark(radio.time() - ended())
  end
end)

def segment_changed(~state, file) =
  # Liquidsoap reports a segment once it's complete.
  if state == "created" and string.contains(suffix=".ts", file) then
    segment := path.basename(file)
    ended := radio.time()
    if track() != "" then add_mark(0.) end
  end
end

thread.run(every=1., fun () -> begin
  if not player.is_ready() and track() != "" then
    track := ""
    add_mark(radio.time() - ended())
  end
end)

output.file.hls(playlist="live.m3u8", segment_duration=2., segments=10, segments_overhead=5,
  on_file_change=segment_changed, hls_dir,
  [("aac", %ffmpeg(format="mpegts", %audio(codec="aac", b="192k", samplerate=44100)))], radio)

# "now" answers with the playing file and when it started (unix seconds), one per line.
server.register("now", fun (_) -> if player.is_ready() then "#{now_file()}\n#{now_started()}" else "" end)
# URIs, not filenames: a request has no filename until it is prepared, as after the queue is edited.
server.register("pending", fun (_) -> string.concat(separator="\n", list.map(request.uri, q.queue())))
# Queue editing for the admin page. Positions count from 0, soonest first; out of range does nothing.
# set_queue discards the requests it replaces, so the new queue is built from fresh ones with the same
# URIs (annotations included).
def fresh(r) =
  request.create(request.uri(r))
end

def queue_remove(arg) =
  qs = q.queue()
  i = int_of_string(default=-1, arg)
  n = list.length(qs)
  if i >= 0 and i < n then
    q.set_queue(list.init(n - 1, fun (k) -> fresh(list.nth(qs, if k < i then k else k + 1 end))))
    "ok"
  else
    "no such position"
  end
end
server.register("remove", queue_remove)

def queue_swap(arg) =
  qs = q.queue()
  parts = string.split(separator=" ", arg)
  i = int_of_string(default=-1, list.nth(default="", parts, 0))
  j = int_of_string(default=-1, list.nth(default="", parts, 1))
  n = list.length(qs)
  if i >= 0 and j >= 0 and i < n and j < n then
    q.set_queue(list.init(n, fun (k) -> fresh(list.nth(qs, if k == i then j elsif k == j then i else k end))))
    "ok"
  else
    "no such position"
  end
end
server.register("swap", queue_swap)

server.register("skip", fun (_) -> begin player.skip() "ok" end)

output.icecast(%mp3(bitrate=128), host="icecast", port=8000,
  password=environment.get("ICECAST_SOURCE_PASSWORD"), mount="live",
  name=environment.get(default="radio", "RADIO_NAME"), radio)
