package mobileui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/mobile"
	"golang.org/x/image/draw"
)

type folderCoverCrop struct {
	image                    image.Image
	preview                  *ui.Bitmap
	zoom                     mobile.ZoomState
	open, picking, uploading bool
	status                   string
}

func (s *shell) pickFolderCover(state *folderEditor) {
	if s.win == nil {
		return
	}
	state.crop.picking = true
	go func() {
		files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: s.win, Title: "选择收藏夹封面", Filters: []mygo.FileFilter{{
			Name: "图片", Extensions: []string{"png", "jpg", "jpeg"}, MIMETypes: []string{"image/png", "image/jpeg"},
		}}})
		var src image.Image
		if err == nil && len(files) > 0 {
			src, err = readFolderCover(files[0])
		}
		var preview *ui.Bitmap
		if src != nil {
			preview = ui.NewBitmap(src)
		}
		s.update(func() {
			state.crop.picking = false
			if err != nil {
				state.status = "无法打开这张图片。请选择有效的 PNG 或 JPEG 图片。"
			} else if src != nil {
				state.status = ""
				state.crop = folderCoverCrop{image: src, preview: preview, open: true}
			}
		})
	}()
}

func readFolderCover(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 30_000_000 {
		return nil, errors.New("unsupported cover dimensions")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, err
	}
	src, _, err := image.Decode(file)
	return src, err
}

func (s *shell) folderCoverSheet(c *ui.Context, state *folderEditor) {
	crop := &state.crop
	librarySheet(c, &crop.open, mobile.SheetOptions{Label: "裁剪封面", Detents: []float32{0.68, 0.92}, NoDismiss: crop.uploading}, func() {
		width, _ := c.Size()
		side := min(width-32, float32(260))
		ui.Column(c).AlignItems(ui.Center).Padding(16).Gap(10).Children(func() {
			ui.Text(c, "裁剪封面").FontSize(20).Bold()
			ui.Text(c, "双指缩放，拖动调整取景。").FontSize(13).TextColor(c.Theme().TextMuted)
			mobile.Zoomable(c, &crop.zoom, mobile.ZoomOptions{}, func() {
				ui.Image(c, crop.preview).Size(side, side).Fit(ui.Cover)
			}).Size(side, side).Radius(12)
			if crop.status != "" {
				ui.Text(c, crop.status).FontSize(13).TextColor(c.Theme().Danger)
			}
			ui.Row(c).Gap(8).Children(func() {
				libraryButton(c, "", "取消", false, crop.uploading, func() { crop.open = false })
				libraryButton(c, "", "使用此封面", true, crop.uploading || crop.image == nil, func() { s.uploadFolderCover(crop, side) })
			})
		})
	})
}

func (s *shell) uploadFolderCover(crop *folderCoverCrop, side float32) {
	if crop.uploading {
		return
	}
	crop.uploading, crop.status = true, ""
	src, zoom := crop.image, crop.zoom
	account := s.app.AccountSnapshot()
	go func() {
		var jpegData bytes.Buffer
		err := jpeg.Encode(&jpegData, cropFolderImage(src, zoom, side), &jpeg.Options{Quality: 88})
		s.update(func() {
			if account != s.app.AccountSnapshot() {
				crop.uploading = false
				return
			}
			if err != nil {
				crop.uploading, crop.status = false, "无法处理这张图片，请重试。"
				return
			}
			s.app.UploadCover("data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegData.Bytes()))
		})
	}()
}

func cropFolderImage(src image.Image, zoom mobile.ZoomState, previewSide float32) image.Image {
	bounds := src.Bounds()
	side := float64(min(bounds.Dx(), bounds.Dy()))
	scale := float64(max(zoom.Scale, 1))
	span := max(1, int(math.Round(side/scale)))
	pixelsPerPoint := side / float64(max(previewSide, 1)) / scale
	left := clampCrop(bounds.Min.X+(bounds.Dx()-span)/2-int(math.Round(float64(zoom.X)*pixelsPerPoint)), bounds.Min.X, bounds.Max.X-span)
	top := clampCrop(bounds.Min.Y+(bounds.Dy()-span)/2-int(math.Round(float64(zoom.Y)*pixelsPerPoint)), bounds.Min.Y, bounds.Max.Y-span)
	pixels := min(512, span)
	output := image.NewRGBA(image.Rect(0, 0, pixels, pixels))
	draw.CatmullRom.Scale(output, output.Bounds(), src, image.Rect(left, top, left+span, top+span), draw.Src, nil)
	return output
}

func clampCrop(value, lower, upper int) int {
	return min(upper, max(lower, value))
}
