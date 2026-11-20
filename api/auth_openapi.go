// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

import "strings"

// SessionCookieName is the session cookie name for a project:
// "<project name>_session".
func SessionCookieName(projectName string) string {
	return projectName + "_session"
}

// OpenAPISecuritySchemes returns the OpenAPI security schemes schemaf's auth
// accepts: bearerAuth (Authorization: Bearer <JWT>) and cookieAuth (the
// session cookie named cookieName), for the codegen spec.
func OpenAPISecuritySchemes(cookieName string) map[string]any {
	return map[string]any{
		"bearerAuth": map[string]any{
			"type":         "http",
			"scheme":       "bearer",
			"bearerFormat": "JWT",
		},
		"cookieAuth": map[string]any{
			"type": "apiKey",
			"in":   "cookie",
			"name": cookieName,
		},
	}
}

// OpenAPISecurity returns the OpenAPI security requirement for an auth level:
// either scheme for AuthRequired, anonymous or either scheme for
// AuthOptional, and nil (no security) for AuthNone.
func OpenAPISecurity(level AuthLevel) []any {
	bearer := map[string]any{"bearerAuth": []any{}}
	cookie := map[string]any{"cookieAuth": []any{}}
	switch level {
	case AuthRequired:
		return []any{bearer, cookie}
	case AuthOptional:
		return []any{map[string]any{}, bearer, cookie}
	}
	return nil
}

// OpenAPIErrorResponse returns the default response every operation
// declares: the shared Error schema (see ErrorSchemaName) as JSON.
func OpenAPIErrorResponse() map[string]any {
	return map[string]any{
		"description": "Error",
		"content": map[string]any{"application/json": map[string]any{
			"schema": map[string]any{"$ref": "#/components/schemas/" + ErrorSchemaName},
		}},
	}
}

// ErrorSchemaName is the name of the shared error schema in
// components.schemas.
const ErrorSchemaName = "Error"

// openAPIErrorSchema is the schema of every error response body.
func openAPIErrorSchema() map[string]any {
	codes := make([]string, 0, len(sentinels)+1+len(frameworkStatuses))
	for _, s := range sentinels {
		codes = append(codes, s.code)
	}
	codes = append(codes, codeInternal)
	for _, s := range frameworkStatuses {
		codes = append(codes, s.code)
	}
	return map[string]any{
		"type":     "object",
		"required": []any{"error", "code"},
		"properties": map[string]any{
			"error": map[string]any{"type": "string", "description": "The message. On 5xx always the status text."},
			"code": map[string]any{"type": "string", "description": "Machine-readable code: an app code, or the default code of the status (" +
				strings.Join(codes, ", ") + ")."},
			"id": map[string]any{"type": "string", "description": "On 5xx only: the id the full error is logged under."},
		},
	}
}

