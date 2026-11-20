package dialogs

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/thenktor/discnegative/res"
)

type WhatsNewDialog struct {
	widget.BaseWidget
}

func NewWhatsNewDialog() *WhatsNewDialog {
	w := &WhatsNewDialog{}
	w.ExtendBaseWidget(w)
	return w
}

const changelogTextTemplate = `The changes in this version are listed in the [commit history](%s).`

func (w *WhatsNewDialog) CreateRenderer() fyne.WidgetRenderer {
	changelogPrompt := widget.NewRichTextFromMarkdown(fmt.Sprintf(changelogTextTemplate, res.CommitsURL))
	changelogPrompt.Wrapping = fyne.TextWrapWord
	objects := container.NewVBox(changelogPrompt)

	c := container.NewVScroll(objects)
	return widget.NewSimpleRenderer(c)
}

func (w *WhatsNewDialog) MinSize() fyne.Size {
	return fyne.NewSize(425, 275)
}
