// Part of the schemaf framework — https://github.com/flocko-motion/schemaf
// Read the docs, report bugs and feature requests as GitHub issues. We respond quickly.

package schemaf

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/flocko-motion/schemaf/api"
	"github.com/flocko-motion/schemaf/cli"
)

// authProvider returns the "auth" command tree. It is mounted only when the
// project has a database, since the JWT signing key lives in _schemaf_config.
func (a *App) authProvider(_ *cli.Context) []*cobra.Command {
	authCmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication helpers",
	}
	authCmd.AddCommand(a.authTokenCmd())
	return []*cobra.Command{authCmd}
}

// authTokenCmd implements `auth token <subject>`: mint a signed JWT for any
// subject so you can call authenticated endpoints as that user in local dev.
// Dev-only — it refuses to run outside development mode (see api.DevMode).
func (a *App) authTokenCmd() *cobra.Command {
	var ttl time.Duration
	cmd := &cobra.Command{
		Use:   "token <subject>",
		Short: "Mint a JWT for a subject (dev only)",
		Long: `Mint a signed JWT for any subject, so you can call authenticated
endpoints as that user during local development:

  TOKEN=$(./schemaf.sh auth token alice)
  curl -H "Authorization: Bearer $TOKEN" localhost:8000/api/user/me

Dev-only: refuses to run outside development mode (SCHEMAF_DEV=1, which
./schemaf.sh sets). Requires the database to be running, since the signing
key is stored there.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !api.DevMode() {
				return fmt.Errorf("auth token is dev-only and refuses to run in production: run it as ./schemaf.sh auth token, which sets SCHEMAF_DEV=1")
			}
			if err := a.initDB(a.ctx, a.dsn()); err != nil {
				return err
			}
			if err := a.initAuth(a.ctx); err != nil {
				return fmt.Errorf("auth init: %w", err)
			}
			var exp time.Time
			if ttl > 0 {
				exp = time.Now().Add(ttl)
			}
			token, err := api.IssueToken(args[0], exp)
			if err != nil {
				return err
			}
			fmt.Println(token) // token on stdout; logs go to stderr, so $(...) captures only this
			return nil
		},
	}
	cmd.Flags().DurationVar(&ttl, "ttl", 0, "token lifetime (e.g. 24h); default: no expiry")
	return cmd
}
