from datetime import UTC, datetime, timedelta
from types import SimpleNamespace

import pytest
from pydantic import ValidationError

from goto.domain import (
	ShortURLSpec,
	anonymize_ip,
	check_active,
	forward_query,
	ip_in_cidr,
	normalize_groups,
	resolve_target,
	validate_domain,
	validate_slug,
)


def test_creation_normalizes_values_and_checks_complete_lifetime():
	spec = ShortURLSpec(
		longUrl=" https://example.com/a ",
		customSlug="/docs/start/",
		tags=[" A ", "a", "B"],
		group=" /team/a ",
		title="  title  ",
	)
	assert (spec.long_url, spec.custom_slug, spec.tags, spec.group, spec.title) == (
		"https://example.com/a",
		"docs/start",
		["a", "b"],
		"team/a",
		"title",
	)
	for values in [
		{"maxVisits": 0},
		{"validSince": "2026-01-02", "validUntil": "2026-01-01"},
		{"redirectStatus": 303},
		{"customSlug": "graphql/nested"},
		{"longUrl": "file:///etc/passwd"},
	]:
		with pytest.raises(ValidationError):
			ShortURLSpec.model_validate({"longUrl": "https://example.com", **values})


def test_query_forwarding_preserves_duplicates_blanks_fragment_and_percent_encoding():
	assert (
		forward_query("https://example.com?a=0#part", [("a", "1"), ("q", "a b&c"), ("blank", "")])
		== "https://example.com?a=0&a=1&q=a%20b%26c&blank#part"
	)


def test_rule_priority_conditions_language_and_mobile():
	rules = [
		{
			"priority": 2,
			"longUrl": "https://second",
			"conditions": [{"type": "device", "matchValue": "mobile"}],
		},
		{
			"priority": 1,
			"longUrl": "https://first",
			"conditions": [
				{"type": "device", "matchValue": "ios"},
				{"type": "language", "matchValue": "en"},
				{"type": "query-param", "matchKey": "a", "matchValue": "yes"},
			],
		},
	]
	assert (
		resolve_target("https://default", rules, "iPhone", "en-GB;q=0.9", {"a": "yes"})
		== "https://first"
	)
	assert resolve_target("https://default", rules, "Android") == "https://second"
	assert resolve_target("https://default", rules, "Firefox") == "https://default"


def test_lifetime_includes_end_instant_and_enforces_visit_limit():
	now = datetime.now(UTC)
	link = SimpleNamespace(valid_since=now, valid_until=now, max_visits=2)
	assert check_active(link, now, 1) is None
	assert check_active(link, now, 2) == "max_visits_reached"
	assert check_active(link, now + timedelta(microseconds=1), 0) == "no_longer_valid"


def test_ip_privacy_and_family_matching():
	assert anonymize_ip("192.168.1.42") == "192.168.1.0"
	assert anonymize_ip("2001:db8:1:2:3:4:5:6") == "2001:db8:1::"
	assert anonymize_ip("::ffff:192.168.1.42") == "192.168.1.0"
	assert anonymize_ip("invalid") == ""
	assert ip_in_cidr("10.0.0.0/8", "10.2.3.4")
	assert not ip_in_cidr("10.0.0.0/8", "2001:db8::1")


def test_group_authority_slug_normalization():
	assert normalize_groups(["/team", "team", "", "/nested/team"]) == ["team", "nested/team"]
	assert validate_domain(" EXAMPLE.com:8443 ") == "example.com:8443"
	for value in (
		"https://example.com",
		"example.com/path",
		"example.com?query",
		"user@example.com",
	):
		with pytest.raises(ValueError):
			validate_domain(value)
	with pytest.raises(ValueError):
		validate_slug("a//b")


@pytest.mark.parametrize("raw", [" 3", "3 ", "1_0", "１２", "9223372036854775808"])
def test_paging_rejects_invalid_integer_syntax_and_overflow(raw):
	from goto.services import integer

	assert integer(raw, 20) == 20


@pytest.mark.parametrize(
	"raw", ["20260101", "2026-W01-1", "2026-01-01X12:00:00", "2026-01-01T12:00Z"]
)
def test_filter_dates_reject_python_only_iso_formats(raw):
	from goto.services import _filter_date

	assert _filter_date(raw) is None
