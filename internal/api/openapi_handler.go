package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// handleOpenAPI serves an OpenAPI 3 description GENERATED from the Access/route
// registry (5.1), so scopes-per-operation, auth, and error codes are always in
// lockstep with what the enforce layer actually checks — never a hand-written
// spec that drifts. Point Swagger UI or an OpenAPI client generator at
// /api/v1/openapi.json; authenticate with Authorization: Bearer apt_… (a scoped
// PAT) or a session cookie.
func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	b, _ := json.MarshalIndent(s.generateOpenAPI(), "", "  ")
	_, _ = w.Write(b)
}

var pathParamRe = regexp.MustCompile(`\{([^}]+)\}`)

// pagedCollections are GET routes that accept keyset pagination (T2.4).
var pagedCollections = map[string]bool{
	"/events": true, "/system/audit": true, "/cameras/{id}/segments": true,
}

func (s *Server) generateOpenAPI() map[string]any {
	paths := map[string]any{}
	// Deterministic order so the generated document is stable across requests.
	keys := make([]string, 0, len(s.routeSpecs))
	for k := range s.routeSpecs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		a := s.routeSpecs[key]
		sp := strings.SplitN(key, " ", 2)
		method, path := strings.ToLower(sp[0]), sp[1]

		op := map[string]any{
			"operationId": method + strings.NewReplacer("/", "_", "{", "", "}", "", ".", "_").Replace(path),
			"responses":   openAPIResponses(method, path, a),
		}
		if !a.public {
			// A scoped operation lists the scopes a token must hold; session/CLI
			// credentials (cookie/bearer CLI) bypass scopes.
			op["security"] = []map[string]any{
				{"bearerAuth": a.scopes},
				{"cookieAuth": []string{}},
			}
			if len(a.scopes) > 0 {
				op["x-required-scopes"] = a.scopes
			}
			if a.admin {
				op["x-requires-admin"] = true
			}
			if a.sessionOnly {
				op["x-session-only"] = true // token credentials are refused
			}
			if a.camera == camScopeParam {
				op["x-camera-scoped"] = true
			}
		}
		params := openAPIPathParams(path)
		if method == "get" && pagedCollections[path] {
			params = append(params,
				map[string]any{"name": "limit", "in": "query", "schema": map[string]any{"type": "integer", "default": 100, "maximum": 1000}},
				map[string]any{"name": "cursor", "in": "query", "schema": map[string]any{"type": "string"}, "description": "Opaque keyset cursor from a prior X-Next-Cursor header."},
			)
		}
		if len(params) > 0 {
			op["parameters"] = params
		}

		entry, _ := paths[path].(map[string]any)
		if entry == nil {
			entry = map[string]any{}
			paths[path] = entry
		}
		entry[method] = op
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":       "Aperture API",
			"version":     "v1",
			"description": "Self-hosted NVR HTTP API. Generated from the authorization registry: the scopes listed per operation are exactly those the server enforces. Errors share a machine-readable shape (see components.schemas.Error). Collections use keyset pagination via ?limit/?cursor and the X-Next-Cursor response header. Config writes support optimistic concurrency via ETag/If-Match (required for token credentials).\n\n**Request bodies are strict; responses are not.** A request may contain ONLY the fields defined for that operation — any unknown field is rejected with 400 (code `validation`) and the message names the offending field. Responses are the opposite: they may gain new fields at any time WITHOUT a version bump, so a client must tolerate unknown response fields. Consequently, do NOT round-trip a whole GET response back into a PUT/POST: server-emitted read-only fields (for example the per-item `etag` on list responses, or `id`/`createdAt`) are not accepted in a request body and will 400. Send only the fields you intend to write; the resource id travels in the URL and the ETag travels in the If-Match header, never in the body.",
		},
		"servers": []map[string]any{{"url": "/api/v1"}},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{"type": "http", "scheme": "bearer", "description": "A scoped Personal Access Token (apt_…). The array value lists the scopes this operation requires."},
				"cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": "aperture_session", "description": "First-party web session; bypasses scope checks."},
			},
			"schemas": map[string]any{
				"Error": map[string]any{
					"type":     "object",
					"required": []string{"error"},
					"properties": map[string]any{
						"error":   map[string]any{"type": "string", "description": "Human-readable message."},
						"code":    map[string]any{"type": "string", "description": "Machine-readable code: validation, unauthenticated, forbidden, forbidden.scope, forbidden.camera, not_found, method_not_allowed, conflict, precondition_required, precondition_failed, idempotency_conflict, rate_limited, payload_too_large, unavailable, internal."},
						"details": map[string]any{"type": "object"},
					},
				},
			},
		},
		"paths": paths,
	}
}

func openAPIPathParams(path string) []map[string]any {
	var out []map[string]any
	for _, m := range pathParamRe.FindAllStringSubmatch(path, -1) {
		out = append(out, map[string]any{
			"name": m[1], "in": "path", "required": true,
			"schema": map[string]any{"type": "string"},
		})
	}
	return out
}

// openAPIResponses derives the plausible response set from the method + access
// spec: every non-public route can 401/403; writes can 400 and (for config
// resources) 412/428; anything with a path param can 404.
func openAPIResponses(method, path string, a access) map[string]any {
	errRef := map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Error"}}}
	err := func(desc string) map[string]any { return map[string]any{"description": desc, "content": errRef} }

	resp := map[string]any{}
	switch method {
	case "post":
		resp["201"] = map[string]any{"description": "Created"}
		resp["200"] = map[string]any{"description": "OK"}
	case "delete":
		resp["204"] = map[string]any{"description": "No Content"}
	default:
		resp["200"] = map[string]any{"description": "OK"}
	}
	if !a.public {
		resp["401"] = err("Unauthenticated")
		if a.admin || len(a.scopes) > 0 || a.camera == camScopeParam || a.sessionOnly {
			resp["403"] = err("Forbidden (role, scope, camera, or session-only)")
		}
		resp["429"] = err("Rate limited")
	}
	if method == "post" || method == "put" || method == "delete" {
		resp["400"] = err("Validation error")
		// Config writes carry optimistic-concurrency preconditions (1.9).
		if method != "post" {
			resp["412"] = err("If-Match does not match the current resource")
			resp["428"] = err("If-Match required (token credentials)")
		}
	}
	if strings.Contains(path, "{") {
		resp["404"] = err("Not found")
	}
	return resp
}
