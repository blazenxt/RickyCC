package main

import (
	"bytes"
	_ "embed"
	"sync"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// cardImage is the branded reward image shipped inside the binary.
//
//go:embed assets/card.jpg
var cardImage []byte

var (
	imgMu           sync.Mutex
	cardImageFileID string
)

// cardPhotoInput returns an upload on first use and a cached file_id after,
// so the image is only uploaded to Telegram once per process.
func cardPhotoInput() gotgbot.InputFileOrString {
	imgMu.Lock()
	defer imgMu.Unlock()
	if cardImageFileID != "" {
		return gotgbot.InputFileByID(cardImageFileID)
	}
	return gotgbot.InputFileByReader("card.jpg", bytes.NewReader(cardImage))
}

// cacheCardImageID stores the Telegram file_id returned by the first upload.
func cacheCardImageID(msg *gotgbot.Message) {
	if msg == nil || len(msg.Photo) == 0 {
		return
	}
	largest := msg.Photo[len(msg.Photo)-1].FileId
	imgMu.Lock()
	if cardImageFileID == "" && largest != "" {
		cardImageFileID = largest
	}
	imgMu.Unlock()
}
