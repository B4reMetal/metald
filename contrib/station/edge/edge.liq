# The station's public side: takes the stream from home (station.liq, Ogg Opus over WireGuard) and
# serves it as HLS for the listening page and as MP3 on Icecast for music apps.
#
# The page times lyrics against the audio it plays, not a clock. Beside the playlist this writes
# stamps.json: as each segment ends, and wherever a track starts, the track id and how far into the
# track that point is, given as an offset from the end of the last finished segment. The page knows
# where each segment sits on its own timeline, so segment end + offset is an exact track position
# whatever the latency. The stamps stay out of the audio:
# ID3 in the stream forced extra segment cuts, and hls.js surfaced the tags unreliably.
settings.log.stdout := true
settings.log.file := false
settings.log.level := 3
settings.harbor.bind_addrs := [environment.get(default="0.0.0.0", "HARBOR_BIND")]

hls_dir = environment.get(default="/hls", "HLS_DIR")

# max: room for home catching up after a stall (it sends what it owes at once) without dropping any.
home = input.harbor(environment.get(default="jpop", "HARBOR_MOUNT"), port=8005, max=60.,
  password=environment.get("HARBOR_PASSWORD"))
radio = mksafe(home)

track = ref("")
since = ref(0.)
# The last finished segment, and when it finished (Liquidsoap reports a segment once it's complete).
segment = ref("")
ended = ref(0.)
marks = ref([])

def add_mark(offset)
  if track() != "" and segment() != "" then
    m = {after=segment(), offset=offset, id=track(), pos=radio.time() - since()}
    l = list.append(marks(), [m])
    # 60 marks is about two minutes: more than the playlist keeps.
    marks := list.length(l) > 60 ? list.tl(l) : l
    file.write(data=json.stringify(marks()), hls_dir ^ "/stamps.json.tmp")
    ignore(file.move(hls_dir ^ "/stamps.json.tmp", hls_dir ^ "/stamps.json"))
  end
end

# After a reconnection the track playing is unknown until the next one starts; the page shows what's
# on air until then.
def mark(m)
  id = m["jpop_id"]
  if id != "" and id != track() then
    track := id
    since := radio.time()
    add_mark(radio.time() - ended())
    log.important(label="edge", "track " ^ id)
  end
end
radio.on_metadata(mark)

def segment_changed(~state, file)
  if state == "created" and string.contains(suffix=".ts", file) then
    segment := path.basename(file)
    ended := radio.time()
    add_mark(0.)
  end
end

output.file.hls(playlist="live.m3u8", segment_duration=2., segments=10, segments_overhead=5,
  on_file_change=segment_changed, hls_dir,
  [("aac", %ffmpeg(format="mpegts", %audio(codec="aac", b="256k", samplerate=44100)))], radio)

output.icecast(%mp3(bitrate=320, samplerate=44100), host=environment.get(default="127.0.0.1", "ICECAST_HOST"),
  port=int_of_string(environment.get(default="8100", "ICECAST_PORT")),
  password=environment.get("ICECAST_SOURCE_PASSWORD"), mount=environment.get(default="jpop", "ICECAST_MOUNT"),
  name=environment.get(default="station", "STATION_NAME"), public=false, radio)
