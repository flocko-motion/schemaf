// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

// AuthOpenAPI returns the OpenAPI path items and component schemas of the
// framework's auth routes, as plain JSON-shaped maps. Both the codegen
// (static gen/openapi.json) and BuildOpenAPI merge them in, so the generated
// clients always have the auth routes. The auth0 login route is always
// included, independent of deployment config.
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
			"200": jsonResp("Session started; the session cookie is set", ref("SchemafLoginResponse")),
			"400": map[string]any{"description": "Bad Request"},
			"403": map[string]any{"description": "Forbidden"},
			"404": map[string]any{"description": "Provider not configured"},
			"500": map[string]any{"description": "Internal Server Error"},
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
					"204": map[string]any{"description": "Logged out"},
					"403": map[string]any{"description": "Cross-origin request rejected"},
				},
			},
		},
		"/api/auth/refresh": map[string]any{
			"post": map[string]any{
				"operationId": "postApiAuthRefresh",
				"summary":     "Get a fresh token for the current session",
				"security":    []any{map[string]any{"bearerAuth": []any{}}},
				"responses": map[string]any{
					"200": jsonResp("OK", ref("SchemafRefreshResponse")),
					"400": map[string]any{"description": "Token is not tied to a session"},
					"401": map[string]any{"description": "Unauthorized"},
				},
			},
		},
	}

	str := map[string]any{"type": "string"}
	schemas = map[string]any{
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
