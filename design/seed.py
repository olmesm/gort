"""Populate a disposable PostgreSQL database for the design preview.
Run Gort once to create the schema, then pass its connection string here.
Never use this against a production database.
"""

import hashlib
import random
import sys
from datetime import datetime, timedelta, timezone

import psycopg

if len(sys.argv) != 2:
	raise SystemExit("Usage: uv run python design/seed.py POSTGRES_CONNECTION")
connection = sys.argv[1]
conn = psycopg.connect(connection)
if conn.execute("SELECT COUNT(*) FROM short_urls").fetchone()[0]:
	raise SystemExit("Database already contains links; refusing to overwrite it.")
rng = random.Random(41)
now = datetime.now(timezone.utc)
domain_id = conn.execute("SELECT id FROM domains WHERE is_default = TRUE").fetchone()[0]
for authority in ["links.northstar.example", "go.fieldwork.example", "read.commonplace.example"]:
	conn.execute(
		"INSERT INTO domains (authority, is_default, created_at) VALUES (%s, FALSE, %s)",
		(authority, now),
	)
tags = [
	"campaign",
	"editorial",
	"product",
	"social",
	"newsletter",
	"launch",
	"internal",
	"research",
]
for tag in tags:
	conn.execute("INSERT INTO tags (name) VALUES (%s)", (tag,))
tag_ids = [r[0] for r in conn.execute("SELECT id FROM tags ORDER BY id")]
links = [
	(
		"summer-notes",
		"Summer newsletter",
		"https://northstar.example/journal/summer-notes",
		"Studio",
		[1, 4],
	),
	(
		"new-perspectives",
		"New product collection",
		"https://fieldwork.example/collections/new-perspectives",
		"Commerce",
		[0, 2],
	),
	(
		"issue-08",
		"Commonplace issue 08",
		"https://commonplace.example/issues/08",
		"Editorial",
		[1, 4],
	),
	(
		"made-to-last",
		"Product materials guide",
		"https://fieldwork.example/stories/made-to-last",
		"Commerce",
		[0, 3],
	),
	(
		"open-studio",
		"Open studio event",
		"https://northstar.example/events/open-studio",
		"Studio",
		[3, 5],
	),
	("field-guide", "Field guide", "https://commonplace.example/field-guide", "Editorial", [1, 7]),
	("work-with-us", "Open positions", "https://northstar.example/careers", "Studio", [6]),
	(
		"autumn-preview",
		"Autumn collection preview",
		"https://fieldwork.example/autumn",
		"Commerce",
		[0, 5],
	),
	(
		"on-the-record",
		"Interview archive",
		"https://commonplace.example/interviews",
		"Editorial",
		[1, 3],
	),
	(
		"small-details",
		"Product specifications",
		"https://fieldwork.example/details",
		"Commerce",
		[2, 7],
	),
]
link_ids = []
for i in range(40):
	slug, title, target, group, assigned = links[i % len(links)]
	if i >= len(links):
		slug += f"-{i // len(links) + 1}"
		title += f" / Edition {i // len(links) + 1}"
	link_id = conn.execute(
		"INSERT INTO short_urls (short_code, domain_id, long_url, title, group_name, created_at, "
		"title_was_auto_resolved, redirect_status, forward_query, crawlable) "
		"VALUES (%s, %s, %s, %s, %s, %s, FALSE, 302, TRUE, FALSE) RETURNING id",
		(slug, domain_id, target, title, group, now - timedelta(hours=i * 13)),
	).fetchone()[0]
	link_ids.append(link_id)
	for index in assigned:
		conn.execute(
			"INSERT INTO short_url_tags (short_url_id, tag_id) VALUES (%s, %s)",
			(link_id, tag_ids[index]),
		)
for day in range(30):
	count = int(180 + day * 11 + rng.randint(0, 200))
	if day in [7, 15, 24, 28]:
		count += 220
	for j in range(count):
		when = (
			now.replace(hour=0, minute=0, second=0, microsecond=0)
			- timedelta(days=29 - day)
			+ timedelta(seconds=rng.randrange(86400))
		)
		# Keep today's events in the past.
		when = min(when, now)
		conn.execute(
			"INSERT INTO visits (short_url_id, visit_type, visited_at, referer, browser, os, device, is_bot) VALUES (%s, %s, %s, %s, %s, %s, %s, %s)",
			(
				rng.choice(link_ids[:10] if j % 3 else link_ids),
				"valid",
				when,
				rng.choice(
					[
						"https://instagram.com",
						"https://google.com",
						"https://newsletter.example",
						None,
					]
				),
				rng.choice(["Chrome", "Safari", "Firefox"]),
				rng.choice(["macOS", "iOS", "Windows", "Android"]),
				rng.choice(["desktop", "mobile"]),
				j % 43 == 0,
			),
		)
	for j in range(day % 7 + 1):
		conn.execute(
			"INSERT INTO visits (visit_type, visited_at, visited_url, is_bot) VALUES (%s, %s, %s, %s)",
			(
				"invalid_short_url",
				now - timedelta(days=29 - day),
				"https://go.gort.test/archived-" + str(j),
				j % 4 == 0,
			),
		)
for name, role in [
	("Production API", "admin"),
	("Editorial publishing", "author"),
	("Campaign reporting", "author"),
]:
	conn.execute(
		"INSERT INTO api_keys (key_hash, name, role, enabled, created_at) VALUES (%s, %s, %s, TRUE, %s)",
		(hashlib.sha256(name.encode()).hexdigest(), name, role, now - timedelta(days=12)),
	)
for name, events in [
	("Editorial notifications", "url.created"),
	("Campaign analytics", "visit.recorded"),
	("Unresolved link monitor", "orphan_visit.recorded"),
]:
	conn.execute(
		"INSERT INTO webhooks (name, url, secret, events, enabled, created_at) VALUES (%s, %s, %s, %s, FALSE, %s)",
		(name, "https://receiver.example/hooks/gort", "demo-only", events, now),
	)
password_hash = conn.execute("SELECT password_hash FROM users LIMIT 1").fetchone()[0]
for name, role in [("alex", "user"), ("jules", "user"), ("morgan", "admin")]:
	conn.execute(
		"INSERT INTO users (username, password_hash, role, created_at, auth_source) "
		"VALUES (%s, %s, %s, %s, 'local')",
		(name, password_hash, role, now - timedelta(days=10)),
	)
rule_id = conn.execute(
	"INSERT INTO redirect_rules (short_url_id, priority, long_url) VALUES (%s, 1, %s) RETURNING id",
	(link_ids[0], "https://northstar.example/mobile/summer"),
).fetchone()[0]
conn.execute(
	"INSERT INTO redirect_conditions (rule_id, cond_type, match_value) VALUES (%s, 'device', 'mobile')",
	(rule_id,),
)
conn.commit()
print(
	f"Seeded {len(link_ids)} links and {conn.execute('SELECT COUNT(*) FROM visits').fetchone()[0]} preview visits"
)
conn.close()
