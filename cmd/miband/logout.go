package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the saved cloud session",
		Long: `Remove the saved cloud session.

Like Mi Fitness, logout only forgets the session locally: the pass token stays
valid on the Xiaomi server. To revoke it, change the password or remove the
device in the security settings of account.xiaomi.com.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := sessionPath()
			if err != nil {
				return err
			}
			err = os.Remove(p)
			if errors.Is(err, os.ErrNotExist) {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "not logged in")
				return nil
			}
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "logged out, removed %s\n", p)
			return nil
		},
	}
}
