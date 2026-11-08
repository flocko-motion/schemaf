// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package api

// This file holds the public provider config exposed under "auth" in
// /api/status. It is a deliberately narrow interim solution until generic
// runtime frontend config (GitHub issue #35) replaces it.

// PublicConfigProvider is optionally implemented by identity providers that
// expose public client config (values safe for anyone to read, e.g. the
// Auth0 domain and client ID) to the frontend via /api/status.
type PublicConfigProvider interface {
	PublicConfig() map[string]string
}

// authStatus returns {"providers": {name: publicConfig, ...}} for the active
// identity providers. Providers without public config appear as {}.
func authStatus() map[string]any {
	providers := map[string]map[string]string{}
	for _, p := range identityProviders {
		cfg := map[string]string{}
		if pc, ok := p.(PublicConfigProvider); ok {
			for k, v := range pc.PublicConfig() {
				cfg[k] = v
			}
		}
		providers[p.Name()] = cfg
	}
	return map[string]any{"providers": providers}
}
