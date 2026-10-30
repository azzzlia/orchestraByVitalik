package main

import (
	"claude-squad/app"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

var webOpen bool

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Ссылки на веб-интерфейс Squad (на маке и с телефона через Tailscale)",
	Long: `Веб-интерфейс работает внутри запущенного Squad: сессии, «ждут тебя», живые терминалы.
Первая ссылка — на этом маке, остальные — по адресу мака в Tailscale (открой на телефоне,
подключённом к той же сети Tailscale). В ссылке ключ: не пересылай её никому.
--open — открыть первую ссылку в браузере.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		urls, err := app.WebURLs()
		if err != nil {
			return err
		}
		for i, u := range urls {
			where := "на этом маке"
			if i > 0 {
				where = "с телефона (Tailscale)"
			}
			fmt.Printf("%s: %s\n", where, u)
		}
		if len(urls) == 1 {
			fmt.Println("(адреса Tailscale нет — для телефона включи Tailscale на маке и перезапусти Squad)")
		}
		if webOpen && runtime.GOOS == "darwin" {
			return exec.Command("open", urls[0]).Run()
		}
		return nil
	},
}

func init() {
	webCmd.Flags().BoolVar(&webOpen, "open", false, "открыть в браузере")
	webCmd.SilenceUsage = true
	rootCmd.AddCommand(webCmd)
}
