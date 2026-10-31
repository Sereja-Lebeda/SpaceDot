package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// Поля остаются слева и не растягиваются вместе с широким окном.
type widthLimitLayout struct{ width float32 }

func (l widthLimitLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	size := objects[0].MinSize()
	size.Width = min(size.Width, l.width)
	return size
}

func (l widthLimitLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(min(size.Width, l.width), size.Height))
}

func limitedWidth(object fyne.CanvasObject, width float32) *fyne.Container {
	return container.New(widthLimitLayout{width: width}, object)
}
