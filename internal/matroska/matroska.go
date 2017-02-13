package matroska

import (
	"io"
	"time"

	"honnef.co/go/xcapture/internal/matroska/ebml"
)

type MKV struct {
	SegmentUID      [16]byte
	SegmentFilename string
	PrevUID         [16]byte
	PrevFilename    string
	NextUID         [16]byte
	NextFilename    string
	SegmentFamily   [][16]byte
	// TODO ChapterTranslate
	TimecodeScale time.Duration
	Duration      time.Duration
	Date          time.Time
	Title         string
	WritingApp    string
}

func (mkv *MKV) Size() int {
	return mkv.generate().Size()
}

func (mkv *MKV) generate() ebml.Element {
	var elts []ebml.Element
	SegmentUID(ebml.Binary(mkv.SegmentUID[:]))
	if mkv.SegmentFilename != "" {
		elts = append(elts, SegmentFilename(ebml.UTF8(mkv.SegmentFilename)))
	}
	PrevUID(ebml.Binary(mkv.PrevUID[:]))
	if mkv.PrevFilename != "" {
		elts = append(elts, PrevFilename(ebml.UTF8(mkv.PrevFilename)))
	}
	NextUID(ebml.Binary(mkv.NextUID[:]))
	if mkv.NextFilename != "" {
		elts = append(elts, NextFilename(ebml.UTF8(mkv.NextFilename)))
	}
	for _, sf := range mkv.SegmentFamily {
		elts = append(elts, SegmentFamily(ebml.Binary(sf[:])))
	}
	ts := mkv.TimecodeScale
	if ts == 0 {
		ts = 1
	}
	elts = append(elts, TimecodeScale(ebml.Uint(uint64(ts))))
	// Duration
	// Date
	if mkv.Title != "" {
		elts = append(elts, Title(ebml.UTF8(mkv.Title)))
	}
	elts = append(elts, MuxingApp(ebml.UTF8("honnef.co/go/mkv")))
	elts = append(elts, WritingApp(ebml.UTF8(mkv.WritingApp)))

	return ebml.Element{}
}

func (mkv *MKV) Write(w io.Writer) error {
	return nil
}

func _() {
	SeekHead(
		ebml.Void(ebml.Padding(1024)))
}
