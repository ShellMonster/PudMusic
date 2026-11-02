package list

import (
	"fmt"
	"testing"

	"github.com/ShellMonster/PudMusic/internal/app"

)

func TestZZProbe(t *testing.T) {
	_, tt := newList(t, Actions{}, rowTextSongs())
	fmt.Println("TEXTS:", tt.Texts())
	r, ok := tt.Find("UP 乙")
	fmt.Println("find UP 乙:", ok, r)
	if err := tt.RightClick("UP 乙"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("menu after rightclick on UP 乙:", tt.Menu())
	tt.CloseMenu()

	// right click on the title
	if err := tt.RightClick("第二首歌"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("menu after rightclick on 第二首歌:", tt.Menu())
	tt.CloseMenu()
}

func TestZZProbeGrid(t *testing.T) {
	_, tt := newGrid(t, Actions{}, rowTextSongs(), 900, gridH)
	fmt.Println("GRID TEXTS:", tt.Texts())
	if err := tt.RightClick("第二首歌"); err != nil {
		t.Fatal(err)
	}
	fmt.Println("grid menu:", tt.Menu())
	tt.CloseMenu()
}

func TestZZProbeFocus(t *testing.T) {
	var played []app.Item
	_, tt := newList(t, Actions{PlayNow: func(item app.Item) { played = append(played, item) }}, rowTextSongs())
	r, _ := tt.Find("UP 乙")
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	fmt.Println("after 1 click, played:", len(played))
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	fmt.Println("after 2 clicks, played:", len(played))
	fmt.Println("focused UP 乙:", tt.Focused("UP 乙"))
}
