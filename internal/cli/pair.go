package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

type pairingCode struct {
	Code      string `json:"code"`
	ExpiresAt int64  `json:"expires_at"`
	URL       string `json:"url"`
}

// pairURL is the QR payload the mobile app understands (scheme from app.json).
func pairURL(publicURL, code string) string {
	q := url.Values{"url": {publicURL}, "code": {code}}
	return "rocketmobile://pair?" + q.Encode()
}

func webLoginURL(base, code string) string {
	return base + "/login?code=" + url.QueryEscape(code)
}

func newPairCmd() *cobra.Command {
	var web bool
	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Подключить устройство: QR для мобилки или ссылка входа для браузера (--web)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return &usageError{message: "usage: rocket pair [--web]"}
			}
			c, cfg, err := connect(true)
			if err != nil {
				return err
			}
			var pc pairingCode
			if err := c.Post("/v1/auth/pairing-codes", nil, &pc); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, pc)
			}
			out := cmd.OutOrStdout()
			ttl := time.Until(time.Unix(pc.ExpiresAt, 0)).Round(time.Minute)
			if web {
				base := pc.URL
				if base == "" {
					base = fmt.Sprintf("http://localhost:%d", cfg.Port)
				}
				fmt.Fprintf(out, "Открой в браузере (одноразовая ссылка, %s):\n\n  %s\n", ttl, webLoginURL(base, pc.Code))
				return nil
			}
			if pc.URL == "" {
				fmt.Fprintln(out, "⚠ public_url не задан в ~/.rocket/config.yaml — QR не содержит адреса; введи адрес и код в приложении вручную.")
			} else {
				qrterminal.GenerateHalfBlock(pairURL(pc.URL, pc.Code), qrterminal.L, out)
				fmt.Fprintf(out, "\nАдрес: %s\n", pc.URL)
			}
			fmt.Fprintf(out, "Код:   %s  (действует %s, одноразовый)\n", pc.Code, ttl)
			return nil
		},
	}
	cmd.Flags().BoolVar(&web, "web", false, "напечатать одноразовую ссылку входа для браузера")
	return cmd
}

func newDevicesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "devices",
		Short: "Подключённые устройства (ls / revoke)",
	}
	cmd.AddCommand(newDevicesLsCmd(), newDevicesRevokeCmd())
	return cmd
}

func newDevicesLsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "Список подключённых устройств",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			var list []struct {
				ID         int64  `json:"id"`
				Name       string `json:"name"`
				Kind       string `json:"kind"`
				CreatedAt  int64  `json:"created_at"`
				LastSeenAt *int64 `json:"last_seen_at"`
			}
			if err := c.Get("/v1/auth/devices", nil, &list); err != nil {
				return err
			}
			if flags.JSON {
				return printJSON(cmd, list)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tNAME\tKIND\tCREATED\tLAST SEEN")
			for _, d := range list {
				seen := "-"
				if d.LastSeenAt != nil {
					seen = time.Unix(*d.LastSeenAt, 0).Format("2006-01-02 15:04")
				}
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Kind,
					time.Unix(d.CreatedAt, 0).Format("2006-01-02 15:04"), seen)
			}
			return tw.Flush()
		},
	}
}

func newDevicesRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <id>",
		Short: "Отозвать устройство (его открытые соединения рвутся сразу)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return &usageError{message: "usage: rocket devices revoke <id>"}
			}
			if _, err := strconv.ParseInt(args[0], 10, 64); err != nil {
				return &usageError{message: "usage: rocket devices revoke <id> (id — число из `rocket devices ls`)"}
			}
			c, _, err := connect(true)
			if err != nil {
				return err
			}
			if err := c.Delete("/v1/auth/devices/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Устройство %s отозвано\n", args[0])
			return nil
		},
	}
}
