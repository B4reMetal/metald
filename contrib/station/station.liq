# The station's playout: the curator keeps the queue a few tracks ahead; this plays it with crossfades
# and sends it to the edge (edge/edge.liq) over the WireGuard link. Opus at 256k, not FLAC: the edge's
# harbor drops an Ogg FLAC stream at every track change.
settings.server.telnet := true
settings.server.telnet.bind_addr := "0.0.0.0"
settings.server.telnet.port := 1234
settings.log.stdout := true
settings.log.file := false
settings.log.level := 3
# jpop_id comes from the curator's annotate: on each request; the edge stamps the HLS segments with it.
settings.encoder.metadata.export := ["title", "artist", "album", "jpop_id"]

q = request.queue(id="q")

# Tell the curator what started (before the crossfade, which loses the filename).
def started(m)
  f = m["filename"]
  if f != "" then
    ignore(http.post(environment.get(default="http://curator:8080", "CURATOR_URL") ^ "/track",
                     headers=[("Content-Type", "application/json")],
                     data=json.stringify({filename=f})))
  end
end
q.on_track(started)

server.register("skip", fun (_) -> begin q.skip() "ok" end)

radio = mksafe(crossfade(duration=4., q))

output.icecast(%ogg(%opus(bitrate=256, samplerate=48000)), host=environment.get("EDGE_HOST"),
  port=int_of_string(environment.get(default="8005", "EDGE_PORT")), password=environment.get("EDGE_PASSWORD"),
  mount=environment.get(default="jpop", "EDGE_MOUNT"), radio)
