// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

// authStatus returns {"providers": ["auth0", ...]}: the names of the active
// identity providers. The client settings the frontend needs for a provider
// come from runtime config (/api/config), not from here.
func authStatus() map[string]any {
	providers := make([]string, 0, len(identityProviders))
	for _, p := range identityProviders {
		providers = append(providers, p.Name())
	}
	return map[string]any{"providers": providers}
}
