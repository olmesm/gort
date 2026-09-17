"""Installed commands and environment configuration."""

import subprocess
import sys
from pathlib import Path

import pytest

from goto import __version__
from goto.config import Settings


def test_environment_configures_server(monkeypatch):
	monkeypatch.setenv("GOTO_PORT", "8123")
	monkeypatch.setenv("GOTO_DB_CONNECTION", "postgresql://localhost/goto_config_test")
	monkeypatch.setenv("GOTO_INITIAL_ADMIN_USERNAME", "configured-admin")
	settings = Settings()
	assert settings.port == 8123
	assert settings.db_connection == "postgresql://localhost/goto_config_test"
	assert settings.initial_admin_username == "configured-admin"


@pytest.mark.parametrize("command", ["module", "console"])
def test_installed_command_reports_version_outside_checkout(tmp_path, command):
	args = (
		[sys.executable, "-m", "goto"]
		if command == "module"
		else [str(Path(sys.executable).with_name("goto"))]
	)
	result = subprocess.run(
		[*args, "--version"], cwd=tmp_path, capture_output=True, text=True, timeout=10
	)
	assert result.returncode == 0, result.stderr
	assert result.stdout.strip() == __version__
