"""Server-rendered dashboard and OIDC authorization-code login."""

from __future__ import annotations

import hashlib
import math
import re
import secrets
import time
from datetime import UTC, date, datetime, timedelta
from pathlib import Path
from urllib.parse import quote, urlencode

import httpx
from authlib.jose import JsonWebToken
from fastapi import APIRouter, HTTPException, Request
from jinja2 import Environment, FileSystemLoader, select_autoescape
from sqlalchemy import delete, func
from sqlalchemy.exc import IntegrityError
from sqlmodel import Session, select
from starlette.concurrency import run_in_threadpool
from starlette.responses import HTMLResponse, RedirectResponse

from . import auth, services
from .models import APIKey, Domain, ShortURL, ShortURLTag, Tag, User, Visit, Webhook

router = APIRouter(include_in_schema=False)
env = Environment(
	loader=FileSystemLoader(Path(__file__).parent / "templates"),
	autoescape=select_autoescape(["html"]),
)
env.globals.update(
	active=lambda path, href: path == href or (href != "/admin" and path.startswith(href + "/")),
	fmtDate=lambda value: format_date(value),
	fmtCount=lambda n: f"{n:,}",
	deref=lambda s: s or "",
	orDash=lambda s: s if s is not None else "—",
	csv=lambda s: [v.strip() for v in s.split(",")],
)


def render(name, data=None, status=200):
	return HTMLResponse(
		env.get_template(name + ".html").render(d=data or {}, root=data or {}), status_code=status
	)


def page(request, user, name, title, data, status=200):
	return render(
		name,
		{
			"Title": title,
			"Path": request.url.path,
			"User": {"ID": user.id, "Username": user.username, "IsAdmin": user.is_admin},
			"WebhooksEnabled": request.app.state.settings.webhooks_enabled,
			"Data": data,
		},
		status,
	)


def redirect(url):
	return RedirectResponse(url, status_code=302)


def format_date(value):
	if not value:
		return "—"
	if isinstance(value, str):
		value = datetime.fromisoformat(value.replace("Z", "+00:00"))
	return value.strftime("%Y-%m-%d %H:%M")


def date_value(value):
	return value.strftime("%Y-%m-%dT%H:%M") if value else ""


def parse_date(value):
	if not value:
		return None
	try:
		parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
		return parsed.astimezone(UTC) if parsed.tzinfo else parsed.replace(tzinfo=UTC)
	except ValueError:
		return None


def integer(value, default=1):
	try:
		return int(value)
	except (TypeError, ValueError):
		return default


def model_view(model):
	special = {
		"id": "ID",
		"url": "URL",
		"long_url": "LongURL",
		"short_code": "ShortCode",
		"is_default": "IsDefault",
		"base_url_redirect": "BaseURLRedirect",
		"regular_404_redirect": "Regular404Redirect",
		"invalid_short_url_redirect": "InvalidShortURLRedirect",
	}
	return {
		special.get(k, "".join(x.title() for x in k.split("_"))): v
		for k, v in model.model_dump().items()
	}


def pager(request, count, current=None):
	current = max(1, current or integer(request.query_params.get("page")))
	pages = max(1, math.ceil(count / 25))
	current = min(current, pages)

	def url(number):
		params = dict(request.query_params)
		params.pop("page", None)
		if number > 1:
			params["page"] = number
		return request.url.path + ("?" + urlencode(sorted(params.items())) if params else "")

	return {
		"CurrentPage": current,
		"TotalPages": pages,
		"TotalItems": count,
		"PrevURL": url(current - 1) if current > 1 else "",
		"NextURL": url(current + 1) if current < pages else "",
	}


def paginate(request, rows):
	current = min(
		max(1, integer(request.query_params.get("page"))), max(1, math.ceil(len(rows) / 25))
	)
	return rows[(current - 1) * 25 : current * 25], pager(request, len(rows), current)


def options(values, current=""):
	return [{"Value": v, "Selected": v == current} for v in values]


def filters(request, placeholder, selects=()):
	return {
		"BasePath": request.url.path,
		"Search": request.query_params.get("search", ""),
		"Placeholder": placeholder,
		"Selects": [
			{
				"Name": name,
				"Label": label,
				"Options": options(values, request.query_params.get(name, "")),
			}
			for name, label, values in selects
		],
	}


def login_page(request, error="", return_url="/admin", status=200):
	cfg = request.app.state.settings
	return render(
		"login",
		{
			"Error": error,
			"ReturnURL": return_url,
			"ReturnURLParam": quote(return_url, safe=""),
			"OIDCEnabled": cfg.oidc_enabled,
			"ShowPasswordForm": not cfg.oidc_enabled or not cfg.oidc_only,
			"ProviderName": cfg.oidc_provider_name,
		},
		status,
	)


@router.api_route("/admin/login", methods=["GET", "POST"])
async def login(request: Request):
	form = await request.form() if request.method == "POST" else {}
	return await run_in_threadpool(login_response, request, form)


