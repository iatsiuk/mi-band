package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"miband/internal/cloud"
)

func newLogoutCmd(cf *cloudFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "End the account session and remove the saved cloud session",
		Long: `End the account session and remove the saved cloud session.

Logout first ends the session of the saved pass token on account.xiaomi.com,
as the account web page does, then removes the session file. The file is
removed even when the server call fails; the command then exits with an error
because the pass token may still be valid. Mi Fitness itself only forgets the
account locally.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := sessionPath()
			if err != nil {
				return err
			}
			serverErr := cf.endServerSession(cmd)
			err = os.Remove(p)
			if errors.Is(err, os.ErrNotExist) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "not logged in")
				return nil
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "removed %s\n", p)
			if serverErr != nil {
				return fmt.Errorf("local session removed, but the server logout failed and the pass token may stay valid: %w", serverErr)
			}
			return nil
		},
	}
}

// endServerSession logs the saved session out on the account server; without a usable saved
// session there is nothing to end there.
func (c *cloudFlags) endServerSession(cmd *cobra.Command) error {
	saved, ok, err := c.loadSaved()
	if !ok {
		if err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "saved session unreadable, logout is local only: %v\n", err)
		}
		return nil
	}
	sess, err := cloud.Resume(&saved.Session, c.options())
	if err != nil {
		return err
	}
	if err := sess.Logout(cmd.Context()); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "account session ended on the server")
	return nil
}