// FrameworkOpenAPI returns the OpenAPI path items of the framework's routes
// under /api/ (health, status, config, user/me, errors/{id} and the auth
// routes) and the framework's component schemas (including the shared Error schema), as
// plain JSON-shaped maps. Codegen merges them into gen/openapi.json (which
// the server serves and enforces, see NewMux), so the generated clients
// always have these routes and the error type, and the route gate lets them
// through. The auth0 login route is always included, independent of
// deployment config.
func FrameworkOpenAPI() (paths map[string]any, schemas map[string]any) {
	ref := func(name string) map[string]any {
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	jsonBody := func(schema map[string]any) map[string]any {
		return map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}
	jsonResp := func(desc string, schema map[string]any) map[string]any {
		return map[string]any{
			"description": desc,
			"content":     map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}
	loginResponses := func() map[string]any {
		return map[string]any{
			"200":     jsonResp("Session started; the session cookie is set", ref("SchemafLoginResponse")),
			"default": OpenAPIErrorResponse(),
		}
	}

	stringMap := map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}
	anyMap := map[string]any{"type": "object", "additionalProperties": true}
	// getOp is a GET operation with the given path parameters (nil for
	// none) and responses, plus the default error response.
	getOp := func(id, summary, desc string, level AuthLevel, params []any, responses map[string]any) map[string]any {
		responses["default"] = OpenAPIErrorResponse()
		op := map[string]any{
			"operationId": id,
			"summary":     summary,
			"description": desc,
			"responses":   responses,
		}
		if sec := OpenAPISecurity(level); sec != nil {
			op["security"] = sec
		}
		if params != nil {
			op["parameters"] = params
		}
		return map[string]any{"get": op}
	}

	paths = map[string]any{
		"/api/health": getOp("getApiHealth", "Health check",
			`"status" is "ok" or "error", "db" and every registered health check are "ok" or "error: …". 503 (with the same body) when anything is unhealthy.`,
			AuthNone, nil, map[string]any{
				"200": jsonResp("Healthy", stringMap),
				// Unhealthy: the same body, not the Error schema.
				"503": jsonResp("Unhealthy", stringMap),
			}),
		"/api/status": getOp("getApiStatus", "Service status",
			"Mode (development or production), uptime, last backup, active identity providers, and every registered status provider.",
			AuthNone, nil, map[string]any{"200": jsonResp("OK", anyMap)}),
		"/api/config": getOp("getApiConfig", "Public frontend config",
			"Every PUBLIC_* environment variable, keyed by its full name. Read it in the frontend through config() from config.gen.ts.",
			AuthNone, nil, map[string]any{"200": jsonResp("OK", stringMap)}),
		"/api/user/me": getOp("getApiUserMe", "The authenticated caller",
			"The subject of the caller.",
			AuthRequired, nil, map[string]any{"200": jsonResp("OK", ref("SchemafUser"))}),
		"/api/errors/{id}": getOp("getApiErrorsId", "Details of a server error",
			"The details stored for the 5xx answered with this id: open in development mode, in production only to callers the app allows (app.CanSeeErrorDetails). 404 when the id is unknown or expired (kept: newest 1000, at most 24 hours, per process).",
			AuthOptional, []any{map[string]any{
				"name": "id", "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			}}, map[string]any{"200": jsonResp("OK", ref("SchemafErrorDetails"))}),
		"/api/auth/auth0/login": map[string]any{
			"post": map[string]any{
				"operationId": "postApiAuthAuth0Login",
				"summary":     "Sign in with an Auth0 ID token",
				"description": "Verifies the ID token from the Auth0 SPA SDK and starts a schemaf session. 404 if Auth0 is not configured on this instance.",
				"requestBody": jsonBody(ref("SchemafAuth0LoginRequest")),
				"responses":   loginResponses(),
			},
		},
		"/api/auth/{provider}/login": map[string]any{
			"post": map[string]any{
				"operationId": "postApiAuthProviderLogin",
				"summary":     "Sign in with an identity provider",
				"description": "Passes the request to the named identity provider and starts a schemaf session. The body is defined by the provider.",
				"parameters": []any{map[string]any{
					"name": "provider", "in": "path", "required": true,
					"schema": map[string]any{"type": "string"},
				}},
				"requestBody": jsonBody(map[string]any{"type": "object", "additionalProperties": true}),
				"responses":   loginResponses(),
			},
		},
		"/api/auth/logout": map[string]any{
			"post": map[string]any{
				"operationId": "postApiAuthLogout",
				"summary":     "End the current session",
				"description": "Revokes the current session (if any) and clears the session cookie.",
				"responses": map[string]any{
					"204":     map[string]any{"description": "Logged out"},
					"default": OpenAPIErrorResponse(),
				},
			},
		},
		"/api/auth/refresh": map[string]any{
			"post": map[string]any{
				"operationId": "postApiAuthRefresh",
				"summary":     "Get a fresh token for the current session",
				"security":    OpenAPISecurity(AuthRequired),
				"responses": map[string]any{
					"200":     jsonResp("OK", ref("SchemafRefreshResponse")),
					"default": OpenAPIErrorResponse(),
				},
			},
		},
	}

	str := map[string]any{"type": "string"}
	schemas = map[string]any{
		ErrorSchemaName: openAPIErrorSchema(),
		"SchemafAuth0LoginRequest": map[string]any{
			"type": "object", "required": []any{"id_token"},
			"properties": map[string]any{"id_token": str},
		},
		"SchemafLoginResponse": map[string]any{
			"type": "object", "required": []any{"subject", "token"},
			"properties": map[string]any{"subject": str, "token": str},
		},
		"SchemafUser": map[string]any{
			"type": "object", "required": []any{"id"},
			"properties": map[string]any{"id": str},
		},
		"SchemafErrorDetails": map[string]any{
			"type":     "object",
			"required": []any{"id", "time", "method", "path", "status", "code", "error"},
			"properties": map[string]any{
				"id":      str,
				"time":    map[string]any{"type": "string", "format": "date-time"},
				"method":  str,
				"path":    str,
				"status":  map[string]any{"type": "integer"},
				"code":    str,
				"subject": map[string]any{"type": "string", "description": "The caller, if authenticated."},
				"error":   map[string]any{"type": "string", "description": "The full error."},
				"misuse":  map[string]any{"type": "string", "description": "The misuse of the error API, if any."},
			},
		},
		"SchemafRefreshResponse": map[string]any{
			"type": "object", "required": []any{"token"},
			"properties": map[string]any{"token": str},
		},
	}
	return paths, schemas
}
