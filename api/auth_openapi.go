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
// session cookie named cookieName). Both generators (runtime BuildOpenAPI and
// the static codegen spec) use it.
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
	codes := make([]string, 0, len(sentinels)+1)
	for _, s := range sentinels {
		codes = append(codes, s.code)
	}
	codes = append(codes, codeInternal)
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

// AuthOpenAPI returns the OpenAPI path items of the framework's auth routes
// and the framework's component schemas (including the shared Error schema),
// as plain JSON-shaped maps. Both the codegen (static gen/openapi.json) and
// BuildOpenAPI merge them in, so the generated clients always have the auth
// routes and the error type. The auth0 login route is always included,
// independent of deployment config.
func AuthOpenAPI() (paths map[string]any, schemas map[string]any) {
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

	paths = map[string]any{
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
		"SchemafRefreshResponse": map[string]any{
			"type": "object", "required": []any{"token"},
			"properties": map[string]any{"token": str},
		},
	}
	return paths, schemas
}
