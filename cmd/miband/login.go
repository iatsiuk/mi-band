package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func newLoginCmd(cf *cloudFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log into the Xiaomi account and save the cloud session",
		Long: `Log into the Xiaomi account and save the cloud session.

With --user the password is read from MIBAND_PASSWORD, a no-echo prompt on a
terminal, or the first line of stdin. When the account asks for a browser
check, finish it there and pass the userId and passToken cookies of
account.xiaomi.com with --user-id and --pass-token.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cf.explicit() {
				return errors.New("use --user (password prompt) or --user-id with --pass-token")
			}
			s, err := cf.login(cmd.Context())
			if err != nil {
				return err
			}
			p, err := sessionPath()
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "logged in as %s (region %q), session saved to %s\n", s.UserID, cf.region, p)
			return nil
		},
	}
}
