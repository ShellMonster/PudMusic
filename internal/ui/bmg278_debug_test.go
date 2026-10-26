package ui

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestBMG278DebugPositions(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		var st ui.ListState
		ui.Column(c).Key("outer").FillWidth().Padding(16, 16, 16, 16).Children(func() {
			ui.List(c, &st, 1).FillWidth().Grow(1).Rows(func(r ui.ListRow) {
				ui.Box(c).Key("wrap").Height(100).FillWidth().Children(func() {
					ui.Box(c).Key("esc").Label("escaped").Absolute().Left(-16).Right(-16).Top(0).Height(50).
						Background(ui.Color{R: 255, A: 255})
					ui.Box(c).Key("noesc").Label("plain").FillWidth().Height(20)
				})
			})
		})
	}, 400, 200)
	tt.Frame()
	for _, s := range []string{"escaped", "plain"} {
		r, ok := tt.Find(s)
		fmt.Printf("find %q -> ok=%v rect=%+v\n", s, ok, r)
	}
}
