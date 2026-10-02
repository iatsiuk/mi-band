package main

import (
	"strings"

	"github.com/spf13/cobra"

	"miband/internal/cloud"
)

type deviceRow struct {
	DID   string `json:"did"`
	Model string `json:"model"`
	Name  string `json:"name"`
	MAC   string `json:"mac,omitempty"`
	SN    string `json:"sn,omitempty"`
}

type deviceList []deviceRow

func (l deviceList) markdown(b *strings.Builder) {
	t := table{header: []string{"DID", "Model", "Name", "MAC", "SN"}}
	for _, d := range l {
		t.add(d.DID, d.Model, d.Name, d.MAC, d.SN)
	}
	t.write(b)
}

func newDevicesCmd(cf *cloudFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "List devices bound to the account (use their did with data --did)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cl, err := newClient(ctx, cf)
			if err != nil {
				return err
			}
			devs, err := withRetry(ctx, cl, func(s *cloud.Session) ([]cloud.Device, error) { return s.Devices(ctx) })
			if err != nil {
				return err
			}
			rows := deviceList{}
			for i := range devs {
				d := &devs[i]
				rows = append(rows, deviceRow{d.DID, d.Model, strings.TrimSpace(d.Name), d.MAC, d.SN})
			}
			return writeOutput(cmd.OutOrStdout(), cf.format, rows)
		},
	}
}
