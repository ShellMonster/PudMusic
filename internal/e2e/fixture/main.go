//go:build e2e

package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type fixture struct {
	count int
}

func (f *fixture) view(c *ui.Context) {
	ui.Column(c).Key("fixture").Gap(12).Padding(24).Children(func() {
		ui.Text(c, "Count: "+itoa(f.count)).Key("count")
		ui.PrimaryButton(c, "Increment").Key("increment").OnClick(func() {
			f.count++
		})
		ui.Button(c, "Quit").Key("quit").OnClick(mygo.App.Quit)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}

func main() {
	f := &fixture{}
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title:   "MyGo automation fixture",
			Width:   320,
			Height:  220,
			Content: ui.View(f.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}
