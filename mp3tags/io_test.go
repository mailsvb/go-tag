package mp3tags

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

// fixtures are files with every kind of tag layout the package handles.
func fixtures(t *testing.T) map[string][]byte {
	ext := append([]byte("TAG+"), make([]byte, id3v1ExtSize-4)...)
	return map[string][]byte{
		"id3v24":    bytes.Join([][]byte{id3v24Tag(t), fakeAudio()}, nil),
		"id3v1":     bytes.Join([][]byte{fakeAudio(), id3v1Tag("V1 Title", "V1 Artist", 5, 8)}, nil),
		"all":       bytes.Join([][]byte{id3v24Tag(t), id3v24Tag(t), fakeAudio(), lyrics3Tag(), apeTag(), ext, id3v1Tag("V1", "V1", 1, 1)}, nil),
		"none":      fakeAudio(),
		"truncated": id3v24Tag(t)[:40],
	}
}

// eofReaderAt returns io.EOF together with a full read at the end of the
// data, as io.ReaderAt allows and some remote file implementations do.
type eofReaderAt struct {
	b     []byte
	reads int
	read  int64 // bytes read
}

func (r *eofReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	n, err := bytes.NewReader(r.b).ReadAt(p, off)
	r.read += int64(n)
	if err == nil && off+int64(n) == int64(len(r.b)) {
		err = io.EOF
	}
	return n, err
}

func TestReadFromMatchesRead(t *testing.T) {
	for name, b := range fixtures(t) {
		want, wantErr := Read(writeFile(t, b))
		got, err := ReadFrom(&eofReaderAt{b: b}, int64(len(b)))
		if (err != nil) != (wantErr != nil) || !got.Equal(want) {
			t.Errorf("%s: ReadFrom = %+v, %v; Read = %+v, %v", name, got, err, want, wantErr)
		}
	}
}

func TestReadAudioFromMatchesReadAudio(t *testing.T) {
	for name, b := range fixtures(t) {
		if name == "truncated" {
			continue
		}
		want, err := ReadAudio(writeFile(t, b))
		if err != nil {
			t.Fatal(err)
		}
		got, err := ReadAudioFrom(&eofReaderAt{b: b}, int64(len(b)))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s: ReadAudioFrom differs from ReadAudio (err %v)", name, err)
		}
	}
}

func TestWriteToMatchesWrite(t *testing.T) {
	transform := func(b []byte) error {
		for i := range b {
			b[i] ^= 0x5A
		}
		return nil
	}
	tags := Tags{Title: "Neu", Artist: "A", Cover: &Picture{MIME: "image/png", Data: []byte("c")}, Gain: Gain{Steps: 1}}
	for name, b := range fixtures(t) {
		if name == "truncated" {
			continue
		}
		for _, overwrite := range []bool{true, false} {
			for _, tf := range []func([]byte) error{nil, transform} {
				path := writeFile(t, b)
				var err error
				if tf == nil {
					err = Write(path, tags, Overwrite(overwrite))
				} else {
					err = WriteAudio(path, tags, tf, Overwrite(overwrite))
				}
				if err != nil {
					t.Fatal(err)
				}
				want, _ := os.ReadFile(path)

				var got bytes.Buffer
				src := &eofReaderAt{b: b}
				if tf == nil {
					err = WriteTo(&got, src, int64(len(b)), tags, Overwrite(overwrite))
				} else {
					err = WriteAudioTo(&got, src, int64(len(b)), tags, tf, Overwrite(overwrite))
				}
				if err != nil || !sameFile(t, got.Bytes(), want) {
					t.Errorf("%s overwrite=%v transform=%v: WriteTo differs from Write (err %v)",
						name, overwrite, tf != nil, err)
				}
			}
		}
	}
}

// sameFile reports whether the MP3 files a and b hold the same tags, ID3v2
// frames, audio and trailing tags. Bytes are not compared directly because
// id3v2 writes frames in map order.
func sameFile(t *testing.T, a, b []byte) bool {
	t.Helper()
	pa, pb := writeFile(t, a), writeFile(t, b)
	ta, err1 := Read(pa)
	tb, err2 := Read(pb)
	aa, err3 := ReadAudio(pa)
	ab, err4 := ReadAudio(pb)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		t.Fatal(err1, err2, err3, err4)
	}
	_, ia := frameIDs(t, pa)
	_, ib := frameIDs(t, pb)
	trailer := func(b []byte, audio []byte) []byte { return b[bytes.Index(b, audio)+len(audio):] }
	return len(a) == len(b) && ta.Equal(tb) && bytes.Equal(aa, ab) &&
		fmt.Sprint(ia) == fmt.Sprint(ib) && bytes.Equal(trailer(a, aa), trailer(b, ab))
}

func TestReadFromFewReads(t *testing.T) {
	audio := bytes.Repeat(fakeAudio(), 256) // 1 MiB
	path := writeFile(t, fakeAudio())
	if err := Write(path, Tags{Title: "T", Cover: &Picture{MIME: "image/jpeg", Data: make([]byte, 200<<10)}}); err != nil {
		t.Fatal(err)
	}
	tagged, _ := os.ReadFile(path)
	head, _ := readFirstID3v2(bytes.NewReader(tagged), int64(len(tagged)))
	b := bytes.Join([][]byte{head, audio, id3v1Tag("V1", "V1", 1, 1)}, nil)

	r := &eofReaderAt{b: b}
	got, err := ReadFrom(r, int64(len(b)))
	if err != nil || got.Title != "T" || got.Cover == nil {
		t.Fatalf("ReadFrom = %+v, %v", got, err)
	}
	if r.reads > 3 {
		t.Errorf("ReadFrom made %d ReadAt calls, want <= 3", r.reads)
	}
	if limit := int64(len(head) + 10 + id3v1Size); r.read > limit {
		t.Errorf("ReadFrom read %d bytes, want <= %d (no audio)", r.read, limit)
	}
}
