package mp3tags

import (
	"bytes"
	"os"
	"testing"

	"github.com/bogem/id3v2/v2"
)

// mp3gainAPE returns an APEv2 tag as MP3Gain writes it, plus a foreign item.
func mp3gainAPE(undoValue string) []byte {
	items := []apeItem{
		{key: "Artist", value: []byte("APE Artist")},
		{key: "MP3GAIN_MINMAX", value: []byte("100,200")},
		{key: "REPLAYGAIN_TRACK_GAIN", value: []byte("-1.230000 dB")},
	}
	if undoValue != "" {
		items = append(items, apeItem{key: "mp3gain_undo", value: []byte(undoValue)})
	}
	return encodeAPE(items)
}

// id3WithTXXX returns an ID3v2.3 tag with a title and the given TXXX frames.
func id3WithTXXX(t *testing.T, kv ...string) []byte {
	t.Helper()
	tag := newTag()
	tag.SetTitle("T")
	for i := 0; i < len(kv); i += 2 {
		tag.AddUserDefinedTextFrame(id3v2.UserDefinedTextFrame{Encoding: id3v2.EncodingISO, Description: kv[i], Value: kv[i+1]})
	}
	var buf bytes.Buffer
	if _, err := tag.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fileAPE returns the APEv2 items of the file at path.
func fileAPE(t *testing.T, path string) []apeItem {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start, err := skipID3v2(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	return readAPE(bytes.NewReader(data), start, int64(len(data)))
}

func readSteps(t *testing.T, path string) int {
	t.Helper()
	tg, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	return tg.Gain.Steps
}

func TestUndoFormat(t *testing.T) {
	for _, c := range []struct {
		u undo
		s string
	}{
		{undo{-3, -3, 'N'}, "-003,-003,N"},
		{undo{2, 12, 'W'}, "+002,+012,W"},
		{undo{0, 0, 0}, "+000,+000,N"},
	} {
		if got := c.u.String(); got != c.s {
			t.Errorf("%+v.String() = %q, want %q", c.u, got, c.s)
		}
		if u, ok := parseUndo(c.s); !ok || u.left != c.u.left || u.right != c.u.right {
			t.Errorf("parseUndo(%q) = %+v, %v", c.s, u, ok)
		}
	}
	if _, ok := parseUndo("junk"); ok {
		t.Error("parseUndo accepted junk")
	}
}

func TestAPERoundTrip(t *testing.T) {
	items := []apeItem{{key: "A", value: []byte("x")}, {flags: 2, key: "Bin", value: []byte{0, 1, 2}}}
	got := parseAPE(encodeAPE(items))
	if len(got) != 2 || got[0].key != "A" || string(got[0].value) != "x" ||
		got[1].flags != 2 || !bytes.Equal(got[1].value, []byte{0, 1, 2}) {
		t.Errorf("parseAPE(encodeAPE) = %+v", got)
	}
	if got := parseAPE(apeTag()); len(got) != 1 || got[0].key != "Title" {
		t.Errorf("parseAPE(test tag) = %+v", got)
	}
}

// A change written by go-tag is stored where MP3Gain looks for it.
func TestWriteStoresMP3GainUndo(t *testing.T) {
	audio := fakeAudio()
	path := writeFile(t, id3v24Tag(t), audio, apeTag(), id3v1Tag("V1", "V1", 1, 1))
	if err := WriteAudio(path, Tags{Title: "T", Gain: Gain{Steps: 3}}, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := encodeAPE([]apeItem{{key: keyUndo, value: []byte("-003,-003,N")}})
	if !bytes.HasSuffix(data, append(append([]byte{}, audio...), want...)) {
		t.Error("file does not end with the audio and an APEv2 tag holding only MP3GAIN_UNDO")
	}
	if _, ids := frameIDs(t, path); ids["TXXX"] != 0 {
		t.Errorf("frames = %v, want no TXXX", ids)
	}
	if s := readSteps(t, path); s != 3 {
		t.Errorf("Steps = %d, want 3", s)
	}

	// Undoing removes the APEv2 tag again.
	if err := WriteAudio(path, Tags{Title: "T"}, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !bytes.HasSuffix(data, audio) {
		t.Error("APEv2 tag left after undo")
	}
}

// A change made by MP3Gain is reported and undone in both write modes.
func TestReadMP3GainChange(t *testing.T) {
	v1 := id3v1Tag("V1", "V1", 1, 1)
	for _, overwrite := range []bool{true, false} {
		path := writeFile(t, id3WithTXXX(t), fakeAudio(), mp3gainAPE("-002,-002,N"), v1)
		tg, err := Read(path)
		if err != nil || tg.Gain.Steps != 2 {
			t.Fatalf("Read = %+v, %v; want Steps 2", tg, err)
		}

		// Saving other fields keeps the undo information.
		tg.Title = "New"
		if err := Write(path, tg, Overwrite(overwrite)); err != nil {
			t.Fatal(err)
		}
		if s := readSteps(t, path); s != 2 {
			t.Errorf("overwrite=%v: Steps after saving = %d, want 2", overwrite, s)
		}
		if !overwrite {
			if len(fileAPE(t, path)) != 4 {
				t.Errorf("APEv2 items changed although the gain did not: %+v", fileAPE(t, path))
			}
		}

		// Undo: the stale MP3Gain values go, foreign items stay.
		tg.Gain.Steps = 0
		if err := WriteAudio(path, tg, func([]byte) error { return nil }, Overwrite(overwrite)); err != nil {
			t.Fatal(err)
		}
		if s := readSteps(t, path); s != 0 {
			t.Errorf("overwrite=%v: Steps after undo = %d, want 0", overwrite, s)
		}
		items := fileAPE(t, path)
		data, _ := os.ReadFile(path)
		if overwrite {
			if items != nil {
				t.Errorf("APEv2 items %+v after undo in overwrite mode", items)
			}
		} else {
			if len(items) != 1 || items[0].key != "Artist" {
				t.Errorf("APEv2 items = %+v, want only Artist", items)
			}
			if !bytes.HasSuffix(data[:len(data)-len(v1)], encodeAPE(items)) || string(data[len(data)-len(v1):][:3]) != "TAG" {
				t.Error("APEv2 tag is not directly before the ID3v1 tag")
			}
		}
	}
}

func TestReadGainSources(t *testing.T) {
	audio := fakeAudio()
	for _, c := range []struct {
		name  string
		parts [][]byte
		want  int
	}{
		{"legacy", [][]byte{id3WithTXXX(t, txxxSteps, "4"), audio}, 4},
		{"id3 undo", [][]byte{id3WithTXXX(t, keyUndo, "-003,-003,N"), audio}, 3},
		{"ape over id3", [][]byte{id3WithTXXX(t, keyUndo, "-003,-003,N"), audio, mp3gainAPE("-001,-001,N")}, 1},
		{"legacy plus ape", [][]byte{id3WithTXXX(t, txxxSteps, "1"), audio, mp3gainAPE("-002,-002,N")}, 3},
		{"undone by mp3gain", [][]byte{id3WithTXXX(t), audio, mp3gainAPE("+000,+000,N")}, 0},
		{"no undo", [][]byte{id3WithTXXX(t), audio, mp3gainAPE("")}, 0},
	} {
		if s := readSteps(t, writeFile(t, c.parts...)); s != c.want {
			t.Errorf("%s: Steps = %d, want %d", c.name, s, c.want)
		}
	}
}

func TestWriteMigratesLegacySteps(t *testing.T) {
	path := writeFile(t, id3WithTXXX(t, txxxSteps, "4", "X", "y"), fakeAudio())
	tg, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(path, tg, Overwrite(false)); err != nil {
		t.Fatal(err)
	}
	if u, _ := apeValue(fileAPE(t, path), keyUndo); u != "-004,-004,N" {
		t.Errorf("%s = %q, want -004,-004,N", keyUndo, u)
	}
	tag, ids := frameIDs(t, path)
	if ids["TXXX"] != 1 || tag.GetFrames("TXXX")[0].(id3v2.UserDefinedTextFrame).Description != "X" {
		t.Errorf("frames = %v, want only TXXX X", ids)
	}
	if s := readSteps(t, path); s != 4 {
		t.Errorf("Steps = %d, want 4", s)
	}
}

// A per-channel change made by MP3Gain is shifted, not replaced.
func TestWriteKeepsChannelDifference(t *testing.T) {
	path := writeFile(t, id3WithTXXX(t), fakeAudio(), mp3gainAPE("-002,-004,W"))
	tg, err := Read(path)
	if err != nil || tg.Gain.Steps != 2 {
		t.Fatalf("Read = %+v, %v; want Steps 2", tg, err)
	}
	tg.Gain.Steps = 3
	if err := WriteAudio(path, tg, func([]byte) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if u, _ := apeValue(fileAPE(t, path), keyUndo); u != "-003,-005,N" {
		t.Errorf("%s = %q, want -003,-005,N", keyUndo, u)
	}
}

func TestReadFromFewReadsWithAPE(t *testing.T) {
	head := id3WithTXXX(t)
	b := bytes.Join([][]byte{head, bytes.Repeat(fakeAudio(), 256), mp3gainAPE("-002,-002,N"), id3v1Tag("V1", "V1", 1, 1)}, nil)
	r := &eofReaderAt{b: b}
	got, err := ReadFrom(r, int64(len(b)))
	if err != nil || got.Gain.Steps != 2 || got.Title != "T" {
		t.Fatalf("ReadFrom = %+v, %v", got, err)
	}
	if r.reads > 3 {
		t.Errorf("ReadFrom made %d ReadAt calls, want <= 3", r.reads)
	}
}
