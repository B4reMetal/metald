# The station's curator: Python plus mutagen for reading tags.
FROM python:3.13-alpine
RUN pip install --no-cache-dir mutagen==1.47.0
COPY curator.py /app/curator.py
CMD ["python3", "-u", "/app/curator.py"]
