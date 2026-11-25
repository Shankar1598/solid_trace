package storage

import "testing"

func TestTheTypeOfAnEvidenceFileIsFoundFromItsFirstBytes(t *testing.T) {
	for _, c := range []struct {
		name, head, want string
	}{
		{"PNG", "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR", "image/png"},
		{"JPEG", "\xff\xd8\xff\xe0\x00\x10JFIF", "image/jpeg"},
		{"GIF 87a", "GIF87a\x01\x00", "image/gif"},
		{"GIF 89a", "GIF89a\x01\x00", "image/gif"},
		{"WebP", "RIFF\x24\x00\x00\x00WEBPVP8 ", "image/webp"},
		{"MP4 of ffmpeg", "\x00\x00\x00\x20ftypisom\x00\x00\x02\x00isomiso2avc1mp41", "video/mp4"},
		{"MP4 with a compatible brand only", "\x00\x00\x00\x18ftypXXXX\x00\x00\x00\x00mp42XXXX", "video/mp4"},
		{"WebM", "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\xf7\x81\x01\x42\x82\x84webm\x42\x87\x81\x04", "video/webm"},
		{"Matroska", "\x1a\x45\xdf\xa3\x9f\x42\x86\x81\x01\x42\x82\x88matroska", ""},
		{"HEIC", "\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic", ""},
		{"QuickTime", "\x00\x00\x00\x14ftypqt  \x00\x00\x02\x00qt  ", ""},
		{"an ftyp box longer than the bytes", "\x00\x00\x01\x00ftypisom", ""},
		{"a RIFF that is not WebP", "RIFF\x24\x00\x00\x00WAVEfmt ", ""},
		{"SVG", `<svg xmlns="http://www.w3.org/2000/svg">`, ""},
		{"HTML", "<!doctype html>", ""},
		{"PDF", "%PDF-1.7", ""},
		{"empty", "", ""},
	} {
		got, ok := EvidenceContentType([]byte(c.head))
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s: expected %q, got %q (%v)", c.name, c.want, got, ok)
		}
	}
}
