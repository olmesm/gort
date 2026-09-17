"""REST adapters and operations shared with the GraphQL API."""

from __future__ import annotations

import hashlib
import re
import secrets
from datetime import datetime, timezone
from typing import Annotated
from urllib.parse import urlsplit

from fastapi import APIRouter, Depends, Request, Response, Security
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from fastapi.security import APIKeyHeader, HTTPBearer
from pydantic import BaseModel, BeforeValidator, ConfigDict, Field, StrictBool, StrictInt
from sqlalchemy import delete, func
from sqlmodel import Session, select

from . import models as m
from . import services as s
from .domain import validate_domain, validate_tag


class Problem(Exception):
	def __init__(self, status: int, detail: str, kind: str | None = None):
		self.status_code = status
		self.kind = kind or {
			400: "invalid-data",
			401: "missing-authentication",
			403: "forbidden",
			404: "not-found",
			409: "conflict",
			500: "internal",
		}.get(status, "invalid-data")
		self.detail = detail
		super().__init__(detail)


def problem_response(status: int, detail: str, kind: str | None = None):
	error = Problem(status, detail, kind)
	title = {
		400: "Invalid data",
		401: "Authentication required",
		403: "Forbidden",
		404: "Not found",
		409: "Conflict",
		429: "Too many requests",
		500: "Internal server error",
	}.get(status, "Invalid data")
	return JSONResponse(
		{
			"type": "https://goto.dev/errors/" + error.kind,
			"title": title,
			"detail": detail,
			"status": status,
		},
		status_code=status,
		media_type="application/problem+json",
	)


def install_exception_handlers(app):
	async def handle_problem(request, exc):
		return problem_response(exc.status_code, str(exc), getattr(exc, "kind", None))

	async def handle_validation(request, exc):
		return problem_response(
			400, "; ".join(f"{'.'.join(map(str, e['loc']))}: {e['msg']}" for e in exc.errors())
		)

	app.add_exception_handler(Problem, handle_problem)
	app.add_exception_handler(s.ServiceError, handle_problem)
	app.add_exception_handler(RequestValidationError, handle_validation)


class Body(BaseModel):
	model_config = ConfigDict(extra="ignore")


def require_rfc3339(value):
	if not isinstance(value, str) or not re.fullmatch(
		r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})",
		value,
	):
		raise ValueError("Expected an RFC 3339 date-time string.")
	return value


RFC3339Time = Annotated[datetime, BeforeValidator(require_rfc3339)]


class CreateLink(Body):
	longUrl: str
	customSlug: str | None = None
	shortCodeLength: StrictInt | None = None
	domain: str | None = None
	title: str | None = None
	tags: list[str] | None = None
	group: str | None = None
	maxVisits: StrictInt | None = Field(default=None, ge=-(2**63), lt=2**63)
	validSince: RFC3339Time | None = None
	validUntil: RFC3339Time | None = None
	forwardQuery: StrictBool | None = None
	crawlable: StrictBool | None = None
	redirectStatus: StrictInt | None = None
	findIfExists: StrictBool | None = None


class EditLink(Body):
	"""Omitted fields stay unchanged. Null clears title, tags, group, maxVisits,
	validSince and validUntil. Null sets forwardQuery and crawlable to false;
	longUrl and redirectStatus reject null.
	"""

	longUrl: str | None = None
	title: str | None = None
	tags: list[str] | None = None
	group: str | None = None
	maxVisits: StrictInt | None = Field(default=None, ge=-(2**63), lt=2**63)
	validSince: RFC3339Time | None = None
	validUntil: RFC3339Time | None = None
	forwardQuery: StrictBool | None = None
	crawlable: StrictBool | None = None
	redirectStatus: StrictInt | None = None


class Condition(Body):
	type: str
	matchKey: str | None = None
	matchValue: str


class Rule(Body):
	longUrl: str
	conditions: list[Condition]


class Rules(Body):
	redirectRules: list[Rule] | None = None


class CreateDomain(Body):
	domain: str


class DomainRedirects(CreateDomain):
	baseUrlRedirect: str | None = None
	regular404Redirect: str | None = None
	invalidShortUrlRedirect: str | None = None


class RenameTag(Body):
	oldName: str
	newName: str


class CreateKey(Body):
	name: str | None = None
	role: str | None = None
	domain: str | None = None
	expiresAt: RFC3339Time | None = None


class Enabled(Body):
	enabled: StrictBool | None = False


class CreateWebhook(Body):
	name: str
	url: str
	events: list[str]


def number(raw, default=1):
	return s.integer(raw, default)


