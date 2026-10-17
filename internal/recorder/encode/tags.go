package encode

import (
	"fmt"

	"github.com/mailsvb/go-tag/internal/tags"
)

// ToTags maps recorder metadata onto the fields go-tag keeps. Composer and
// disc numbers have no place there and are dropped. Like the tag editor, the
// album artist follows the artist.
func ToTags(m Meta, embedCover bool) tags.Tags {
	t := tags.Tags{
		Title:       m.Title,
		Artist:      m.Artist,
		AlbumArtist: m.Artist,
		Album:       m.Album,
		Genre:       tags.NormalizeGenre(m.Genre),
		Year:        m.Year,
	}
	if m.Track > 0 {
		t.Track = fmt.Sprintf("%02d", m.Track)
	}
	if embedCover && m.Cover != nil {
		t.Cover = &tags.Picture{MIME: m.Cover.MIME, Data: m.Cover.Data}
	}
	return t
}
