// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

// Package isolation marks a server start as isolated: started by schemaftest,
// with identity providers only from AddIdentityProvider (never from the OIDC
// environment variables), and without the SFTP backup scheduler or the
// frontend dev proxy. It is internal, so only the framework can start an
// isolated server: app code can't import it.
package isolation

import "context"

type key struct{}

// Context returns ctx marked as an isolated server start.
func Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, key{}, true)
}

// Isolated reports whether ctx is marked as an isolated server start.
func Isolated(ctx context.Context) bool {
	v, _ := ctx.Value(key{}).(bool)
	return v
}
