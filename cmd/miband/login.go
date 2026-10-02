package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLoginCmd(cf *cloudFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Log into the Xiaomi account and save the cloud session",
		Long: `Log into the Xiaomi account and save the cloud session.

Prompts for what no flag gives: the server region (--region), the account
(--user), the password (MIBAND_PASSWORD, a no-echo prompt on a terminal, else a
stdin line) and, when the account asks for a security check, the code it sends
to the account email. Without a terminal the answers are read line by line
from stdin. A check without an email code has to be finished in a browser;
then pass the userId and passToken cookies of account.xiaomi.com with
--user-id and --pass-token.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cf.passToken == "" {
				if err := cf.askMissing(cmd.Context(), cmd.Flags().Changed("region")); err != nil {
					return err
				}
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
