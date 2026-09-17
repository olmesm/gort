"""Public redirects, QR codes, robots.txt, and visit capture."""

import io
import logging
import re
from urllib.parse import parse_qsl

import qrcode
from fastapi import APIRouter, Request
from fastapi.responses import PlainTextResponse, Response
from sqlalchemy import func
from sqlmodel import Session, select
from user_agents import parse as parse_user_agent

from goto.domain import anonymize_ip, check_active, detect_device, forward_query, resolve_target
from goto.models import Domain, RedirectCondition, RedirectRule, ShortURL, Visit, utcnow

router = APIRouter()
logger = logging.getLogger(__name__)
BOT_PATTERN = re.compile(
	r"bot|crawl|spider|slurp|curl|wget|python-requests|httpclient|headless|preview|scan|monitor|facebookexternalhit|whatsapp|telegrambot|skypeuripreview|bingpreview",
	re.I,
)


def request_domain(session, request):
	domain = session.exec(
		select(Domain).where(Domain.authority == request.headers.get("host", "").lower())
	).first()
	return domain or session.exec(select(Domain).where(Domain.is_default.is_(True))).first()


def public_page(message=None):
	from goto.ui import render

	return render("notfound" if message else "landing", message, status=404 if message else 200)


def redirect(target, status=302):
	# Preserve the URL as stored, including its escaping, instead of re-quoting it.
	return Response(status_code=status, headers={"Location": target})


def record_visit(request, session, kind, link=None, domain=None):
	settings = request.app.state.settings
	if settings.disable_tracking or (kind != "valid" and not settings.track_orphan_visits):
		return
	if settings.track_skip_param and settings.track_skip_param in request.query_params:
		return
	user_agent = request.headers.get("user-agent") or None
	parsed = parse_user_agent(user_agent or "")
	remote = getattr(request.state, "remote_ip", "")
	if settings.disable_ip_tracking:
		remote = None
	elif settings.anonymize_ips:
		remote = anonymize_ip(remote) or None
	visited_url = None
	if kind != "valid":
		visited_url = f"{getattr(request.state, 'scheme', request.url.scheme)}://{request.headers.get('host', '')}{request.url.path}"
		if request.url.query:
			visited_url += "?" + request.url.query
	visit = Visit(
		short_url_id=link.id if link else None,
		visit_type=kind,
		user_agent=user_agent,
		referer=request.headers.get("referer") or None,
		browser=parsed.browser.family if parsed.browser.family != "Other" else None,
		os=parsed.os.family if parsed.os.family != "Other" else None,
		device=detect_device(user_agent or ""),
		is_bot=bool(BOT_PATTERN.search(user_agent or "")),
		remote_ip=remote,
		visited_url=visited_url,
	)
	try:
		session.add(visit)
		session.commit()
		from goto.workers import enqueue_event

		payload = {"visitType": kind, "potentialBot": visit.is_bot}
		for name, value in [
			("visitedUrl", visited_url),
			("referer", visit.referer),
			("userAgent", user_agent),
		]:
			if value is not None:
				payload[name] = value
		if link:
			payload["shortUrl"] = {
				"shortCode": link.short_code,
				"domain": domain.authority,
				"longUrl": link.long_url,
			}
		enqueue_event(
			session,
			settings,
			"visit.recorded" if kind == "valid" else "orphan_visit.recorded",
			payload,
		)
	except Exception:
		session.rollback()
		logger.exception("Failed to record visit")


def invalid(request, session, domain, slug):
	is_code = (
		"/" not in slug
		and "." not in slug
		and len(slug) <= 64
		and bool(re.fullmatch(r"[A-Za-z0-9_-]+", slug))
	)
	record_visit(request, session, "invalid_short_url" if is_code else "regular_404")
	field = "invalid_short_url_redirect" if is_code else "regular_404_redirect"
	target = getattr(domain, field) or getattr(request.app.state.settings, field)
	return redirect(target) if target else public_page("This short URL does not exist.")


@router.get("/", include_in_schema=False)
def landing(request: Request):
	with Session(request.app.state.engine) as session:
		domain = request_domain(session, request)
		record_visit(request, session, "base_url")
		target = domain.base_url_redirect or request.app.state.settings.base_url_redirect
		return redirect(target) if target else public_page()


@router.get("/robots.txt", include_in_schema=False)
def robots(request: Request):
	with Session(request.app.state.engine) as session:
		codes = session.exec(select(ShortURL.short_code).where(ShortURL.crawlable.is_(True))).all()
	return PlainTextResponse(
		"\n".join(
			["User-agent: *", *(f"Allow: /{code}" for code in codes), "Allow: /$", "Disallow: /"]
		)
	)


