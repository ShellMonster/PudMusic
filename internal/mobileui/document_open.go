package mobileui

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

type mobileDocumentSelection struct {
	Directory string
	Files     []string
	Failed    int
}

func (s *shell) watchDocumentShares() func() {
	if runtime.GOOS == "ios" {
		return mygo.App.OnOpenURL(func(value string) {
			location, err := url.Parse(value)
			if err != nil || location.Scheme != "file" || location.Host != "" || !filepath.IsAbs(location.Path) {
				return
			}
			go s.finishLocalImport(&localLibraryState{base: mobileImportBase()}, []string{location.Path}, 0, nil)
		})
	}
	if runtime.GOOS != "android" {
		return func() {}
	}
	plugin := mygo.Native("pudmusic_documents")
	take := func() {
		var shares []mobileDocumentSelection
		if err := plugin.Call(context.WithoutCancel(s.ctx), "takeShares", nil, &shares); err != nil {
			return
		}
		for _, share := range shares {
			s.finishLocalImport(&localLibraryState{base: mobileImportBase()}, share.Files, share.Failed, nil)
			if share.Directory != "" && strings.HasPrefix(filepath.Base(share.Directory), "pudmusic-share-") {
				_ = os.RemoveAll(share.Directory)
			}
		}
	}
	off := plugin.On("shared", func([]byte) { go take() })
	mygo.App.WhenReady(func() { go take() })
	return off
}
