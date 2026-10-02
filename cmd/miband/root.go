package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"miband/internal/cloud"
)

// exit codes
const (
	exitOK    = 0
	exitError = 1
	exitAuth  = 3
	exitINT   = 130
)

func newRootCmd() *cobra.Command {
	cf := &cloudFlags{}
	cmd := &cobra.Command{
		Use:           "miband",
		Short:         "Xiaomi Smart Band data synced by Mi Fitness to the Xiaomi cloud",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			if !validFormat(cf.format) {
				return fmt.Errorf("--format must be %s or %s, got %q", formatMarkdown, formatJSON, cf.format)
			}
			cf.stdin, cf.stderr = cmd.InOrStdin(), cmd.ErrOrStderr()
			return nil
		},
	}
	cmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.AddCommand(newLoginCmd(cf))
	cmd.AddCommand(newDevicesCmd(cf))
	cmd.AddCommand(newDataCmd(cf))

	f := cmd.PersistentFlags()
	f.StringVar(&cf.region, "region", "", "Xiaomi health cloud region (e.g. sg, de); default: saved login, else China")
	f.StringVarP(&cf.user, "user", "u", "", "Xiaomi account for password login")
	f.StringVar(&cf.userID, "user-id", "", "numeric userId cookie of account.xiaomi.com (with --pass-token)")
	f.StringVar(&cf.passToken, "pass-token", "", "passToken cookie of account.xiaomi.com after browser verification")
	f.StringVarP(&cf.format, "format", "f", formatMarkdown, "output format: markdown, json")
	f.BoolVarP(&cf.verbose, "verbose", "v", false, "log cloud requests (endpoints and sizes only) to stderr")
	return cmd
}

func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	var ne *cloud.NotificationError
	var ce *cloud.CaptchaError
	var ve *cloud.VerificationError
	if errors.Is(err, errNotLoggedIn) || errors.Is(err, cloud.ErrUnauthorized) ||
		errors.Is(err, cloud.ErrInvalidCredentials) || errors.Is(err, cloud.ErrInvalidUser) ||
		errors.As(err, &ne) || errors.As(err, &ce) || errors.As(err, &ve) {
		return exitAuth
	}
	return exitError
}
