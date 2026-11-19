package mobileui

import (
	"strconv"

	"github.com/ShellMonster/PudMusic/internal/app"
	"github.com/ShellMonster/PudMusic/internal/service/bili"
	"github.com/egoist/mygo/ui"
)

func (s *shell) collectionSiblings() []browseCollection {
	stack := s.stacks[s.tab]
	if len(stack) < 2 {
		return nil
	}
	parent := stack[len(stack)-2]
	switch state := parent.data.(type) {
	case *discoverState:
		return state.mixes.cards
	case *discoverDetail:
		if state.mixes != nil {
			return state.mixes.cards
		}
	case *mobileSearchState:
		if state.query != "" {
			return state.results[mobileSearchMixes].mixes
		}
		return state.mixes.cards
	case *myLibraryState:
		var cards []browseCollection
		for _, group := range []app.FavoriteGroup{app.FavoritesCreated, app.FavoritesCollected} {
			for _, folder := range s.app.SideFavorites(group) {
				if !folder.Invalid {
					ref := app.CollectionRef{ID: folder.ID, OwnerMid: folder.Mid, Type: bili.ParseCollectionType(strconv.Itoa(folder.Type))}
					cards = append(cards, browseCollection{ref: ref, title: folder.Title, cover: folder.Cover, count: folder.MediaCount})
				}
			}
		}
		return cards
	}
	return nil
}

func (s *shell) collectionIndex(c *ui.Context, selected app.CollectionRef, cards []browseCollection) {
	settingsPageHeading(c, "歌单")
	seen := make(map[string]bool, len(cards))
	for _, card := range cards {
		key := card.ref.Key()
		if seen[key] {
			continue
		}
		seen[key] = true
		subtitle := card.ref.Type.Label()
		if card.owner != "" {
			subtitle = card.owner
		}
		row := libraryRow(c.Key(key), card.cover, "folder", card.title, subtitle, func() {
			if s.current().ref.Key() == key {
				return
			}
			stack := s.stacks[s.tab]
			s.stacks[s.tab] = stack[:len(stack)-1]
			s.push(route{kind: pageCollection, title: card.title, ref: card.ref})
		})
		if key == selected.Key() {
			row.AccessSelected(true).Background(phonePalette(c).soft)
		}
	}
}
