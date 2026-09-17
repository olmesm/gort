"""Schema-first GraphQL transport backed by the same operations as REST."""

from __future__ import annotations

import json
from datetime import datetime
from pathlib import Path

from fastapi import APIRouter, Request, Response
from fastapi.encoders import jsonable_encoder
from fastapi.responses import JSONResponse, PlainTextResponse
from graphql import (
	GraphQLError,
	build_schema,
	execute_sync,
	get_operation_ast,
	parse,
	validate,
	value_from_ast_untyped,
)
from graphql.execution.values import get_variable_values
from graphql.language import ast
from starlette.concurrency import run_in_threadpool

from . import models as m
from .api import Ops, Problem, date, paging
from .services import ServiceError

SCHEMA_TEXT = Path(__file__).with_name("schema.graphql").read_text()
schema = build_schema(SCHEMA_TEXT)
router = APIRouter()


def parse_time(value):
	if not isinstance(value, (str, datetime)):
		raise GraphQLError("Time must be an ISO 8601 timestamp.")
	parsed = date(value)
	if parsed is None:
		raise GraphQLError("Time must be an ISO 8601 timestamp.")
	return parsed


def parse_long(value):
	if isinstance(value, bool) or not isinstance(value, int) or not -(2**63) <= value < 2**63:
		raise GraphQLError("Long must be a signed 64-bit integer.")
	return value


schema.type_map["Time"].serialize = lambda value: (
	value.isoformat().replace("+00:00", "Z") if isinstance(value, datetime) else value
)
schema.type_map["Time"].parse_value = parse_time
schema.type_map["Time"].parse_literal = lambda node, variables=None: parse_time(
	value_from_ast_untyped(node, variables)
)
schema.type_map["Long"].serialize = parse_long
schema.type_map["Long"].parse_value = parse_long
schema.type_map["Long"].parse_literal = lambda node, variables=None: parse_long(
	value_from_ast_untyped(node, variables)
)


def bind(type_name, field_name, resolver):
	def resolve(source, info, **kwargs):
		return resolver(info.context, source, **kwargs)

	schema.type_map[type_name].fields[field_name].resolve = resolve


bind("Query", "shortURLs", lambda op, _, filter=None: op.short_urls(filter))
bind("Query", "shortURL", lambda op, _, code, domain=None: op.short_url(code, domain))
bind("Query", "tags", lambda op, _, **filters: op.tags(filters, True))
bind("Query", "domains", lambda op, _: op.domains())
bind("Query", "visitsOverview", lambda op, _: op.overview())
bind("Query", "visits", lambda op, _, filter=None: op.visits(filter))
bind("Query", "orphanVisits", lambda op, _, type=None, filter=None: op.visits(filter, True, type))
bind("Query", "tagVisits", lambda op, _, tag, filter=None: op.tag_visits(tag, filter))
bind(
	"Query",
	"domainVisits",
	lambda op, _, authority, filter=None: op.domain_visits(authority, filter),
)
bind("Query", "visitsPerDay", lambda op, _, scope=None: op.stats(scope))
bind("Query", "breakdown", lambda op, _, by, scope=None, limit=25: op.stats(scope, by, limit))
bind("Query", "apiKeys", lambda op, _: op.keys())
bind("Query", "webhooks", lambda op, _: op.webhooks())
bind("Mutation", "createShortURL", lambda op, _, input: op.create_short_url(input))
bind(
	"Mutation",
	"updateShortURL",
	lambda op, _, code, input, domain=None: op.update_short_url(code, input, domain),
)
bind(
	"Mutation", "deleteShortURL", lambda op, _, code, domain=None: op.delete_short_url(code, domain)
)
bind(
	"Mutation",
	"setRedirectRules",
	lambda op, _, code, rules, domain=None: op.rules(code, domain, rules),
)
bind(
	"Mutation",
	"deleteShortURLVisits",
	lambda op, _, code, domain=None: op.delete_visits(code, domain),
)
bind("Mutation", "renameTag", lambda op, _, oldName, newName: op.rename_tag(oldName, newName))
bind("Mutation", "deleteTags", lambda op, _, tags: op.delete_tags(tags))
bind("Mutation", "createDomain", lambda op, _, domain: op.create_domain(domain))
bind("Mutation", "updateDomainRedirects", lambda op, _, input: op.domain_redirects(input))
bind("Mutation", "deleteDomain", lambda op, _, authority: op.delete_domain(authority))
bind("Mutation", "deleteOrphanVisits", lambda op, _: op.delete_visits())
bind("Mutation", "createAPIKey", lambda op, _, input: op.create_key(input))
bind(
	"Mutation", "setAPIKeyEnabled", lambda op, _, id, enabled: op.set_enabled(m.APIKey, id, enabled)
)
bind("Mutation", "deleteAPIKey", lambda op, _, id: op.delete_managed(m.APIKey, id))
bind("Mutation", "createWebhook", lambda op, _, input: op.create_webhook(input))
bind(
	"Mutation",
	"setWebhookEnabled",
	lambda op, _, id, enabled: op.set_enabled(m.Webhook, id, enabled),
)
bind("Mutation", "deleteWebhook", lambda op, _, id: op.delete_managed(m.Webhook, id))
bind(
	"ShortURL",
	"visits",
	lambda op, source, filter=None: op.link_visits(source["shortCode"], source["domain"], filter),
)
bind(
	"ShortURL", "redirectRules", lambda op, source: op.rules(source["shortCode"], source["domain"])
)
# One-time secrets are non-null; subsequent queries return "".
bind("APIKey", "apiKey", lambda op, source: source.get("apiKey", ""))
bind("Webhook", "secret", lambda op, source: source.get("secret", ""))


