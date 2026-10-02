# The radio API: Python's standard library plus ffmpeg, which levels DJ interludes with the songs.
FROM python:3.13-alpine
RUN apk add --no-cache ffmpeg