def login_response(request, form):
	with Session(request.app.state.engine) as session:
		if request.method == "GET":
			target = auth.safe_return_url(request.query_params.get("returnUrl", "/admin"))
			return (
				redirect(target)
				if auth.current_user(request, session)
				else login_page(request, return_url=target)
			)
		cfg = request.app.state.settings
		if cfg.oidc_enabled and cfg.oidc_only:
			return login_page(
				request, "Password login is disabled; use single sign-on.", status=403
			)
		target = auth.safe_return_url(form.get("returnUrl", ""))
		user = session.exec(
			select(User).where(User.username == form.get("username", "").strip())
		).first()
		if (
			user
			and user.auth_source == "local"
			and auth.verify_password(form.get("password", ""), user.password_hash)
		):
			response = redirect(target)
			auth.sign_in(request, response, user)
			return response
		return login_page(request, "Invalid username or password.", target, 401)


@router.post("/admin/logout")
def logout(request: Request):
	response = redirect("/admin/login")
	auth.sign_out(request, response)
	return response


async def discover(request):
	cfg = request.app.state.settings
	async with httpx.AsyncClient(timeout=15) as client:
		response = await client.get(
			cfg.oidc_issuer.rstrip("/") + "/.well-known/openid-configuration"
		)
		response.raise_for_status()
		metadata = response.json()
	if metadata.get("issuer") != cfg.oidc_issuer:
		raise ValueError("OIDC discovery issuer mismatch")
	return metadata


def oidc_callback_url(request):
	return (
		request.app.state.settings.oidc_redirect_url
		or f"{getattr(request.state, 'scheme', request.url.scheme)}://{request.url.netloc}/admin/oidc/callback"
	)


@router.get("/admin/oidc/login")
async def oidc_login(request: Request):
	cfg = request.app.state.settings
	if not cfg.oidc_enabled:
		raise HTTPException(404)
	try:
		metadata = await discover(request)
	except (httpx.HTTPError, ValueError):
		return login_page(
			request,
			"Single sign-on is unavailable: the identity provider could not be reached.",
			status=502,
		)
	state = {
		"s": secrets.token_urlsafe(24),
		"n": secrets.token_urlsafe(24),
		"v": secrets.token_urlsafe(32),
		"r": auth.safe_return_url(request.query_params.get("returnUrl", "")),
		"exp": int(time.time()) + 600,
	}
	params = {
		"client_id": cfg.oidc_client_id,
		"redirect_uri": oidc_callback_url(request),
		"response_type": "code",
		"scope": "openid " + cfg.oidc_scopes.replace(",", " "),
		"state": state["s"],
		"nonce": state["n"],
		"code_challenge": auth.encode(hashlib.sha256(state["v"].encode()).digest()),
		"code_challenge_method": "S256",
	}
	response = redirect(
		metadata["authorization_endpoint"] + "?" + urlencode(sorted(params.items()))
	)
	response.set_cookie(
		"gort_oidc",
		auth.sign_payload(auth.session_key(request), state),
		max_age=600,
		path="/admin/oidc",
		httponly=True,
		secure=cfg.use_https,
		samesite="lax",
	)
	return response


