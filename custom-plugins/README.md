# Custom plugins

Your own tools go here. Everything in this folder except this README is
git-ignored, so site-specific tools never end up in the repo. In Docker the
same role is played by the `/plugins` volume.

A plugin is any executable that answers two calls:

- `./tool.py --schema` prints a JSON tool definition: `title`, `description`,
  `properties`, `required`, plus the optional `sandbox` policy and `requires`,
  the environment variables it cannot work without. An enabled tool with a
  missing `requires` entry stops the bot at startup.
- `./tool.py --execute '<json args>'` runs it and prints the result.

List it in `config.yml` under `tool:` as `custom-plugins/tool.py` (or
`/plugins/tool.py` in Docker). Make it executable.

The shared `metald_tools` package is on `PYTHONPATH` for every tool, so a
custom plugin gets the same building blocks as the shipped ones:

```python
from metald_tools import toollog, safetyreview, urlguard
from metald_tools.hosting import upload_file, HostingError  # configured backend + UPLOAD_HEADERS
from metald_tools.media import strip_audio_metadata, strip_mp4_metadata
from metald_tools.promptrefine import refine_prompt
```

Keep secrets in `.env` (or `/config/.env` in Docker), never in the plugin.
