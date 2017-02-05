package avi

import (
	"encoding/binary"
	"io"
)

type Description struct {
	Width     int
	Height    int
	RateNum   int
	RateDenom int
}

type Stream struct {
	w    io.Writer
	desc Description
}

func (s *Stream) writeRIFFHeader() error {
	h := RIFFHeader{
		FourCC: FourCC{'R', 'I', 'F', 'F'},
		Size:   0xFFFFFFFF,
		Type:   FourCC{'A', 'V', 'I', ' '},
	}
	return binary.Write(s.w, binary.LittleEndian, h)
}

func (s *Stream) start() error {
	hdrl := Hdrl{
		ListHeader: ListHeader{
			ChunkHeader: ChunkHeader{
				FourCC: FourCC{'L', 'I', 'S', 'T'},
				Size:   4 + 8 + 56 + 8 + 4 + 8 + 56 + 40,
			},
			Type: FourCC{'h', 'd', 'r', 'l'},
		},
		AVIH: AVIMainHeader{
			ChunkHeader: ChunkHeader{
				FourCC: FourCC{'a', 'v', 'i', 'h'},
				Size:   56,
			},

			MicroSecPerFrame:    0,
			MaxBytesPerSec:      0,
			PaddingGranularity:  0, // XXX
			Flags:               0,
			TotalFrames:         0,
			InitialFrames:       0,
			Streams:             1,
			SuggestedBufferSize: 0,
			Width:               uint32(s.desc.Width),
			Height:              uint32(s.desc.Height),
		},
		Strl: []Strl{
			Strl{
				ListHeader: ListHeader{
					ChunkHeader: ChunkHeader{
						FourCC: FourCC{'L', 'I', 'S', 'T'},
						Size:   4 + 8 + 56 + 40, // looks right
					},
					Type: FourCC{'s', 't', 'r', 'l'},
				},
				Strh: AVIStreamHeader{
					ChunkHeader: ChunkHeader{
						FourCC: FourCC{'s', 't', 'r', 'h'},
						Size:   56, // looks right
					},
					FCCType:    FourCC{'v', 'i', 'd', 's'},
					FCCHandler: FourCC{},
					// Flags               uint32
					// Priority            uint16
					// Language            uint16
					// InitialFrames       uint32
					Scale: uint32(s.desc.RateDenom),
					Rate:  uint32(s.desc.RateNum),
					// Start               uint32
					// Length              uint32
					// SuggestedBufferSize uint32
					// Quality             uint32
					// SampleSize          uint32
					Frame: struct {
						Left   int16
						Top    int16
						Right  int16
						Bottom int16
					}{
						Left:   0,
						Top:    0,
						Right:  int16(s.desc.Width),
						Bottom: int16(s.desc.Height),
					},
				},
				Strf: Strf{
					ChunkHeader: ChunkHeader{
						FourCC: FourCC{'s', 't', 'r', 'f'},
						Size:   40,
					},
					BitmapInfoHeader: BitmapInfoHeader{
						Size:          0, // WTF
						Width:         int32(s.desc.Width),
						Height:        int32(-s.desc.Height),
						Planes:        1,
						BitCount:      32,
						Compression:   FourCC{},
						SizeImage:     0,
						XPelsPerMeter: 0, // WTF
						YPelsPerMeter: 0, // WTF
						ClrUsed:       0, // WTF
						ClrImportant:  0, // WTF
					},
				},
			},
		},
	}

	if err := writeHdrl(s.w, &hdrl); err != nil {
		return err
	}

	movi := ListHeader{
		ChunkHeader: ChunkHeader{
			FourCC: FourCC{'L', 'I', 'S', 'T'},
			Size:   0,
		},
		Type: FourCC{'m', 'o', 'v', 'i'},
	}
	if err := writeListHeader(s.w, movi); err != nil {
		return err
	}
	return nil
}

func (s *Stream) SendFrame(b []byte) error {
	chunk := Chunk{
		ChunkHeader: ChunkHeader{
			FourCC: FourCC{'0', '0', 'd', 'b'},
			Size:   uint32(len(b)),
		},
		Data: b,
	}
	return writeChunk(s.w, chunk)
}

func NewStream(w io.Writer, desc Description) (*Stream, error) {
	// TODO figure out a better name. "stream" already has a distinct
	// meaning in the AVI format, and we mean something else here.
	// Maybe "pipe"?
	s := &Stream{w, desc}
	if err := s.writeRIFFHeader(); err != nil {
		return nil, err
	}
	if err := s.start(); err != nil {
		return nil, err
	}

	return s, nil
}