@router.get("/admin/oidc/callback")
async def oidc_callback(request: Request):
	cfg = request.app.state.settings
	if not cfg.oidc_enabled:
		raise HTTPException(404)
	state = auth.verify_payload(auth.session_key(request), request.cookies.get("gort_oidc", ""))

	def finish(response):
		response.delete_cookie(
			"gort_oidc", path="/admin/oidc", httponly=True, secure=cfg.use_https, samesite="lax"
		)
		return response

	if (
		not state
		or state.get("exp", 0) <= time.time()
		or not secrets.compare_digest(
			str(state.get("s", "")), request.query_params.get("state", "")
		)
	):
		return finish(
			login_page(
				request,
				"Sign-on failed: the login attempt expired or was tampered with. Try again.",
				status=401,
			)
		)
	if request.query_params.get("error"):
		return finish(
			login_page(
				request, "Sign-on failed: the identity provider rejected the login.", status=401
			)
		)
	try:
		metadata = await discover(request)
		async with httpx.AsyncClient(timeout=15) as client:
			data = {
				"grant_type": "authorization_code",
				"code": request.query_params.get("code", ""),
				"redirect_uri": oidc_callback_url(request),
				"code_verifier": state["v"],
				"client_id": cfg.oidc_client_id,
			}
			methods = metadata.get("token_endpoint_auth_methods_supported", ["client_secret_basic"])
			credentials = None
			if cfg.oidc_client_secret:
				if "client_secret_basic" in methods:
					credentials = (cfg.oidc_client_id, cfg.oidc_client_secret)
				else:
					data["client_secret"] = cfg.oidc_client_secret
			token = await client.post(metadata["token_endpoint"], data=data, auth=credentials)
			token.raise_for_status()
			keys = await client.get(metadata["jwks_uri"])
			keys.raise_for_status()
		claims = JsonWebToken(
			["RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "EdDSA"]
		).decode(
			token.json()["id_token"],
			keys.json(),
			claims_options={
				"iss": {"essential": True, "value": cfg.oidc_issuer},
				"aud": {"essential": True, "value": cfg.oidc_client_id},
				"exp": {"essential": True},
				"sub": {"essential": True},
				"iat": {"essential": True},
			},
		)
		claims.validate(leeway=0)
		if (
			claims.get("nonce") != state["n"]
			or not isinstance(claims["sub"], str)
			or not claims["sub"]
		):
			raise ValueError("Invalid token identity or nonce")
		if (
			isinstance(claims.get("aud"), list)
			and len(claims["aud"]) > 1
			and claims.get("azp") != cfg.oidc_client_id
		):
			raise ValueError("Invalid authorized party")
		username = next(
			(
				claims[x].strip()
				for x in ["preferred_username", "email", "sub"]
				if isinstance(claims.get(x), str) and claims[x].strip()
			),
			claims["sub"],
		)
		raw_groups = claims.get(cfg.oidc_groups_claim, [])
		groups = (
			auth.normalize_groups([g for g in raw_groups if isinstance(g, str)])
			if isinstance(raw_groups, list)
			else []
		)
		role = "admin" if auth.normalize_group(cfg.oidc_admin_group) in groups else "user"
		with Session(request.app.state.engine) as session:
			user = session.exec(select(User).where(User.oidc_subject == claims["sub"])).first()
			collision = session.exec(select(User).where(User.username == username)).first()
			if user:
				if not collision or collision.id == user.id:
					user.username = username
				user.role = role
			else:
				if collision:
					username += "-" + claims["sub"][:8]
				user = User(
					username=username,
					password_hash="!oidc",
					auth_source="oidc",
					oidc_subject=claims["sub"],
					role=role,
				)
			session.add(user)
			session.commit()
			session.refresh(user)
			response = redirect(auth.safe_return_url(state["r"]))
			auth.sign_in(request, response, user, groups, int(claims["exp"]))
			return finish(response)
	except (httpx.HTTPError, ValueError, KeyError, IntegrityError):
		return finish(
			login_page(request, "Sign-on failed: the ID token could not be verified.", status=401)
		)
	except Exception as exc:
		from authlib.jose.errors import JoseError

		if isinstance(exc, JoseError):
			return finish(
				login_page(
					request, "Sign-on failed: the ID token could not be verified.", status=401
				)
			)
		raise


def visible_link(session, user, link_id):
	link = session.get(ShortURL, link_id)
	if not link or not user.can_see_group(link.group_name):
		raise HTTPException(404)
	return link


def status_options(status):
	labels = {
		301: "301 Permanent",
		302: "302 Found, default",
		307: "307 Temporary, keep method",
		308: "308 Permanent, keep method",
	}
	return [{"Code": k, "Label": v, "Selected": k == status} for k, v in labels.items()]


def group_picker(user, current=""):
	return {"IsAdmin": user.is_admin, "Current": current, "Options": user.groups}


def link_form(form):
	return {
		"longUrl": form.get("longUrl", ""),
		"customSlug": form.get("customSlug") or None,
		"domain": form.get("domain") or None,
		"title": form.get("title") or None,
		"tags": [t.strip() for t in form.get("tags", "").split(",") if t.strip()],
		"group": form.get("group") or None,
		"validSince": parse_date(form.get("validSince")),
		"validUntil": parse_date(form.get("validUntil")),
		"maxVisits": integer(form.get("maxVisits"), None),
		"redirectStatus": integer(form.get("redirectStatus"), 302),
		"forwardQuery": form.get("forwardQuery") == "true",
		"crawlable": form.get("crawlable") == "true",
	}


def chart(counts):
	if not counts:
		return {}
	start = min(counts)
	end = max(max(counts), start + timedelta(days=6))
	days = (end - start).days + 1
	maximum = max(counts.values())
	points = [
		{
			"X": 40 + 1120 * i / (days - 1),
			"Y": 178 - 168 * counts.get(start + timedelta(days=i), 0) / maximum,
			"Date": str(start + timedelta(days=i)),
			"Count": counts.get(start + timedelta(days=i), 0),
		}
		for i in range(days)
	]
	line = " ".join(f"{'M' if i == 0 else 'L'}{p['X']},{p['Y']}" for i, p in enumerate(points))
	return {
		"Points": points,
		"LinePath": line,
		"AreaPath": line + " L1160,178 L40,178 Z",
		"Grid": [{"Y": 178 - 168 * f, "Text": int(maximum * f)} for f in [0, 0.5, 1]],
		"Labels": [
			{"X": p["X"], "Y": 194, "Text": p["Date"][5:]}
			for i, p in enumerate(points)
			if i % max(1, days // 8) == 0 or i == days - 1
		],
	}


def visit_day_counts(session, predicates):
	"""Fetch one aggregate per UTC day, never the underlying visit records."""
	day = func.to_char(func.timezone("UTC", Visit.visited_at), "YYYY-MM-DD")
	rows = session.exec(
		select(day, func.count()).select_from(Visit).where(*predicates).group_by(day).order_by(day)
	).all()
	return {date.fromisoformat(value): count for value, count in rows}


def chart_start():
	return datetime.now(UTC).replace(hour=0, minute=0, second=0, microsecond=0) - timedelta(days=29)


def analytics(request, session, short_url_id=None, orphan=False):
	predicates = (
		[Visit.visit_type != "valid"]
		if orphan
		else [Visit.visit_type == "valid", Visit.short_url_id == short_url_id]
	)
	if orphan and request.query_params.get("type") in (
		"base_url",
		"regular_404",
		"invalid_short_url",
	):
		predicates.append(Visit.visit_type == request.query_params["type"])
	start = parse_date(request.query_params.get("startDate"))
	end = parse_date(request.query_params.get("endDate"))
	if start:
		predicates.append(Visit.visited_at >= start)
	if end:
		predicates.append(Visit.visited_at <= end)
	total = session.exec(select(func.count()).select_from(Visit).where(*predicates)).one()
	pagination = pager(request, total)
	current = session.exec(
		select(Visit)
		.where(*predicates)
		.order_by(Visit.visited_at.desc(), Visit.id.desc())
		.limit(25)
		.offset((pagination["CurrentPage"] - 1) * 25)
	).all()
	cards = []
	for title, column in [
		("Browsers", Visit.browser),
		("Operating systems", Visit.os),
		("Referrers", Visit.referer),
	]:
		label = func.coalesce(func.nullif(column, ""), "Unknown")
		count = func.count()
		values = session.exec(
			select(label, count)
			.select_from(Visit)
			.where(*predicates)
			.group_by(label)
			.order_by(count.desc(), label)
			.limit(8)
		).all()
		cards.append(
			{
				"Title": title,
				"Rows": [
					{"Label": value, "Count": count, "Pct": count / values[0][1] * 100}
					for value, count in values
				],
			}
		)
	series = visit_day_counts(session, [*predicates, Visit.visited_at >= (start or chart_start())])
	return {
		"BasePath": request.url.path,
		"StartVal": request.query_params.get("startDate", ""),
		"EndVal": request.query_params.get("endDate", ""),
		"ShowVisitedURL": orphan,
		"Chart": chart(series),
		"Breakdowns": cards,
		"Pager": pagination,
		"Visits": [
			{
				"When": format_date(v.visited_at),
				"VisitedURL": v.visited_url or "—",
				"BrowserOS": " / ".join(x for x in [v.browser, v.os] if x) or "—",
				"Device": v.device or "—",
				"Referer": v.referer,
				"IsBot": v.is_bot,
			}
			for v in current
		],
	}


def short_urls_page(request, session, user):
	query = dict(request.query_params)
	query.update(searchTerm=query.get("search", ""), itemsPerPage=25)
	query["orderBy"] = {query.get("orderBy", "dateCreated"): query.get("dir", "desc")}
	if query.get("tag"):
		query["tags"] = [query["tag"]]
	if not query.get("group"):
		query.pop("group", None)
	result = services.list_short_urls(
		session, request.app.state.settings, query, groups=user.visible_groups()
	)

	def head(field, label):
		params = dict(request.query_params)
		direction = (
			"asc"
			if params.get("orderBy", "dateCreated") == field and params.get("dir", "desc") == "desc"
			else "desc"
		)
		params.update(orderBy=field, dir=direction)
		params.pop("page", None)
		marker = (
			(" ↑" if request.query_params.get("dir", "desc") == "asc" else " ↓")
			if request.query_params.get("orderBy", "dateCreated") == field
			else ""
		)
		return {
			"URL": "/admin/short-urls?" + urlencode(sorted(params.items())),
			"Label": label + marker,
		}

	table = {
		"HeadShortCode": head("shortCode", "Short URL"),
		"HeadTitle": head("title", "Title"),
		"HeadLongURL": head("longUrl", "Long URL"),
		"HeadVisits": head("visits", "Visits"),
		"HeadCreated": head("dateCreated", "Created"),
		"Pager": pager(request, result["pagination"]["totalItems"]),
		"Rows": [],
	}
	for item in result["data"]:
		link = services.find_short_url(
			session, request.app.state.settings, item["shortCode"], item["domain"]
		)
		table["Rows"].append(
			{
				"ShortURL": item["shortUrl"],
				"Display": item["shortUrl"].split("://", 1)[-1],
				"Title": item.get("title") or "—",
				"LongURL": item["longUrl"],
				"Tags": item["tags"],
				"Group": item.get("group"),
				"VisitsURL": f"/admin/short-urls/{link.id}/visits",
				"VisitsLabel": item["visitsSummary"]["total"],
				"Created": format_date(item["dateCreated"]),
				"EditURL": f"/admin/short-urls/{link.id}/edit",
			}
		)
	if request.headers.get("HX-Request") == "true":
		return render("su-table", table)
	tags = session.exec(select(Tag.name).order_by(Tag.name)).all()
	groups = (
		session.exec(
			select(ShortURL.group_name).where(ShortURL.group_name.is_not(None)).distinct()
		).all()
		if user.is_admin
		else user.groups
	)
	return page(
		request,
		user,
		"shorturls",
		"Short URLs",
		{
			"Search": query.get("search", ""),
			"TagOptions": options(tags, query.get("tag", "")),
			"GroupOptions": options(groups, query.get("group", "")),
			"Table": table,
		},
	)


def create_page(request, user, form=None, error="", status=200):
	form = form or {"forwardQuery": "true", "redirectStatus": "302"}
	values = {
		k: form.get(v, "")
		for k, v in [
			("LongURL", "longUrl"),
			("CustomSlug", "customSlug"),
			("Domain", "domain"),
			("Title", "title"),
			("Tags", "tags"),
			("Group", "group"),
			("ValidSince", "validSince"),
			("ValidUntil", "validUntil"),
			("MaxVisits", "maxVisits"),
		]
	}
	values.update(
		ForwardQuery=form.get("forwardQuery") == "true", Crawlable=form.get("crawlable") == "true"
	)
	return page(
		request,
		user,
		"shorturl_new",
		"New short URL",
		{
			"Form": values,
			"Error": error,
			"DefaultDomain": request.app.state.settings.default_domain,
			"Group": group_picker(user, form.get("group", "")),
			"StatusOptions": status_options(integer(form.get("redirectStatus"), 302)),
		},
		status,
	)


def condition_label(condition):
	kind = condition["type"]
	value = condition["matchValue"]
	return {
		"device": f"Device is {value}",
		"language": f"Language matches {value}",
		"query-param": f"Query param {condition.get('matchKey', '')} = {value}",
		"ip-address": f"IP in {value}",
	}.get(kind, kind)


def edit_page(request, session, user, link, error="", status=200):
	cfg = request.app.state.settings
	dto = services.short_url_dto(session, cfg, link)
	base = f"/admin/short-urls/{link.id}"
	rules = services.get_rules(session, link)["redirectRules"]
	search = request.query_params.get("search", "").strip().lower()
	rules = [r for r in rules if search in r["longUrl"].lower()]
	rules, pagination = paginate(request, rules)
	return page(
		request,
		user,
		"shorturl_edit",
		"Edit short URL",
		{
			"Error": error,
			"ShortURL": dto["shortUrl"],
			"QRURL": f"/{link.short_code}/qr-code?size=300",
			"VisitsURL": base + "/visits",
			"VisitCount": dto["visitsSummary"]["total"],
			"EditAction": base + "/edit",
			"LongURL": link.long_url,
			"Title": link.title or "",
			"Tags": ", ".join(dto["tags"]),
			"Group": group_picker(user, link.group_name or ""),
			"ValidSince": date_value(link.valid_since),
			"ValidUntil": date_value(link.valid_until),
			"MaxVisits": link.max_visits or "",
			"StatusOptions": status_options(link.redirect_status),
			"ForwardQuery": link.forward_query,
			"Crawlable": link.crawlable,
			"RuleFilters": filters(request, "Search target URL…"),
			"RulePager": pagination,
			"Rules": [
				{
					"Priority": r["priority"],
					"LongURL": r["longUrl"],
					"Conditions": [condition_label(c) for c in r["conditions"]],
				}
				for r in rules
			],
			"RuleAddAction": base + "/rules/add",
			"RuleDeleteAction": base + "/rules/delete",
			"DeleteAction": base + "/delete",
			"VisitsDeleteAction": base + "/visits/delete",
		},
		status,
	)


def overview_page(request, session, user):
	if not user.is_admin:
		return redirect("/admin/short-urls")
	links = session.exec(
		select(ShortURL).order_by(ShortURL.created_at.desc(), ShortURL.id.desc()).limit(5)
	).all()
	counts = (
		dict(
			session.exec(
				select(Visit.short_url_id, func.count())
				.where(
					Visit.visit_type == "valid", Visit.short_url_id.in_([link.id for link in links])
				)
				.group_by(Visit.short_url_id)
			).all()
		)
		if links
		else {}
	)
	recent = []
	for link in links:
		row = model_view(link)
		row["Authority"] = session.get(Domain, link.domain_id).authority
		row["VisitCount"] = counts.get(link.id, 0)
		recent.append(row)
	stats = {
		"ShortURLCount": session.exec(select(func.count()).select_from(ShortURL)).one(),
		"VisitCount": session.exec(
			select(func.count()).select_from(Visit).where(Visit.visit_type == "valid")
		).one(),
		"OrphanVisitCount": session.exec(
			select(func.count()).select_from(Visit).where(Visit.visit_type != "valid")
		).one(),
		"TagCount": session.exec(select(func.count()).select_from(Tag)).one(),
		"BotVisitCount": session.exec(
			select(func.count())
			.select_from(Visit)
			.where(Visit.visit_type == "valid", Visit.is_bot.is_(True))
		).one(),
	}
	return page(
		request,
		user,
		"overview",
		"Overview",
		{
			"Stats": stats,
			"Chart": chart(
				visit_day_counts(
					session, [Visit.visit_type == "valid", Visit.visited_at >= chart_start()]
				)
			),
			"Recent": recent,
		},
	)


def admin_rows(request, session, kind):
	models = {"users": User, "api-keys": APIKey, "domains": Domain, "webhooks": Webhook}
	ordering = {
		"users": [User.username, User.id],
		"domains": [Domain.is_default.desc(), Domain.authority, Domain.id],
		"webhooks": [Webhook.name, Webhook.id],
		"api-keys": [APIKey.created_at.desc(), APIKey.id.desc()],
	}
	rows = session.exec(select(models[kind]).order_by(*ordering[kind])).all()
	search = request.query_params.get("search", "").strip().lower()
	fields = {
		"users": ["username"],
		"api-keys": ["name"],
		"domains": ["authority"],
		"webhooks": ["name", "url"],
	}[kind]
	rows = [r for r in rows if any(search in str(getattr(r, f) or "").lower() for f in fields)]
	role = request.query_params.get("role")
	status = request.query_params.get("status")
	event = request.query_params.get("event")
	if role in ("admin", "user", "author", "domain") and kind in ("users", "api-keys"):
		rows = [r for r in rows if r.role == role]
	now = datetime.now(UTC)
	if kind == "api-keys":
		if status == "expired":
			rows = [r for r in rows if r.expires_at and r.expires_at <= now]
		if status in ("enabled", "disabled"):
			rows = [
				r
				for r in rows
				if r.enabled == (status == "enabled") and (not r.expires_at or r.expires_at > now)
			]
	if kind == "webhooks":
		if status in ("enabled", "disabled"):
			rows = [r for r in rows if r.enabled == (status == "enabled")]
		if event:
			rows = [r for r in rows if event in r.events.split(",")]
	if kind == "domains" and status in ("default", "additional"):
		rows = [r for r in rows if r.is_default == (status == "default")]
	return paginate(request, rows)


EVENTS = ["url.created", "visit.recorded", "orphan_visit.recorded"]


def admin_page(request, session, user, kind, error="", secret=""):
	rows, pagination = admin_rows(request, session, kind)
	data = {"Pager": pagination, "Error": error}
	if kind == "users":
		admins = session.exec(select(User).where(User.role == "admin")).all()
		data.update(
			Users=[model_view(r) for r in rows],
			LastAdminID=admins[0].id if len(admins) == 1 else 0,
			Filters=filters(request, "Search username…", [("role", "Role", ["admin", "user"])]),
		)
		return page(request, user, "users", "Users", data)
	if kind == "api-keys":
		domains = session.exec(select(Domain)).all()
		by_id = {d.id: d.authority for d in domains}
		data.update(
			APIBaseURL=f"{getattr(request.state, 'scheme', request.url.scheme)}://{request.url.netloc}",
			PlainKey=secret,
			Domains=[d.authority for d in domains],
			Filters=filters(
				request,
				"Search key name…",
				[
					("status", "Status", ["enabled", "disabled", "expired"]),
					("role", "Role", ["admin", "author", "domain"]),
				],
			),
			Rows=[
				{
					"Name": r.name or "—",
					"Role": r.role,
					"Domain": by_id.get(r.domain_id, "—"),
					"Expired": bool(r.expires_at and r.expires_at <= datetime.now(UTC)),
					"Enabled": r.enabled,
					"Expires": format_date(r.expires_at) if r.expires_at else "never",
					"Created": format_date(r.created_at),
					"ToggleAction": f"/admin/api-keys/{r.id}/toggle",
					"DeleteAction": f"/admin/api-keys/{r.id}/delete",
				}
				for r in rows
			],
		)
		return page(request, user, "apikeys", "API keys", data)
	if kind == "webhooks":
		data.update(
			Secret=secret,
			Webhooks=[model_view(r) for r in rows],
			EventChecks=[
				{"Field": "event_" + e.replace(".", "_"), "Label": e, "Checked": e == "url.created"}
				for e in EVENTS
			],
			Filters=filters(
				request,
				"Search name or URL…",
				[("status", "Status", ["enabled", "disabled"]), ("event", "Event", EVENTS)],
			),
		)
		return page(request, user, "webhooks", "Webhooks", data)
	if kind == "domains":
		values = []
		for row in rows:
			v = model_view(row)
			links = session.exec(select(ShortURL.id).where(ShortURL.domain_id == row.id)).all()
			v["ShortURLCount"] = len(links)
			v["VisitCount"] = session.exec(
				select(func.count()).select_from(Visit).where(Visit.short_url_id.in_(links))
			).one()
			values.append(v)
		data.update(
			Domains=values,
			Filters=filters(
				request, "Search domain…", [("status", "Type", ["default", "additional"])]
			),
		)
		return page(request, user, "domains", "Domains", data)


def tags_page(request, session, user, error="", status=200):
	tags = session.exec(select(Tag).order_by(Tag.name)).all()
	search = request.query_params.get("search", "").strip().lower()
	tags, pagination = paginate(request, [t for t in tags if search in t.name.lower()])
	rows = []
	for tag in tags:
		ids = session.exec(
			select(ShortURLTag.short_url_id).where(ShortURLTag.tag_id == tag.id)
		).all()
		rows.append(
			{
				"Name": tag.name,
				"ShortURLCount": len(ids),
				"VisitCount": session.exec(
					select(func.count()).select_from(Visit).where(Visit.short_url_id.in_(ids))
				).one(),
			}
		)
	table = {"Tags": rows, "Pager": pagination}
	if request.headers.get("HX-Request") == "true":
		return render("tag-table", table, status)
	return page(
		request,
		user,
		"tags",
		"Tags",
		{"Search": request.query_params.get("search", ""), "Table": table, "Error": error},
		status,
	)


@router.api_route("/admin", methods=["GET"])
@router.api_route("/admin/{path:path}", methods=["GET", "POST"])
async def dashboard(request: Request, path: str = ""):
	form = await request.form() if request.method == "POST" else {}
	return await run_in_threadpool(dashboard_response, request, path, form)


def dashboard_response(request, path, form):
	with Session(request.app.state.engine) as session:
		user = auth.current_user(request, session)
		if not user:
			target = request.url.path + ("?" + request.url.query if request.url.query else "")
			return redirect("/admin/login?returnUrl=" + quote(target, safe=""))
		cfg = request.app.state.settings
		if path.startswith("webhooks") and not cfg.webhooks_enabled:
			raise HTTPException(404)
		if not path:
			return overview_page(request, session, user)
		if not path.startswith("short-urls") and not user.is_admin:
			raise HTTPException(403, detail="This action is only available to administrators.")
		if path == "short-urls" and request.method == "GET":
			return short_urls_page(request, session, user)
		if path == "short-urls/new":
			if request.method == "GET":
				return create_page(request, user)
			try:
				payload = link_form(form)
				if not user.can_see_group(
					auth.normalize_group(payload["group"]) if payload["group"] else None
				):
					raise ValueError("You can only assign groups you are a member of.")
				services.create_short_url(session, cfg, payload, author_user_id=user.id)
				return redirect("/admin/short-urls")
			except ValueError as exc:
				return create_page(request, user, form, str(exc), 400)
		match = re.fullmatch(
			r"short-urls/(\d+)/(edit|delete|visits|visits/delete|rules/add|rules/delete)", path
		)
		if match:
			link = visible_link(session, user, int(match[1]))
			action = match[2]
			base = f"/admin/short-urls/{link.id}"
			if request.method == "GET" and action == "edit":
				return edit_page(request, session, user, link)
			if request.method == "GET" and action == "visits":
				dto = services.short_url_dto(session, cfg, link)
				return page(
					request,
					user,
					"visits_shorturl",
					"Visits",
					{
						"Display": dto["shortUrl"].split("://", 1)[-1],
						"EditURL": base + "/edit",
						"ShortURL": dto["shortUrl"],
						"LongURL": link.long_url,
						"Analytics": analytics(request, session, link.id),
					},
				)
			if request.method != "POST":
				raise HTTPException(405)
			try:
				if action == "edit":
					payload = link_form(form)
					payload.pop("customSlug")
					payload.pop("domain")
					if not user.can_see_group(
						auth.normalize_group(payload["group"]) if payload["group"] else None
					):
						raise ValueError("You can only assign groups you are a member of.")
					services.update_short_url(session, cfg, link, payload)
				elif action == "delete":
					services.delete_short_url(session, link)
					return redirect("/admin/short-urls")
				elif action == "visits/delete":
					session.exec(delete(Visit).where(Visit.short_url_id == link.id))
					session.commit()
				elif action.startswith("rules/"):
					rules = services.get_rules(session, link)["redirectRules"]
					if action == "rules/delete":
						rules = [
							r for r in rules if r["priority"] != integer(form.get("priority"), -1)
						]
					else:
						from .domain import validate_long_url

						conditions = []
						for field, kind in [
							("device", "device"),
							("language", "language"),
							("ipAddress", "ip-address"),
						]:
							if form.get(field, "").strip():
								conditions.append({"type": kind, "matchValue": form[field].strip()})
						if form.get("queryKey", "").strip():
							conditions.append(
								{
									"type": "query-param",
									"matchKey": form["queryKey"].strip(),
									"matchValue": form.get("queryValue", "").strip(),
								}
							)
						if not conditions:
							raise ValueError(
								"A rule needs at least one condition (device, language, query param or IP)."
							)
						rules.append(
							{
								"priority": len(rules) + 1,
								"longUrl": validate_long_url(form.get("ruleLongUrl", "")),
								"conditions": conditions,
							}
						)
					services.set_rules(session, link, rules)
				else:
					raise HTTPException(405)
				return redirect(base + "/edit")
			except ValueError as exc:
				return edit_page(request, session, user, link, str(exc), 400)
		if path == "visits/orphan" and request.method == "GET":
			return page(
				request,
				user,
				"visits_orphan",
				"Orphan visits",
				{"ShowDelete": True, "Analytics": analytics(request, session, orphan=True)},
			)
		if path == "visits/orphan/delete" and request.method == "POST":
			session.exec(delete(Visit).where(Visit.short_url_id.is_(None)))
			session.commit()
			return redirect("/admin/visits/orphan")
		if path == "tags" and request.method == "GET":
			return tags_page(request, session, user)
		if path in ("tags/rename", "tags/delete") and request.method == "POST":
			from .domain import validate_tag

			tag = session.exec(
				select(Tag).where(
					Tag.name == form.get("oldName" if path.endswith("rename") else "name", "")
				)
			).first()
			try:
				if path.endswith("rename"):
					name = validate_tag(form.get("newName", ""))
					duplicate = session.exec(select(Tag).where(Tag.name == name)).first()
					if duplicate and tag and duplicate.id != tag.id:
						raise ValueError(f"Tag '{name}' already exists.")
					if tag:
						tag.name = name
						session.add(tag)
				elif tag:
					session.delete(tag)
				session.commit()
				return redirect("/admin/tags")
			except ValueError as exc:
				return tags_page(request, session, user, str(exc), 400)
		match = re.fullmatch(
			r"(users|api-keys|domains|webhooks)(?:/(\d+)/(role|password|toggle|redirects|delete))?",
			path,
		)
		if match:
			kind, identifier, action = match.groups()
			if request.method == "GET" and not identifier:
				return admin_page(request, session, user, kind)
			if request.method != "POST":
				raise HTTPException(405)
			return admin_action(
				request, session, user, kind, integer(identifier, None), action, form
			)
		raise HTTPException(404)


def admin_action(request, session, user, kind, identifier, action, form):
	from .domain import validate_domain, validate_long_url

	back = "/admin/" + kind
	models = {"users": User, "api-keys": APIKey, "domains": Domain, "webhooks": Webhook}
	try:
		if identifier is not None:
			target = session.get(models[kind], identifier)
			if not target:
				return redirect(back)
			if kind == "users":
				admins = session.exec(
					select(func.count()).select_from(User).where(User.role == "admin")
				).one()
				last_admin = target.role == "admin" and admins <= 1
				if action == "delete":
					if not last_admin and target.id != user.id:
						session.delete(target)
				elif action == "role":
					role = "admin" if form.get("role") == "admin" else "user"
					if not (last_admin and role == "user"):
						target.role = role
						session.add(target)
				elif action == "password":
					password = form.get("password", "")
					if len(password.encode()) < 8:
						raise ValueError("Passwords need at least 8 UTF-8 bytes.")
					if len(password.encode()) > 72:
						raise ValueError("Passwords cannot exceed 72 UTF-8 bytes.")
					target.password_hash = auth.hash_password(password)
					session.add(target)
				else:
					raise HTTPException(404)
			elif action == "delete":
				if kind != "domains" or not target.is_default:
					session.delete(target)
			elif action == "toggle" and kind in ("api-keys", "webhooks"):
				target.enabled = not target.enabled
				session.add(target)
			elif action == "redirects" and kind == "domains":
				for field, name in [
					("base_url_redirect", "baseUrlRedirect"),
					("regular_404_redirect", "regular404Redirect"),
					("invalid_short_url_redirect", "invalidShortUrlRedirect"),
				]:
					value = form.get(name, "").strip() or None
					if value:
						validate_long_url(value)
					setattr(target, field, value)
				session.add(target)
			else:
				raise HTTPException(404)
			session.commit()
			return redirect(back)
		if kind == "users":
			username = form.get("username", "").strip()
			password = form.get("password", "")
			if not username or len(password.encode()) < 8:
				raise ValueError("Enter a username and a password of at least 8 UTF-8 bytes.")
			if len(password.encode()) > 72:
				raise ValueError("Passwords cannot exceed 72 UTF-8 bytes.")
			if session.exec(select(User).where(User.username == username)).first():
				raise ValueError(f"Username '{username}' is already taken.")
			session.add(
				User(
					username=username,
					password_hash=auth.hash_password(password),
					role="admin" if form.get("role") == "admin" else "user",
				)
			)
		elif kind == "domains":
			authority = validate_domain(form.get("authority", ""))
			if session.exec(select(Domain).where(Domain.authority == authority)).first():
				raise ValueError(f"Domain '{authority}' is already registered.")
			session.add(Domain(authority=authority))
		elif kind == "api-keys":
			role = form.get("role", "admin").lower()
			domain = None
			if role not in ("admin", "author", "domain"):
				raise ValueError(f"Unknown role '{role}'. Use admin, author or domain.")
			if role == "domain":
				domain = session.exec(
					select(Domain).where(Domain.authority == form.get("domain", "").strip().lower())
				).first()
				if not domain:
					raise ValueError("Domain keys require a registered domain.")
			expires = parse_date(form.get("expiresAt"))
			if form.get("expiresAt") and not expires:
				raise ValueError("expiresAt must be a valid date.")
			if expires and expires <= datetime.now(UTC):
				raise ValueError("expiresAt must be in the future.")
			plain = auth.generate_api_key()
			session.add(
				APIKey(
					key_hash=auth.hash_api_key(plain),
					name=form.get("name", "").strip() or None,
					role=role,
					domain_id=domain.id if domain else None,
					expires_at=expires,
				)
			)
			session.commit()
			return admin_page(request, session, user, kind, secret=plain)
		elif kind == "webhooks":
			name = form.get("name", "").strip()
			if not name:
				raise ValueError("name is required.")
			try:
				url = validate_long_url(form.get("url", "").strip())
			except ValueError as exc:
				raise ValueError("url must be an absolute http(s) URL.") from exc
			events = [e for e in EVENTS if form.get("event_" + e.replace(".", "_")) == "true"]
			if not events:
				raise ValueError("Subscribe to at least one event: " + ", ".join(EVENTS) + ".")
			secret = secrets.token_hex(32)
			session.add(Webhook(name=name, url=url, events=",".join(events), secret=secret))
			session.commit()
			return admin_page(request, session, user, kind, secret=secret)
		session.commit()
		return redirect(back)
	except ValueError as exc:
		if kind == "domains":
			return page(
				request,
				user,
				"message",
				"Domains",
				{"Error": str(exc), "BackURL": back, "BackLabel": "← Back to domains"},
				400,
			)
		return admin_page(request, session, user, kind, error=str(exc))