def boolean(raw):
	return str(raw).lower() in ("true", "1", "yes")


def date(raw):
	return s._filter_date(raw)


def paging(filters, default=20):
	page = max(1, number(filters.get("page"), 1))
	size = number(filters.get("itemsPerPage"), default)
	return page, min(500, size) if size > 0 else 20


def page_result(items, total, page, size):
	return {
		"data": items,
		"pagination": {
			"currentPage": page,
			"pagesCount": (total + size - 1) // size,
			"itemsPerPage": size,
			"itemsInCurrentPage": len(items),
			"totalItems": total,
		},
	}


def query(request):
	result = dict(request.query_params)
	tags = request.query_params.getlist("tags") + request.query_params.getlist("tags[]")
	if tags:
		result["tags"] = tags
	return result


def admin(key, detail="This operation requires an admin API key."):
	if key.role != "admin":
		raise Problem(403, detail)


class Operations:
	def __init__(self, session, settings, key):
		self.session, self.settings, self.key = session, settings, key

	def domain(self, authority):
		row = self.session.exec(
			select(m.Domain).where(m.Domain.authority == authority.strip().lower())
		).first()
		if row is None:
			raise Problem(404, f"Domain '{authority}' is not registered.")
		return row

	def link(self, code, domain=None):
		if domain and s.resolve_named_domain(self.session, domain) is None:
			raise Problem(404, f"Domain '{domain}' is not registered.")
		row = s.find_short_url(self.session, self.settings, code, domain)
		if row is None or not (
			self.key.role == "admin"
			or self.key.role == "author"
			and row.author_api_key_id == self.key.id
			or self.key.role == "domain"
			and row.domain_id == self.key.domain_id
		):
			raise Problem(404, f"No short URL found for code '{code}'.")
		return row

	def short_urls(self, filters=None):
		scope = {}
		if self.key.role == "author":
			scope["author_api_key_id"] = self.key.id
		if self.key.role == "domain":
			scope["domain_id"] = self.key.domain_id
		return s.list_short_urls(self.session, self.settings, filters or {}, **scope)

	def short_url(self, code, domain=None):
		return s.short_url_dto(self.session, self.settings, self.link(code, domain))

	def create_short_url(self, payload):
		if self.key.role == "domain":
			domain = self.session.get(m.Domain, self.key.domain_id)
			if not domain or (payload.get("domain") or "").lower() != domain.authority:
				raise Problem(403, "This API key may only create short URLs on its own domain.")
		row = s.create_short_url(
			self.session, self.settings, payload, author_api_key_id=self.key.id
		)
		return s.short_url_dto(self.session, self.settings, row)

	def update_short_url(self, code, payload, domain=None):
		row = s.update_short_url(self.session, self.settings, self.link(code, domain), payload)
		return s.short_url_dto(self.session, self.settings, row)

	def delete_short_url(self, code, domain=None):
		self.session.delete(self.link(code, domain))
		self.session.commit()
		return True

	def rules(self, code, domain=None, rules=None):
		row = self.link(code, domain)
		if rules is not None:
			s.set_redirect_rules(self.session, row.id, rules)
		return {
			"defaultLongUrl": row.long_url,
			"redirectRules": s.get_redirect_rules(self.session, row.id),
		}

	def domains(self):
		return [
			s.domain_dto(row)
			for row in self.session.exec(
				select(m.Domain).order_by(m.Domain.is_default.desc(), m.Domain.authority)
			).all()
		]

	def create_domain(self, authority):
		admin(self.key)
		try:
			authority = validate_domain(authority)
		except ValueError as exc:
			raise Problem(400, str(exc)) from exc
		if self.session.exec(select(m.Domain).where(m.Domain.authority == authority)).first():
			raise Problem(409, f"Domain '{authority}' is already registered.", "domain-exists")
		row = m.Domain(authority=authority)
		self.session.add(row)
		self.session.commit()
		self.session.refresh(row)
		return s.domain_dto(row)

	def domain_redirects(self, payload):
		admin(self.key)
		row = self.domain(payload["domain"])
		for wire, field in [
			("baseUrlRedirect", "base_url_redirect"),
			("regular404Redirect", "regular_404_redirect"),
			("invalidShortUrlRedirect", "invalid_short_url_redirect"),
		]:
			setattr(row, field, payload.get(wire))
		self.session.add(row)
		self.session.commit()
		return s.domain_dto(row)

	def delete_domain(self, authority):
		admin(self.key)
		row = self.domain(authority)
		if row.is_default:
			raise Problem(403, "The default domain cannot be deleted.")
		self.session.delete(row)
		self.session.commit()
		return True

	def tags(self, filters=None, with_stats=False):
		filters = filters or {}
		if with_stats:
			admin(self.key, "Only admin keys can view tag statistics.")
		statement = select(m.Tag)
		if search := filters.get("searchTerm"):
			statement = statement.where(m.Tag.name.ilike(f"%{search}%"))
		page, size = paging(filters, 500)
		total = self.session.exec(select(func.count()).select_from(statement.subquery())).one()
		page = min(page, max(1, (total + size - 1) // size))
		rows = self.session.exec(
			statement.order_by(m.Tag.name).offset((page - 1) * size).limit(size)
		).all()
		result = []
		for row in rows:
			if not with_stats:
				result.append(row.name)
				continue
			ids = select(m.ShortURLTag.short_url_id).where(m.ShortURLTag.tag_id == row.id)
			count = self.session.exec(
				select(func.count())
				.select_from(m.ShortURLTag)
				.where(m.ShortURLTag.tag_id == row.id)
			).one()
			visits = self.session.exec(
				select(func.count()).select_from(m.Visit).where(m.Visit.short_url_id.in_(ids))
			).one()
			result.append({"tag": row.name, "shortUrlsCount": count, "visitsCount": visits})
		return page_result(result, total, page, size)

	def rename_tag(self, old_name, new_name):
		admin(self.key, "Only admin keys can change global tags.")
		try:
			new_name = validate_tag(new_name)
		except ValueError as exc:
			raise Problem(400, str(exc)) from exc
		existing = self.session.exec(select(m.Tag).where(m.Tag.name == new_name)).first()
		if existing and old_name != new_name:
			raise Problem(409, f"A tag named '{new_name}' already exists.", "tag-conflict")
		row = self.session.exec(select(m.Tag).where(m.Tag.name == old_name)).first()
		if not row:
			raise Problem(404, f"Tag '{old_name}' was not found.")
		row.name = new_name
		self.session.add(row)
		self.session.commit()
		return {"oldName": old_name, "newName": new_name}

	def delete_tags(self, tags):
		admin(self.key, "Only admin keys can change global tags.")
		tags = [part.strip() for raw in tags for part in raw.split(",") if part.strip()]
		if not tags:
			raise Problem(400, "Provide at least one tag using the tags or tags[] query parameter.")
		result = self.session.exec(delete(m.Tag).where(m.Tag.name.in_(tags)))
		self.session.commit()
		return result.rowcount

	def _visits(self, scope=None, filters=None):
		scope, filters = scope or {}, filters or {}
		statement = select(m.Visit).where(
			m.Visit.visit_type != "valid" if scope.get("orphan") else m.Visit.visit_type == "valid"
		)
		if "short_url_id" in scope:
			statement = statement.where(m.Visit.short_url_id == scope["short_url_id"])
		elif "domain_id" in scope:
			ids = select(m.ShortURL.id).where(m.ShortURL.domain_id == scope["domain_id"])
			statement = statement.where(m.Visit.short_url_id.in_(ids))
		elif "tag" in scope:
			ids = (
				select(m.ShortURLTag.short_url_id)
				.join(m.Tag, m.Tag.id == m.ShortURLTag.tag_id)
				.where(m.Tag.name == scope["tag"])
			)
			statement = statement.where(m.Visit.short_url_id.in_(ids))
		elif scope.get("orphan"):
			statement = statement.where(m.Visit.visit_type != "valid")
		else:
			statement = statement.where(m.Visit.visit_type == "valid")
		if scope.get("type") in ("base_url", "invalid_short_url", "regular_404"):
			statement = statement.where(m.Visit.visit_type == scope["type"])
		for name, comparison in [
			("startDate", m.Visit.visited_at.__ge__),
			("endDate", m.Visit.visited_at.__le__),
		]:
			value = date(filters.get(name))
			if value:
				statement = statement.where(comparison(value))
		if boolean(filters.get("excludeBots")):
			statement = statement.where(m.Visit.is_bot.is_(False))  # noqa: E712
		return statement

	def visit_page(self, scope, filters=None):
		statement = self._visits(scope, filters)
		page, size = paging(filters or {})
		total = self.session.exec(select(func.count()).select_from(statement.subquery())).one()
		page = min(page, max(1, (total + size - 1) // size))
		rows = self.session.exec(
			statement.order_by(m.Visit.visited_at.desc(), m.Visit.id.desc())
			.offset((page - 1) * size)
			.limit(size)
		).all()
		return page_result([s.visit_dto(row) for row in rows], total, page, size)

	def link_visits(self, code, domain=None, filters=None):
		return self.visit_page({"short_url_id": self.link(code, domain).id}, filters)

	def domain_visits(self, authority, filters=None):
		domain = self.domain(authority)
		if self.key.role != "admin" and not (
			self.key.role == "domain" and self.key.domain_id == domain.id
		):
			raise Problem(403, "This API key cannot view visits for this domain.")
		return self.visit_page({"domain_id": domain.id}, filters)

	def tag_visits(self, tag, filters=None):
		admin(self.key, "Only admin keys can view tag visits.")
		if not self.session.exec(select(m.Tag).where(m.Tag.name == tag)).first():
			raise Problem(404, f"Tag '{tag}' was not found.")
		return self.visit_page({"tag": tag}, filters)

	def visits(self, filters=None, orphan=False, visit_type=None):
		admin(
			self.key,
			"Only admin keys can list orphan visits."
			if orphan
			else "Only admin keys can list all visits.",
		)
		return self.visit_page({"orphan": orphan, "type": visit_type}, filters)

	def delete_visits(self, code=None, domain=None):
		if code is None:
			admin(self.key, "Only admin keys can delete orphan visits.")
			condition = m.Visit.visit_type != "valid"
		else:
			condition = m.Visit.short_url_id == self.link(code, domain).id
		result = self.session.exec(delete(m.Visit).where(condition))
		self.session.commit()
		return result.rowcount

	def overview(self):
		admin(self.key, "Only admin keys can view the global visit summary.")

		def count(model, *conditions):
			return self.session.exec(
				select(func.count()).select_from(model).where(*conditions)
			).one()

		return {
			"visitsCount": count(m.Visit, m.Visit.visit_type == "valid"),
			"orphanVisitsCount": count(m.Visit, m.Visit.visit_type != "valid"),
			"shortUrlsCount": count(m.ShortURL),
			"tagsCount": count(m.Tag),
			"botVisitsCount": count(
				m.Visit, m.Visit.visit_type == "valid", m.Visit.is_bot.is_(True)
			),
		}  # noqa: E712

	def stats_scope(self, filters):
		if boolean(filters.get("orphan")):
			admin(self.key, "Only admin keys can query orphan visit stats.")
			return {"orphan": True}
		if filters.get("shortCode"):
			return {"short_url_id": self.link(filters["shortCode"], filters.get("domain")).id}
		if filters.get("tag"):
			admin(self.key, "Only admin keys can query tag statistics.")
			return {"tag": filters["tag"]}
		if filters.get("domain"):
			domain = self.domain(filters["domain"])
			if self.key.role != "admin" and not (
				self.key.role == "domain" and self.key.domain_id == domain.id
			):
				raise Problem(403, "This API key cannot view statistics for this domain.")
			return {"domain_id": domain.id}
		if self.key.role == "author":
			raise Problem(403, "Author keys must scope stats to a shortCode.")
		return {"domain_id": self.key.domain_id} if self.key.role == "domain" else {}

	def stats(self, filters=None, by=None, limit=25):
		filters = filters or {}
		columns = {
			"browser": "browser",
			"os": "os",
			"referer": "referer",
			"referrer": "referer",
			"device": "device",
		}
		if by is not None and by.lower() not in columns:
			raise Problem(
				400,
				"Set by to browser, os, referer, referrer or device.",
			)
		statement = self._visits(self.stats_scope(filters), filters)
		# Project and group in SQL to avoid loading every visit into memory.
		values = statement.subquery()
		column = (
			getattr(values.c, columns[by.lower()])
			if by is not None
			else func.to_char(func.timezone("UTC", values.c.visited_at), "YYYY-MM-DD")
		)
		grouped = select(column, func.count()).select_from(values).group_by(column)
		if by is not None:
			grouped = grouped.order_by(func.count().desc(), column).limit(
				max(1, min(100, number(limit, 25)))
			)
		else:
			grouped = grouped.order_by(column)
		return [
			{
				("value" if by is not None else "date"): value if value is not None else "Unknown",
				"count": count,
			}
			for value, count in self.session.exec(grouped).all()
		]

	def key_dto(self, row, plain=None):
		result = {
			"id": row.id,
			"role": row.role,
			"enabled": row.enabled,
			"createdAt": row.created_at,
		}
		if row.name is not None:
			result["name"] = row.name
		if row.expires_at is not None:
			result["expiresAt"] = row.expires_at
		if row.domain_id is not None:
			domain = self.session.get(m.Domain, row.domain_id)
			if domain:
				result["domain"] = domain.authority
		if plain:
			result["apiKey"] = plain
		return result

	def keys(self):
		admin(self.key)
		return [
			self.key_dto(row)
			for row in self.session.exec(
				select(m.APIKey).order_by(m.APIKey.created_at.desc())
			).all()
		]

	def create_key(self, payload):
		admin(self.key)
		role = (payload.get("role") or "admin").lower()
		domain = self.session.exec(
			select(m.Domain).where(
				m.Domain.authority == (payload.get("domain") or "").strip().lower()
			)
		).first()
		if role not in ("admin", "author", "domain"):
			raise Problem(400, f"Unknown role '{role}'. Use admin, author or domain.")
		if role == "domain" and domain is None:
			raise Problem(400, "Domain keys require a registered domain.")
		expires = date(payload.get("expiresAt"))
		if expires and expires <= datetime.now(timezone.utc):
			raise Problem(400, "expiresAt must be in the future.")
		plain = "goto_" + secrets.token_urlsafe(32)
		row = m.APIKey(
			key_hash=hashlib.sha256(plain.encode()).hexdigest(),
			name=payload.get("name"),
			role=role,
			domain_id=domain.id if role == "domain" else None,
			expires_at=expires,
		)
		self.session.add(row)
		self.session.commit()
		self.session.refresh(row)
		return self.key_dto(row, plain)

	def _managed_row(self, model, row_id):
		admin(self.key)
		parsed_id = number(row_id, None)
		row = self.session.get(model, parsed_id) if parsed_id is not None else None
		if row is None:
			label = "API key" if model is m.APIKey else "Webhook"
			detail = (
				f"{label} {parsed_id} was not found."
				if parsed_id is not None
				else f"{label} was not found."
			)
			raise Problem(404, detail)
		return row

	def set_enabled(self, model, row_id, enabled):
		if model is m.Webhook:
			self.webhooks_enabled()
		row = self._managed_row(model, row_id)
		row.enabled = enabled
		self.session.add(row)
		self.session.commit()
		return {"id": row.id, "enabled": row.enabled}

	def delete_managed(self, model, row_id):
		if model is m.Webhook:
			self.webhooks_enabled()
		self.session.delete(self._managed_row(model, row_id))
		self.session.commit()
		return True

	def webhooks_enabled(self):
		if not self.settings.webhooks_enabled:
			raise Problem(404, "Webhooks are disabled.")
		admin(self.key)

	def webhook_dto(self, row, secret=False):
		result = {
			"id": row.id,
			"name": row.name,
			"url": row.url,
			"events": row.events.split(","),
			"enabled": row.enabled,
			"createdAt": row.created_at,
		}
		if secret:
			result["secret"] = row.secret
		return result

	def webhooks(self):
		self.webhooks_enabled()
		return [
			self.webhook_dto(row)
			for row in self.session.exec(select(m.Webhook).order_by(m.Webhook.name)).all()
		]

	def create_webhook(self, payload):
		self.webhooks_enabled()
		name, url, events = payload["name"].strip(), payload["url"], payload["events"]
		if not name:
			raise Problem(400, "name is required.")
		try:
			parsed = urlsplit(url)
		except ValueError as exc:
			raise Problem(400, "url must be an absolute http(s) URL.") from exc
		if parsed.scheme not in ("http", "https") or not parsed.hostname:
			raise Problem(400, "url must be an absolute http(s) URL.")
		allowed = ("url.created", "visit.recorded", "orphan_visit.recorded")
		if not events:
			raise Problem(400, "Subscribe to at least one event: " + ", ".join(allowed) + ".")
		for event in events:
			if event not in allowed:
				raise Problem(
					400, f"Unknown event '{event}'. Valid events: " + ", ".join(allowed) + "."
				)
		row = m.Webhook(name=name, url=url, secret=secrets.token_hex(32), events=",".join(events))
		self.session.add(row)
		self.session.commit()
		self.session.refresh(row)
		return self.webhook_dto(row, True)


def operations(
	request: Request,
	_header_key=Security(APIKeyHeader(name="X-Api-Key", scheme_name="apiKey", auto_error=False)),
	_bearer_key=Security(HTTPBearer(scheme_name="bearerAuth", auto_error=False)),
):
	from .auth import authenticate_api_key

	with Session(request.app.state.engine) as session:
		key = authenticate_api_key(request, session)
		if key is None:
			raw_key = request.headers.get("X-Api-Key", "")
			authorization = request.headers.get("Authorization", "")
			if not raw_key and authorization.lower().startswith("bearer "):
				raw_key = authorization[7:].strip()
			raise Problem(
				401,
				"The provided API key is not valid."
				if raw_key
				else "Expected an API key in the X-Api-Key header.",
			)
		yield Operations(session, request.app.state.settings, key)


class ResponseDTO(BaseModel):
	"""REST response fields. Routes omit optional fields that the operation did not supply."""

	model_config = ConfigDict(extra="forbid")


class PaginationDTO(ResponseDTO):
	currentPage: int
	pagesCount: int
	itemsPerPage: int
	itemsInCurrentPage: int
	totalItems: int


class PageDTO[T](ResponseDTO):
	data: list[T]
	pagination: PaginationDTO


class DataListDTO[T](ResponseDTO):
	data: list[T]


class ShortURLMetaDTO(ResponseDTO):
	validSince: datetime | None = None
	validUntil: datetime | None = None
	maxVisits: int | None = None


class VisitsSummaryDTO(ResponseDTO):
	total: int
	nonBots: int
	bots: int


class ShortURLDTO(ResponseDTO):
	shortCode: str
	shortUrl: str
	domain: str
	longUrl: str
	title: str | None = None
	dateCreated: datetime
	tags: list[str]
	group: str | None = None
	meta: ShortURLMetaDTO
	visitsSummary: VisitsSummaryDTO
	forwardQuery: bool
	crawlable: bool
	redirectStatus: int


class VisitDTO(ResponseDTO):
	date: datetime
	referer: str | None = None
	userAgent: str | None = None
	browser: str | None = None
	os: str | None = None
	device: str | None = None
	potentialBot: bool
	visitedUrl: str | None = None


class RuleConditionDTO(ResponseDTO):
	type: str
	matchKey: str | None = None
	matchValue: str


class RedirectRuleDTO(ResponseDTO):
	longUrl: str
	priority: int
	conditions: list[RuleConditionDTO]


class RedirectRulesDTO(ResponseDTO):
	defaultLongUrl: str
	redirectRules: list[RedirectRuleDTO]


class DomainRedirectsDTO(ResponseDTO):
	baseUrlRedirect: str | None = None
	regular404Redirect: str | None = None
	invalidShortUrlRedirect: str | None = None


class DomainDTO(ResponseDTO):
	domain: str
	isDefault: bool
	redirects: DomainRedirectsDTO


class TagStatsDTO(ResponseDTO):
	tag: str
	shortUrlsCount: int
	visitsCount: int


class TagRenameDTO(ResponseDTO):
	oldName: str
	newName: str


class DeletedTagsDTO(ResponseDTO):
	deletedTags: int


class DeletedVisitsDTO(ResponseDTO):
	deletedVisits: int


class VisitOverviewDTO(ResponseDTO):
	visitsCount: int
	orphanVisitsCount: int
	shortUrlsCount: int
	tagsCount: int
	botVisitsCount: int


class DayCountDTO(ResponseDTO):
	date: str
	count: int


class BreakdownCountDTO(ResponseDTO):
	value: str
	count: int


class APIKeyDTO(ResponseDTO):
	id: int
	name: str | None = None
	role: str
	domain: str | None = None
	enabled: bool
	expiresAt: datetime | None = None
	createdAt: datetime


class CreatedAPIKeyDTO(APIKeyDTO):
	apiKey: str = Field(description="Plaintext key, returned only when created.")


class WebhookDTO(ResponseDTO):
	id: int
	name: str
	url: str
	events: list[str]
	enabled: bool
	createdAt: datetime


class CreatedWebhookDTO(WebhookDTO):
	secret: str = Field(description="Signing secret, returned only when created.")


class IDEnabledDTO(ResponseDTO):
	id: int
	enabled: bool


Ops = Annotated[Operations, Depends(operations)]
router = APIRouter(prefix="/rest/v1")


@router.get("/short-urls", response_model_exclude_unset=True)
def list_links(request: Request, op: Ops) -> PageDTO[ShortURLDTO]:
	return op.short_urls(query(request))


@router.post("/short-urls", status_code=201, response_model_exclude_unset=True)
def create_link(body: CreateLink, op: Ops) -> ShortURLDTO:
	return op.create_short_url(body.model_dump(exclude_unset=True))


@router.get("/short-urls/{code:path}", response_model_exclude_unset=True)
def get_link(code: str, op: Ops, domain: str | None = None) -> ShortURLDTO:
	return op.short_url(code, domain)


@router.patch("/short-urls/{code:path}", response_model_exclude_unset=True)
def edit_link(code: str, body: EditLink, op: Ops, domain: str | None = None) -> ShortURLDTO:
	return op.update_short_url(code, body.model_dump(exclude_unset=True), domain)


@router.delete("/short-urls/{code:path}", status_code=204)
def delete_link(code: str, op: Ops, domain: str | None = None):
	op.delete_short_url(code, domain)
	return Response(status_code=204)


@router.get("/short-urls/{code:path}/redirect-rules", response_model_exclude_unset=True)
def get_rules(code: str, op: Ops, domain: str | None = None) -> RedirectRulesDTO:
	return op.rules(code, domain)


@router.post("/short-urls/{code:path}/redirect-rules", response_model_exclude_unset=True)
def set_rules(code: str, body: Rules, op: Ops, domain: str | None = None) -> RedirectRulesDTO:
	return op.rules(code, domain, [rule.model_dump() for rule in (body.redirectRules or [])])


@router.get("/short-urls/{code:path}/visits", response_model_exclude_unset=True)
def link_visits(
	code: str, request: Request, op: Ops, domain: str | None = None
) -> PageDTO[VisitDTO]:
	return op.link_visits(code, domain, query(request))


@router.delete("/short-urls/{code:path}/visits", response_model_exclude_unset=True)
def delete_link_visits(code: str, op: Ops, domain: str | None = None) -> DeletedVisitsDTO:
	return {"deletedVisits": op.delete_visits(code, domain)}


@router.get("/domains", response_model_exclude_unset=True)
def domains(op: Ops) -> DataListDTO[DomainDTO]:
	return {"data": op.domains()}


@router.post("/domains", status_code=201, response_model_exclude_unset=True)
def create_domain(body: CreateDomain, op: Ops) -> DomainDTO:
	return op.create_domain(body.domain)


@router.patch("/domains/redirects", response_model_exclude_unset=True)
def domain_redirects(body: DomainRedirects, op: Ops) -> DomainDTO:
	return op.domain_redirects(body.model_dump())


@router.delete("/domains/{authority}", status_code=204)
def delete_domain(authority: str, op: Ops):
	op.delete_domain(authority)
	return Response(status_code=204)


@router.get("/domains/{authority}/visits", response_model_exclude_unset=True)
def domain_visits(authority: str, request: Request, op: Ops) -> PageDTO[VisitDTO]:
	return op.domain_visits(authority, query(request))


@router.get("/tags", response_model_exclude_unset=True)
def tags(request: Request, op: Ops) -> PageDTO[str] | PageDTO[TagStatsDTO]:
	return op.tags(query(request), boolean(request.query_params.get("withStats")))


@router.put("/tags", response_model_exclude_unset=True)
def rename_tag(body: RenameTag, op: Ops) -> TagRenameDTO:
	return op.rename_tag(body.oldName, body.newName)


@router.delete("/tags", response_model_exclude_unset=True)
def delete_tags(request: Request, op: Ops) -> DeletedTagsDTO:
	return {"deletedTags": op.delete_tags(query(request).get("tags", []))}


@router.get("/tags/{tag}/visits", response_model_exclude_unset=True)
def tag_visits(tag: str, request: Request, op: Ops) -> PageDTO[VisitDTO]:
	return op.tag_visits(tag, query(request))


@router.get("/visits", response_model_exclude_unset=True)
def overview(op: Ops) -> VisitOverviewDTO:
	return op.overview()


@router.get("/visits/non-orphan", response_model_exclude_unset=True)
def visits(request: Request, op: Ops) -> PageDTO[VisitDTO]:
	return op.visits(query(request))


@router.get("/visits/orphan", response_model_exclude_unset=True)
def orphan_visits(request: Request, op: Ops) -> PageDTO[VisitDTO]:
	return op.visits(query(request), True, request.query_params.get("type"))


@router.delete("/visits/orphan", response_model_exclude_unset=True)
def delete_orphan_visits(op: Ops) -> DeletedVisitsDTO:
	return {"deletedVisits": op.delete_visits()}


@router.get("/stats/visits-per-day", response_model_exclude_unset=True)
def visits_per_day(request: Request, op: Ops) -> DataListDTO[DayCountDTO]:
	return {"data": op.stats(query(request))}


@router.get("/stats/breakdown", response_model_exclude_unset=True)
def breakdown(request: Request, op: Ops) -> DataListDTO[BreakdownCountDTO]:
	return {
		"data": op.stats(
			query(request),
			request.query_params.get("by", ""),
			request.query_params.get("limit", 25),
		)
	}


@router.get("/api-keys", response_model_exclude_unset=True)
def keys(op: Ops) -> DataListDTO[APIKeyDTO]:
	return {"data": op.keys()}


@router.post("/api-keys", status_code=201, response_model_exclude_unset=True)
def create_key(body: CreateKey, op: Ops) -> CreatedAPIKeyDTO:
	return op.create_key(body.model_dump())


@router.patch("/api-keys/{id}", response_model_exclude_unset=True)
def set_key_enabled(id: str, body: Enabled, op: Ops) -> IDEnabledDTO:
	return op.set_enabled(m.APIKey, id, bool(body.enabled))


@router.delete("/api-keys/{id}", status_code=204)
def delete_key(id: str, op: Ops):
	op.delete_managed(m.APIKey, id)
	return Response(status_code=204)


@router.get("/webhooks", response_model_exclude_unset=True)
def webhooks(op: Ops) -> DataListDTO[WebhookDTO]:
	return {"data": op.webhooks()}


@router.post("/webhooks", status_code=201, response_model_exclude_unset=True)
def create_webhook(body: CreateWebhook, op: Ops) -> CreatedWebhookDTO:
	return op.create_webhook(body.model_dump())


@router.patch("/webhooks/{id}", response_model_exclude_unset=True)
def set_webhook_enabled(id: str, body: Enabled, op: Ops) -> IDEnabledDTO:
	return op.set_enabled(m.Webhook, id, bool(body.enabled))


@router.delete("/webhooks/{id}", status_code=204)
def delete_webhook(id: str, op: Ops):
	op.delete_managed(m.Webhook, id)
	return Response(status_code=204)


# Slash-containing slugs are valid. Match visit/rule suffixes before the greedy slug.
router.routes.sort(key=lambda route: route.path.endswith("/{code:path}"))

_API_TAGS = {
	"short-urls": "Short URLs",
	"api-keys": "API keys",
	"domains": "Domains",
	"stats": "Statistics",
	"tags": "Tags",
	"visits": "Visits",
	"webhooks": "Webhooks",
}
for _route in router.routes:
	_route.tags = [_API_TAGS[_route.path.split("/")[3]]]

# Keep query values as strings: invalid dates are ignored and invalid numbers use defaults.
# Describe them explicitly so the REST explorer still offers its query controls.
_PAGE_PARAMETERS = ["page", "itemsPerPage"]
_DATE_PARAMETERS = ["startDate", "endDate"]
_VISIT_PARAMETERS = _PAGE_PARAMETERS + _DATE_PARAMETERS + ["excludeBots"]
_LIST_PARAMETERS = (
	_PAGE_PARAMETERS
	+ _DATE_PARAMETERS
	+ [
		"searchTerm",
		"tags",
		"tags[]",
		"tagsMode",
		"group",
		"domain",
		"orderBy",
		"excludeMaxVisitsReached",
		"excludePastValidUntil",
	]
)
_STATS_PARAMETERS = _DATE_PARAMETERS + ["shortCode", "domain", "tag", "orphan"]
for _route in router.routes:
	_parameters = []
	if _route.name == "list_links":
		_parameters = _LIST_PARAMETERS
	elif _route.name in ("link_visits", "domain_visits", "tag_visits", "visits"):
		_parameters = _VISIT_PARAMETERS
	elif _route.name == "orphan_visits":
		_parameters = _VISIT_PARAMETERS + ["type"]
	elif _route.name == "tags":
		_parameters = _PAGE_PARAMETERS + ["searchTerm", "withStats"]
	elif _route.name == "delete_tags":
		_parameters = ["tags", "tags[]"]
	elif _route.name == "visits_per_day":
		_parameters = _STATS_PARAMETERS
	elif _route.name == "breakdown":
		_parameters = _STATS_PARAMETERS + ["by", "limit"]
	_route.openapi_extra = {
		"parameters": [
			{
				"name": name,
				"in": "query",
				"required": False,
				"schema": {"type": "array", "items": {"type": "string"}}
				if name in ("tags", "tags[]")
				else {"type": "string"},
				**({"style": "form", "explode": True} if name in ("tags", "tags[]") else {}),
			}
			for name in _parameters
		]
	}
	for _status in (400, 401, 403, 404, 409, 429, 500):
		_route.responses[_status] = {
			"description": {
				400: "Invalid data",
				401: "Authentication required",
				403: "Forbidden",
				404: "Not found",
				409: "Conflict",
				429: "Too many requests",
				500: "Internal server error",
			}[_status],
			"content": {
				"application/problem+json": {
					"schema": {
						"type": "object",
						"required": ["type", "title", "detail", "status"],
						"properties": {
							name: {"type": "integer" if name == "status" else "string"}
							for name in ("type", "title", "detail", "status")
						},
					}
				}
			},
		}
