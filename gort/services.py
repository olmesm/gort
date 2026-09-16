"""Application operations shared by REST, GraphQL and the dashboard."""

import json
import re
from datetime import datetime

from pydantic import ValidationError
from sqlalchemy import delete, func, or_
from sqlalchemy.exc import IntegrityError
from sqlmodel import Session, select

from gort.domain import (
	ServiceError,
	ShortURLSpec,
	generate_short_code,
	normalize_group,
	parse_date,
	validate_long_url,
)
from gort.models import (
	APIKey,
	Domain,
	RedirectCondition,
	RedirectRule,
	ShortURL,
	ShortURLTag,
	Tag,
	Visit,
	Webhook,
	WebhookDelivery,
	utcnow,
)


def integer(value, default=0):
	if isinstance(value, int) and not isinstance(value, bool):
		return value if -(2**63) <= value < 2**63 else default
	if not isinstance(value, str) or not re.fullmatch(r"[+-]?[0-9]+", value):
		return default
	try:
		number = int(value)
	except ValueError:
		return default
	return number if -(2**63) <= number < 2**63 else default


def truthy(value):
	return value is True or isinstance(value, str) and value.lower() in ("true", "1", "yes")


def pagination(items: list, total: int, page: int, size: int) -> dict:
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


def paginate(session: Session, statement, filters: dict, mapper=lambda x: x):
	page = max(1, integer(filters.get("page"), 1))
	size = integer(filters.get("itemsPerPage"), 20)
	size = min(500, size) if size > 0 else 20
	total = session.exec(
		select(func.count()).select_from(statement.order_by(None).subquery())
	).one()
	page = min(page, max(1, (total + size - 1) // size))
	rows = session.exec(statement.limit(size).offset((page - 1) * size)).all()
	return pagination([mapper(row) for row in rows], total, page, size)


def resolve_named_domain(session: Session, authority: str | None = None) -> Domain | None:
	query = select(Domain)
	query = (
		query.where(Domain.authority == authority.strip().lower())
		if authority
		else query.where(Domain.is_default.is_(True))
	)  # noqa: E712
	return session.exec(query).first()


def resolve_request_domain(session: Session, authority: str) -> Domain | None:
	return resolve_named_domain(session, authority) or resolve_named_domain(session)


def find_short_url(
	session: Session, settings, code: str, domain: str | None = None
) -> ShortURL | None:
	target = resolve_named_domain(session, domain)
	return (
		session.exec(
			select(ShortURL).where(ShortURL.domain_id == target.id, ShortURL.short_code == code)
		).first()
		if target
		else None
	)


def tags_for_short_url(session: Session, link_id: int) -> list[str]:
	return list(
		session.exec(
			select(Tag.name)
			.join(ShortURLTag, Tag.id == ShortURLTag.tag_id)
			.where(ShortURLTag.short_url_id == link_id)
			.order_by(Tag.name)
		).all()
	)


def count_valid_visits(session: Session, link_id: int) -> int:
	return session.exec(
		select(func.count())
		.select_from(Visit)
		.where(Visit.short_url_id == link_id, Visit.visit_type == "valid")
	).one()


def short_url_dto(session: Session, settings, link: ShortURL) -> dict:
	domain = session.get(Domain, link.domain_id)
	total = count_valid_visits(session, link.id)
	bots = session.exec(
		select(func.count())
		.select_from(Visit)
		.where(Visit.short_url_id == link.id, Visit.visit_type == "valid", Visit.is_bot.is_(True))
	).one()  # noqa: E712
	result = {
		"shortCode": link.short_code,
		"shortUrl": settings.short_url_base(domain.authority) + "/" + link.short_code,
		"domain": domain.authority,
		"longUrl": link.long_url,
		"dateCreated": link.created_at,
		"tags": tags_for_short_url(session, link.id),
		"meta": {
			name: value
			for name, value in {
				"validSince": link.valid_since,
				"validUntil": link.valid_until,
				"maxVisits": link.max_visits,
			}.items()
			if value is not None
		},
		"visitsSummary": {"total": total, "nonBots": total - bots, "bots": bots},
		"forwardQuery": link.forward_query,
		"crawlable": link.crawlable,
		"redirectStatus": link.redirect_status,
	}
	if link.title is not None:
		result["title"] = link.title
	if link.group_name is not None:
		result["group"] = link.group_name
	return result


def domain_dto(domain: Domain) -> dict:
	return {
		"domain": domain.authority,
		"isDefault": domain.is_default,
		"redirects": {
			key: value
			for key, value in {
				"baseUrlRedirect": domain.base_url_redirect,
				"regular404Redirect": domain.regular_404_redirect,
				"invalidShortUrlRedirect": domain.invalid_short_url_redirect,
			}.items()
			if value is not None
		},
	}


def _spec(payload: dict, settings) -> ShortURLSpec:
	data = {"redirectStatus": settings.default_redirect_status, **payload}
	# Optional creation values may arrive as JSON null, meaning use the default.
	for key in ("redirectStatus", "forwardQuery", "crawlable", "tags", "findIfExists"):
		if data.get(key) is None:
			data.pop(key, None)
	data.setdefault("redirectStatus", settings.default_redirect_status)
	try:
		return ShortURLSpec.model_validate(data)
	except ValidationError as error:
		raise ServiceError(
			"; ".join(item["msg"].removeprefix("Value error, ") for item in error.errors())
		) from error


def _replace_tags(session: Session, link_id: int, names: list[str]):
	session.exec(delete(ShortURLTag).where(ShortURLTag.short_url_id == link_id))
	for name in names:
		tag = session.exec(select(Tag).where(Tag.name == name)).first()
		if tag is None:
			try:
				with session.begin_nested():
					tag = Tag(name=name)
					session.add(tag)
					session.flush()
			except IntegrityError:
				tag = session.exec(select(Tag).where(Tag.name == name)).one()
		session.add(ShortURLTag(short_url_id=link_id, tag_id=tag.id))


def publish_event(session: Session, settings, event: str, data: dict):
	if not settings.webhooks_enabled:
		return

	def encode(value):
		if isinstance(value, datetime):
			return value.isoformat().replace("+00:00", "Z")
		raise TypeError(f"Cannot serialize {type(value)}")

	payload = json.dumps(
		{"event": event, "occurredAt": utcnow(), "data": data},
		default=encode,
		separators=(",", ":"),
	)
	for webhook in session.exec(select(Webhook).where(Webhook.enabled.is_(True))).all():  # noqa: E712
		if event in {name.strip() for name in webhook.events.split(",")}:
			session.add(WebhookDelivery(webhook_id=webhook.id, event=event, payload=payload))


def create_short_url(
	session: Session,
	settings,
	payload: dict,
	author_user_id: int | None = None,
	author_api_key_id: int | None = None,
) -> ShortURL:
	spec = _spec(payload, settings)
	target = resolve_named_domain(session, spec.domain)
	if target is None and spec.domain:
		try:
			with session.begin_nested():
				target = Domain(authority=spec.domain)
				session.add(target)
				session.flush()
		except IntegrityError:
			target = resolve_named_domain(session, spec.domain)
	if target is None:
		raise ServiceError("The domain is not registered.")
	if spec.find_if_exists:
		query = select(ShortURL).where(
			ShortURL.domain_id == target.id, ShortURL.long_url == spec.long_url
		)
		key = session.get(APIKey, author_api_key_id) if author_api_key_id else None
		if key and key.role == "author":
			query = query.where(ShortURL.author_api_key_id == key.id)
		if existing := session.exec(query.order_by(ShortURL.id)).first():
			return existing
	fields = spec.model_dump(
		include={
			"long_url",
			"title",
			"valid_since",
			"valid_until",
			"max_visits",
			"redirect_status",
			"forward_query",
			"crawlable",
		}
	)
	for attempt in range(10):
		code = spec.custom_slug or generate_short_code(
			spec.short_code_length or settings.short_code_length
		)
		try:
			with session.begin_nested():
				link = ShortURL(
					**fields,
					short_code=code,
					domain_id=target.id,
					group_name=spec.group,
					author_user_id=author_user_id,
					author_api_key_id=author_api_key_id,
				)
				session.add(link)
				session.flush()
				_replace_tags(session, link.id, spec.tags)
			break
		except IntegrityError as error:
			if not session.exec(
				select(ShortURL.id).where(
					ShortURL.domain_id == target.id, ShortURL.short_code == code
				)
			).first():
				raise
			if spec.custom_slug:
				raise ServiceError(
					f"The slug '{code}' is already in use on domain '{target.authority}'.",
					409,
					"non-unique-slug",
				) from error
	else:
		raise ServiceError(
			"Could not find a free short code; try again or use a custom slug.",
			500,
			"code-generation",
		)
	session.flush()
	publish_event(session, settings, "url.created", short_url_dto(session, settings, link))
	session.commit()
	session.refresh(link)
	return link


def update_short_url(session: Session, settings, link: ShortURL, payload: dict) -> ShortURL:
	merged = {
		"longUrl": link.long_url,
		"title": link.title,
		"group": link.group_name,
		"validSince": link.valid_since,
		"validUntil": link.valid_until,
		"maxVisits": link.max_visits,
		"redirectStatus": link.redirect_status,
		"forwardQuery": link.forward_query,
		"crawlable": link.crawlable,
		**payload,
	}
	# Null disables booleans; empty URLs and zero redirect statuses fail validation.
	for key, zero in {
		"longUrl": "",
		"redirectStatus": 0,
		"forwardQuery": False,
		"crawlable": False,
	}.items():
		if merged.get(key) is None:
			merged[key] = zero
	spec = _spec(merged, settings)
	unchanged_title = link.title == spec.title
	for field in (
		"long_url",
		"title",
		"valid_since",
		"valid_until",
		"max_visits",
		"redirect_status",
		"forward_query",
		"crawlable",
	):
		setattr(link, field, getattr(spec, field))
	link.group_name = spec.group
	link.title_was_auto_resolved = unchanged_title and link.title_was_auto_resolved
	session.add(link)
	if "tags" in payload:
		_replace_tags(session, link.id, spec.tags)
	session.commit()
	session.refresh(link)
	return link


def delete_short_url(session: Session, link: ShortURL):
	session.delete(link)
	session.commit()


def _filter_date(value):
	if isinstance(value, str) and not re.fullmatch(
		r"[0-9]{4}-[0-9]{2}-[0-9]{2}(?:T[0-9]{2}:[0-9]{2}|[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?|T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2}))?",
		value,
	):
		return None
	try:
		return parse_date(value)
	except (ValueError, TypeError):
		return None


def list_short_urls(
	session: Session, settings, filters: dict, domain_id=None, author_api_key_id=None, groups=None
) -> dict:
	query = select(ShortURL).join(Domain)
	visit_count = (
		select(func.count(Visit.id))
		.where(Visit.short_url_id == ShortURL.id, Visit.visit_type == "valid")
		.correlate(ShortURL)
		.scalar_subquery()
	)
	if domain_id is not None:
		query = query.where(ShortURL.domain_id == domain_id)
	if author_api_key_id is not None:
		query = query.where(ShortURL.author_api_key_id == author_api_key_id)
	if filters.get("domain"):
		query = query.where(Domain.authority == str(filters["domain"]).lower())
	if groups is not None:
		query = query.where(or_(ShortURL.group_name.is_(None), ShortURL.group_name.in_(groups)))  # noqa: E711
	if "group" in filters and filters["group"] is not None:
		group = normalize_group(filters["group"])
		query = query.where(ShortURL.group_name == (group or None))
	tag_links = select(ShortURLTag.short_url_id).join(Tag, Tag.id == ShortURLTag.tag_id)
	search = str(filters.get("searchTerm") or filters.get("search") or "").strip()
	if search:
		pattern = f"%{search}%"
		query = query.where(
			or_(
				ShortURL.long_url.ilike(pattern),
				ShortURL.title.ilike(pattern),
				ShortURL.short_code.ilike(pattern),
				Domain.authority.ilike(pattern),
				ShortURL.id.in_(tag_links.where(Tag.name.ilike(pattern))),
			)
		)
	tags = filters.get("tags") or filters.get("tags[]") or []
	if isinstance(tags, str):
		tags = [s.strip().lower() for s in tags.split(",") if s.strip()]
	tags = list(dict.fromkeys(tags))
	if tags:
		if str(filters.get("tagsMode", "")).lower() == "all" or truthy(filters.get("tagsMatchAll")):
			for tag in tags:
				query = query.where(ShortURL.id.in_(tag_links.where(Tag.name == tag)))
		else:
			query = query.where(ShortURL.id.in_(tag_links.where(Tag.name.in_(tags))))
	for name, operator in (
		("startDate", ShortURL.created_at.__ge__),
		("endDate", ShortURL.created_at.__le__),
	):
		if date := _filter_date(filters.get(name)):
			query = query.where(operator(date))
	if truthy(filters.get("excludeMaxVisitsReached")):
		query = query.where(or_(ShortURL.max_visits.is_(None), visit_count < ShortURL.max_visits))  # noqa: E711
	if truthy(filters.get("excludePastValidUntil")):
		query = query.where(or_(ShortURL.valid_until.is_(None), ShortURL.valid_until >= utcnow()))  # noqa: E711
	order = filters.get("orderBy") or "dateCreated-DESC"
	if isinstance(order, dict):
		order = "-".join(next(iter(order.items())))
	parts = str(order).split("-")
	column = {
		"dateCreated": ShortURL.created_at,
		"shortCode": ShortURL.short_code,
		"longUrl": ShortURL.long_url,
		"title": ShortURL.title,
		"visits": visit_count,
	}.get(parts[0], ShortURL.created_at)
	descending = len(parts) == 2 and parts[-1].upper() == "DESC"
	query = query.order_by(
		column.desc() if descending else column.asc(),
		ShortURL.id.desc() if descending else ShortURL.id.asc(),
	)
	return paginate(session, query, filters, lambda link: short_url_dto(session, settings, link))


def get_redirect_rules(session: Session, short_url_id: int) -> list[dict]:
	result = []
	for rule in session.exec(
		select(RedirectRule)
		.where(RedirectRule.short_url_id == short_url_id)
		.order_by(RedirectRule.priority, RedirectRule.id)
	).all():
		conditions = []
		for condition in session.exec(
			select(RedirectCondition)
			.where(RedirectCondition.rule_id == rule.id)
			.order_by(RedirectCondition.id)
		).all():
			item = {"type": condition.cond_type, "matchValue": condition.match_value}
			if condition.match_key is not None:
				item["matchKey"] = condition.match_key
			conditions.append(item)
		result.append(
			{"longUrl": rule.long_url, "priority": rule.priority, "conditions": conditions}
		)
	return result


def get_rules(session: Session, link: ShortURL) -> dict:
	return {"defaultLongUrl": link.long_url, "redirectRules": get_redirect_rules(session, link.id)}


def set_redirect_rules(session: Session, short_url_id: int, rules: list[dict]):
	validated = []
	for index, item in enumerate(rules):
		try:
			target = validate_long_url(item.get("longUrl", ""))
			conditions = [dict(condition) for condition in item.get("conditions") or []]
			if not conditions:
				raise ValueError("At least one condition is required.")
			for condition in conditions:
				kind, value = condition.get("type"), condition.get("matchValue")
				if kind not in ("device", "language", "query-param", "ip-address"):
					raise ValueError(
						"Condition type must be device, language, query-param or ip-address."
					)
				if not isinstance(value, str) or kind != "query-param" and not value.strip():
					raise ValueError("A condition matchValue is required.")
				if kind == "device" and value not in ("desktop", "mobile", "android", "ios"):
					raise ValueError("Device matchValue must be android, ios, mobile or desktop.")
				if kind == "query-param":
					key = condition.get("matchKey")
					if not isinstance(key, str) or not key.strip():
						raise ValueError("Query-param conditions require a nonempty matchKey.")
					condition["matchKey"] = key.strip()
				elif kind in ("language", "ip-address"):
					condition["matchValue"] = value.strip()
			validated.append((target, conditions))
		except ValueError as error:
			raise ServiceError(f"Rule {index + 1}: {error}") from error
	session.exec(delete(RedirectRule).where(RedirectRule.short_url_id == short_url_id))
	for index, (target, conditions) in enumerate(validated, 1):
		row = RedirectRule(short_url_id=short_url_id, priority=index, long_url=target)
		session.add(row)
		session.flush()
		for condition in conditions:
			session.add(
				RedirectCondition(
					rule_id=row.id,
					cond_type=condition["type"],
					match_key=condition.get("matchKey")
					if condition["type"] == "query-param"
					else None,
					match_value=condition["matchValue"],
				)
			)
	session.commit()


def set_rules(session: Session, link: ShortURL, rules: list[dict]) -> dict:
	set_redirect_rules(session, link.id, rules)
	return get_rules(session, link)


def visit_dto(visit: Visit) -> dict:
	result = {"date": visit.visited_at, "potentialBot": visit.is_bot}
	for key, field in {
		"referer": "referer",
		"userAgent": "user_agent",
		"browser": "browser",
		"os": "os",
		"device": "device",
		"visitedUrl": "visited_url",
	}.items():
		if (value := getattr(visit, field)) is not None:
			result[key] = value
	return result


def list_visits(
	session: Session, filters: dict, short_url_id=None, domain_id=None, tag=None, orphan=None
) -> dict:
	query = select(Visit)
	if short_url_id is not None:
		query = query.where(Visit.short_url_id == short_url_id, Visit.visit_type == "valid")
	if domain_id is not None or tag is not None:
		query = query.join(ShortURL, Visit.short_url_id == ShortURL.id).where(
			Visit.visit_type == "valid"
		)
	if domain_id is not None:
		query = query.where(ShortURL.domain_id == domain_id)
	if tag is not None:
		query = query.where(
			ShortURL.id.in_(
				select(ShortURLTag.short_url_id)
				.join(Tag, ShortURLTag.tag_id == Tag.id)
				.where(Tag.name == tag)
			)
		)
	if orphan is True:
		query = query.where(Visit.visit_type != "valid")
		if filters.get("type") in ("base_url", "invalid_short_url", "regular_404"):
			query = query.where(Visit.visit_type == filters["type"])
	elif orphan is False:
		query = query.where(Visit.visit_type == "valid")
	for name, operator in (
		("startDate", Visit.visited_at.__ge__),
		("endDate", Visit.visited_at.__le__),
	):
		if date := _filter_date(filters.get(name)):
			query = query.where(operator(date))
	if truthy(filters.get("excludeBots")):
		query = query.where(Visit.is_bot.is_(False))  # noqa: E712
	return paginate(
		session, query.order_by(Visit.visited_at.desc(), Visit.id.desc()), filters, visit_dto
	)


def overview(session: Session) -> dict:
	def count(model, *conditions):
		return session.exec(select(func.count()).select_from(model).where(*conditions)).one()

	return {
		"visitsCount": count(Visit, Visit.visit_type == "valid"),
		"orphanVisitsCount": count(Visit, Visit.visit_type != "valid"),
		"shortUrlsCount": count(ShortURL),
		"tagsCount": count(Tag),
		"botVisitsCount": count(Visit, Visit.visit_type == "valid", Visit.is_bot.is_(True)),
	}  # noqa: E712
