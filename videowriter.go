package main

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"

	"honnef.co/go/matroska"
	"honnef.co/go/matroska/ebml"
)

type VideoWriter struct {
	enc       *ebml.Encoder
	firstTime time.Time // XXX rename
	prevFrame Frame
	block     []byte
	canvas    Canvas
	fps       int
}

func NewVideoWriter(c Canvas, fps int, w io.Writer) *VideoWriter {
	const hdrSize = 4
	return &VideoWriter{
		enc:    ebml.NewEncoder(w),
		block:  make([]byte, c.Width*c.Height*bytesPerPixel+hdrSize),
		canvas: c,
		fps:    fps,
	}
}

func (vw *VideoWriter) Start() error {
	vw.block[0] = 129

	bmp := BitmapInfoHeader{
		Width:    int32(vw.canvas.Width),
		Height:   int32(-vw.canvas.Height),
		Planes:   1,
		BitCount: 32,
	}
	codec := &bytes.Buffer{}
	if err := binary.Write(codec, binary.LittleEndian, bmp); err != nil {
		panic(err)
	}

	vw.enc.Emit(
		ebml.EBML(
			ebml.DocType(ebml.String("matroska")),
			ebml.DocTypeVersion(ebml.Uint(4)),
			ebml.DocTypeReadVersion(ebml.Uint(1))))

	vw.enc.EmitHeader(matroska.Segment, -1)
	vw.enc.Emit(
		matroska.Info(
			matroska.TimecodeScale(ebml.Uint(1)),
			matroska.MuxingApp(ebml.UTF8("honnef.co/go/mkv")),
			matroska.WritingApp(ebml.UTF8("xcapture"))))

	vw.enc.Emit(
		matroska.Tracks(
			matroska.TrackEntry(
				matroska.TrackNumber(ebml.Uint(1)),
				matroska.TrackUID(ebml.Uint(0xDEADBEEF)),
				matroska.TrackType(ebml.Uint(1)),
				matroska.FlagLacing(ebml.Uint(0)),
				matroska.DefaultDuration(ebml.Uint(time.Second/time.Duration(vw.fps))),
				matroska.CodecID(ebml.String("V_MS/VFW/FOURCC")),
				matroska.CodecPrivate(ebml.Binary(codec.Bytes())),
				matroska.Video(
					matroska.PixelWidth(ebml.Uint(vw.canvas.Width)),
					matroska.PixelHeight(ebml.Uint(vw.canvas.Height)),
					matroska.ColourSpace(ebml.Binary("BGRA")),
					matroska.Colour(
						matroska.BitsPerChannel(ebml.Uint(8)))))))
	return vw.enc.Err
}

func (vw *VideoWriter) SendFrame(frame Frame) error {
	if vw.prevFrame.Data == nil && frame.Data != nil {
		// This is our first frame
		vw.prevFrame = frame
		vw.firstTime = frame.Time
		return nil
	}
	if frame.Data == nil {
		// No new frame. If it's been 1s since the last frame,
		// emit the previous frame again, to avoid having very
		// long frame durations, which stalls some video players
		// and risks data loss in case of a crash.
		if frame.Time.Sub(vw.prevFrame.Time) < time.Second {
			return nil
		}
		frame.Data = vw.prevFrame.Data
	}
	copy(vw.block[4:], vw.prevFrame.Data)
	ts := vw.prevFrame.Time.Sub(vw.firstTime)
	vw.enc.Emit(
		matroska.Cluster(
			matroska.Timecode(ebml.Uint(ts)),
			matroska.Position(ebml.Uint(0)),
			matroska.BlockGroup(
				matroska.BlockDuration(ebml.Uint(frame.Time.Sub(vw.prevFrame.Time))),
				matroska.Block(ebml.Binary(vw.block)))))

	vw.prevFrame = frame
	return vw.enc.Err
}
