package tags

import (
	"sort"
	"strconv"
	"strings"
)

// Genres is the ID3v1 genre table including the Winamp extensions, indexed by
// genre number.
var Genres = []string{
	"Blues", "Classic Rock", "Country", "Dance", "Disco", "Funk", "Grunge", "Hip-Hop",
	"Jazz", "Metal", "New Age", "Oldies", "Other", "Pop", "R&B", "Rap",
	"Reggae", "Rock", "Techno", "Industrial", "Alternative", "Ska", "Death Metal", "Pranks",
	"Soundtrack", "Euro-Techno", "Ambient", "Trip-Hop", "Vocal", "Jazz+Funk", "Fusion", "Trance",
	"Classical", "Instrumental", "Acid", "House", "Game", "Sound Clip", "Gospel", "Noise",
	"Alternative Rock", "Bass", "Soul", "Punk", "Space", "Meditative", "Instrumental Pop", "Instrumental Rock",
	"Ethnic", "Gothic", "Darkwave", "Techno-Industrial", "Electronic", "Pop-Folk", "Eurodance", "Dream",
	"Southern Rock", "Comedy", "Cult", "Gangsta", "Top 40", "Christian Rap", "Pop/Funk", "Jungle",
	"Native American", "Cabaret", "New Wave", "Psychedelic", "Rave", "Showtunes", "Trailer", "Lo-Fi",
	"Tribal", "Acid Punk", "Acid Jazz", "Polka", "Retro", "Musical", "Rock & Roll", "Hard Rock",
	"Folk", "Folk-Rock", "National Folk", "Swing", "Fast Fusion", "Bebop", "Latin", "Revival",
	"Celtic", "Bluegrass", "Avantgarde", "Gothic Rock", "Progressive Rock", "Psychedelic Rock", "Symphonic Rock", "Slow Rock",
	"Big Band", "Chorus", "Easy Listening", "Acoustic", "Humour", "Speech", "Chanson", "Opera",
	"Chamber Music", "Sonata", "Symphony", "Booty Bass", "Primus", "Porn Groove", "Satire", "Slow Jam",
	"Club", "Tango", "Samba", "Folklore", "Ballad", "Power Ballad", "Rhythmic Soul", "Freestyle",
	"Duet", "Punk Rock", "Drum Solo", "A Cappella", "Euro-House", "Dance Hall", "Goa", "Drum & Bass",
	"Club-House", "Hardcore Techno", "Terror", "Indie", "BritPop", "Afro-Punk", "Polsk Punk", "Beat",
	"Christian Gangsta Rap", "Heavy Metal", "Black Metal", "Crossover", "Contemporary Christian", "Christian Rock", "Merengue", "Salsa",
	"Thrash Metal", "Anime", "JPop", "Synthpop", "Abstract", "Art Rock", "Baroque", "Bhangra",
	"Big Beat", "Breakbeat", "Chillout", "Downtempo", "Dub", "EBM", "Eclectic", "Electro",
	"Electroclash", "Emo", "Experimental", "Garage", "Global", "IDM", "Illbient", "Industro-Goth",
	"Jam Band", "Krautrock", "Leftfield", "Lounge", "Math Rock", "New Romantic", "Nu-Breakz", "Post-Punk",
	"Post-Rock", "Psytrance", "Shoegaze", "Space Rock", "Trop Rock", "World Music", "Neoclassical", "Audiobook",
	"Audio Theatre", "Neue Deutsche Welle", "Podcast", "Indie Rock", "G-Funk", "Dubstep", "Garage Rock", "Psybient",
}

// SortedGenres returns the genre names in alphabetical order.
func SortedGenres() []string {
	s := append([]string(nil), Genres...)
	sort.Strings(s)
	return s
}

// NormalizeGenre resolves numeric genre references in a TCON value:
// "17" and "(17)" become "Rock", "(17)Hard Rock" becomes "Hard Rock",
// "(RX)" and "(CR)" become "Remix" and "Cover".
func NormalizeGenre(s string) string {
	var parts []string
	for _, p := range strings.Split(s, "; ") {
		if p = normalizeGenrePart(strings.TrimSpace(p)); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "; ")
}

func normalizeGenrePart(s string) string {
	var refs []string
	for strings.HasPrefix(s, "(") && !strings.HasPrefix(s, "((") {
		end := strings.IndexByte(s, ')')
		if end < 0 {
			break
		}
		refs = append(refs, s[1:end])
		s = s[end+1:]
	}
	if s = strings.TrimSpace(s); s != "" {
		if strings.HasPrefix(s, "((") {
			s = s[1:] // escaped literal "(" per ID3v2.3
		}
		if len(refs) == 0 {
			if name, ok := genreName(s); ok {
				return name
			}
		}
		return s
	}
	var names []string
	for _, r := range refs {
		if name, ok := genreName(r); ok {
			names = append(names, name)
		}
	}
	return strings.Join(names, "; ")
}

func genreName(ref string) (string, bool) {
	switch ref {
	case "RX":
		return "Remix", true
	case "CR":
		return "Cover", true
	}
	if n, err := strconv.Atoi(ref); err == nil && n >= 0 && n < len(Genres) {
		return Genres[n], true
	}
	return "", false
}
