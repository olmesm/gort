"""Run with `uv run gort` or `python -m gort`."""

import argparse
import logging
import urllib.request

from gort import __version__
from gort.config import Settings


def main():
	parser = argparse.ArgumentParser(description="Gort URL shortener")
	parser.add_argument("--version", "-version", action="version", version=__version__)
	parser.add_argument("--healthcheck", "-healthcheck", action="store_true")
	options = parser.parse_args()
	settings = Settings()
	if options.healthcheck:
		with urllib.request.urlopen(
			f"http://127.0.0.1:{settings.port}/rest/health", timeout=5
		) as response:
			raise SystemExit(0 if response.status == 200 else 1)
	logging.basicConfig(level=logging.INFO, format="%(levelname)s %(name)s %(message)s")
	import uvicorn

	from gort.app import create_app

	uvicorn.run(create_app(settings), host="0.0.0.0", port=settings.port, proxy_headers=False)


if __name__ == "__main__":
	main()