def complexity(document, operation, variables):
	fragments = {
		definition.name.value: definition
		for definition in document.definitions
		if isinstance(definition, ast.FragmentDefinitionNode)
	}
	page_fields = {"shortURLs", "tags", "visits", "orphanVisits", "tagVisits", "domainVisits"}
	collections = {"domains", "apiKeys", "webhooks", "visitsPerDay"}

	def cost(selection_set):
		total = 0
		for selection in selection_set.selections:
			if isinstance(selection, ast.FragmentSpreadNode):
				total += cost(fragments[selection.name.value].selection_set)
				continue
			if isinstance(selection, ast.InlineFragmentNode):
				total += cost(selection.selection_set)
				continue
			child = cost(selection.selection_set) if selection.selection_set else 0
			name = selection.name.value
			arguments = {
				arg.name.value: value_from_ast_untyped(arg.value, variables)
				for arg in selection.arguments
			}
			if name in page_fields:
				filters = arguments if name == "tags" else (arguments.get("filter") or {})
				multiplier = paging(filters, 500 if name == "tags" else 20)[1]
			elif name in collections:
				multiplier = 500
			elif name == "breakdown":
				multiplier = max(1, min(100, arguments.get("limit") or 25))
			else:
				multiplier = 1
			total += 1 + multiplier * child
			if total > 10000:
				return total
		return total

	return cost(operation.selection_set)


def formatted_error(error):
	original = error.original_error
	if isinstance(original, (Problem, ServiceError)):
		result = {
			"message": str(original),
			"extensions": {"code": original.kind, "status": original.status_code},
		}
		if error.path:
			result["path"] = error.path
		return result
	if original is not None and not isinstance(original, GraphQLError):
		import logging

		logging.getLogger(__name__).error("GraphQL operation failed", exc_info=original)
		return {
			"message": "Something went wrong handling the request.",
			"path": error.path,
			"extensions": {"code": "internal", "status": 500},
		}
	return error.formatted


def execute(payload, op, method):
	source = payload.get("query")
	if not isinstance(source, str):
		return {"errors": [{"message": "A GraphQL query is required."}]}, 400
	variables = payload.get("variables")
	if variables is not None and not isinstance(variables, dict):
		return {"errors": [{"message": "variables must be an object."}]}, 400
	try:
		document = parse(source, max_tokens=10000)
		errors = validate(schema, document)
		if errors:
			return {"errors": [error.formatted for error in errors]}, 422
		operation = get_operation_ast(document, payload.get("operationName"))
		if operation is None:
			return {"errors": [{"message": "Choose a valid operationName."}]}, 422
		if method == "GET" and operation.operation.value != "query":
			return {"errors": [{"message": "Mutations must use POST."}]}, 406
		coerced = get_variable_values(schema, operation.variable_definitions, variables or {})
		if isinstance(coerced, list):
			return {"errors": [error.formatted for error in coerced]}, 422
		if complexity(document, operation, coerced) > 10000:
			return {"errors": [{"message": "operation has complexity over limit 10000"}]}, 422
		result = execute_sync(
			schema,
			document,
			context_value=op,
			variable_values=variables,
			operation_name=payload.get("operationName"),
		)
		body = {"data": result.data}
		if result.errors:
			body["errors"] = [formatted_error(error) for error in result.errors]
		return body, 200
	except GraphQLError as exc:
		return {"errors": [exc.formatted]}, 422
	except RecursionError:
		return {"errors": [{"message": "Query nesting exceeds the supported limit."}]}, 422


@router.api_route("/graphql", methods=["GET", "POST", "OPTIONS"], include_in_schema=False)
async def graphql_endpoint(request: Request, op: Ops):
	if request.method == "OPTIONS":
		return Response(
			status_code=200, headers={"Allow": "GET, POST, OPTIONS", "Cache-Control": "no-store"}
		)
	if request.method == "GET":
		payload = dict(request.query_params)
		if payload.get("variables"):
			try:
				payload["variables"] = json.loads(payload["variables"])
			except (TypeError, ValueError):
				raise Problem(400, "variables must contain valid JSON.")
	else:
		body = await request.body()
		if len(body) > 1 << 20:
			raise Problem(400, "Request body is too large.")
		try:
			payload = json.loads(body)
		except (ValueError, UnicodeDecodeError):
			raise Problem(400, "Request body must contain valid JSON.")
	if not isinstance(payload, dict):
		raise Problem(400, "Request body must be a JSON object.")
	result, status = await run_in_threadpool(execute, payload, op, request.method)
	return JSONResponse(
		jsonable_encoder(result), status_code=status, headers={"Cache-Control": "no-store"}
	)


@router.get("/graphql/schema.graphql", response_class=PlainTextResponse, include_in_schema=False)
def schema_document():
	return SCHEMA_TEXT