def bounded_int(raw, default, low, high):
	try:
		return min(high, max(low, int(raw)))
	except (TypeError, ValueError):
		return default


def qr_response(request, session, domain, code):
	link = session.exec(
		select(ShortURL).where(ShortURL.domain_id == domain.id, ShortURL.short_code == code)
	).first()
	if link is None:
		return public_page("There is no short URL to encode.")
	query = request.query_params
	size = bounded_int(query.get("size"), 300, 50, 1000)
	margin = bounded_int(query.get("margin"), 1, 0, 20)
	levels = {
		"L": qrcode.constants.ERROR_CORRECT_L,
		"M": qrcode.constants.ERROR_CORRECT_M,
		"Q": qrcode.constants.ERROR_CORRECT_Q,
		"H": qrcode.constants.ERROR_CORRECT_H,
	}
	qr = qrcode.QRCode(
		error_correction=levels.get(
			query.get("errorCorrection", "L").upper(), qrcode.constants.ERROR_CORRECT_L
		),
		border=margin,
	)
	qr.add_data(request.app.state.settings.short_url_base(domain.authority) + "/" + code)
	qr.make(fit=True)
	matrix = qr.get_matrix()
	total = len(matrix)
	if query.get("format", "").lower() == "svg":
		squares = "".join(
			f'<rect x="{x}" y="{y}" width="1" height="1"/>'
			for y, row in enumerate(matrix)
			for x, value in enumerate(row)
			if value
		)
		return Response(
			f'<svg xmlns="http://www.w3.org/2000/svg" width="{size}" height="{size}" viewBox="0 0 {total} {total}" shape-rendering="crispEdges"><rect width="{total}" height="{total}" fill="white"/><g fill="black">{squares}</g></svg>',
			media_type="image/svg+xml",
		)
	qr.box_size = max(1, size // total)
	output = io.BytesIO()
	qr.make_image(fill_color="black", back_color="white").save(output, format="PNG")
	return Response(output.getvalue(), media_type="image/png")


@router.get("/{slug:path}", include_in_schema=False)
def short_url(request: Request, slug: str):
	slug = slug.strip("/")
	with Session(request.app.state.engine) as session:
		domain = request_domain(session, request)
		if slug.endswith("/qr-code"):
			return qr_response(request, session, domain, slug[: -len("/qr-code")])
		if slug in ("admin", "rest", "graphql") or slug.startswith(("admin/", "rest/", "graphql/")):
			return public_page("This page does not exist.")
		link = session.exec(
			select(ShortURL).where(ShortURL.domain_id == domain.id, ShortURL.short_code == slug)
		).first()
		if link is None:
			return invalid(request, session, domain, slug)
		count = (
			session.exec(
				select(func.count(Visit.id)).where(
					Visit.short_url_id == link.id, Visit.visit_type == "valid"
				)
			).one()
			if link.max_visits is not None
			else 0
		)
		if check_active(link, utcnow(), count):
			return invalid(request, session, domain, slug)
		rules = []
		for rule in session.exec(
			select(RedirectRule)
			.where(RedirectRule.short_url_id == link.id)
			.order_by(RedirectRule.priority)
		).all():
			conditions = session.exec(
				select(RedirectCondition).where(RedirectCondition.rule_id == rule.id)
			).all()
			rules.append(
				{
					"longUrl": rule.long_url,
					"priority": rule.priority,
					"conditions": [
						{"type": c.cond_type, "matchKey": c.match_key, "matchValue": c.match_value}
						for c in conditions
					],
				}
			)
		query = dict(reversed(list(request.query_params.multi_items())))
		target = resolve_target(
			link.long_url,
			rules,
			user_agent=request.headers.get("user-agent", ""),
			accept_language=request.headers.get("accept-language", ""),
			query=query,
			remote_ip=getattr(request.state, "remote_ip", ""),
		)
		if link.forward_query:
			skip = request.app.state.settings.track_skip_param.lower()
			incoming = parse_qsl(request.url.query, keep_blank_values=True)
			# Preserve first-seen key order, grouping repeated values for each key.
			keys = dict.fromkeys(k for k, _ in incoming)
			incoming = [
				(k, v)
				for k in keys
				for key, v in incoming
				if key == k and (not skip or k.lower() != skip)
			]
			target = forward_query(target, incoming)
		record_visit(request, session, "valid", link, domain)
		status = (
			link.redirect_status
			if link.redirect_status in (301, 302, 307, 308)
			else request.app.state.settings.redirect_status
		)
		return redirect(target, status)
