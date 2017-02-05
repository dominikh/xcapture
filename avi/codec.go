package avi

import (
	"encoding/binary"
	"io"
)

type FourCC [4]byte

type RIFFHeader struct {
	FourCC FourCC // 'RIFF'
	Size   uint32
	Type   FourCC // in our case 'AVI '
}

type ChunkHeader struct {
	FourCC FourCC
	Size   uint32 // does not include the padding
}

type Chunk struct {
	ChunkHeader
	Data []byte // padded to uint16 boundaries
}

type ListHeader struct {
	ChunkHeader // 'LIST'
	Type        FourCC
}

// type List struct {
// 	ListHeader
// 	Data []byte
// }

type AVI struct {
	Hdrl Hdrl
	Movi Movi // contains the data for the AVI sequence
	//Idx1 List // contains the index
}

type Hdrl struct {
	ListHeader // 'hdrl'
	AVIH       AVIMainHeader
	Strl       []Strl
}

type Strl struct {
	ListHeader // 'strl'

	Strh AVIStreamHeader
	Strf Strf
}

type Strf struct {
	ChunkHeader      // 'strf'
	BitmapInfoHeader // for audio, this would be a WAVEFORMATEX, but we don't care about audio
}

type AVIStreamHeader struct {
	ChunkHeader         // 'strh'
	FCCType             FourCC
	FCCHandler          FourCC
	Flags               uint32
	Priority            uint16
	Language            uint16
	InitialFrames       uint32
	Scale               uint32
	Rate                uint32
	Start               uint32
	Length              uint32
	SuggestedBufferSize uint32
	Quality             uint32
	SampleSize          uint32
	Frame               struct {
		Left   int16
		Top    int16
		Right  int16
		Bottom int16
	}
}

type AVIMainHeader struct {
	ChunkHeader         // 'avih'
	MicroSecPerFrame    uint32
	MaxBytesPerSec      uint32
	PaddingGranularity  uint32
	Flags               uint32
	TotalFrames         uint32
	InitialFrames       uint32
	Streams             uint32
	SuggestedBufferSize uint32
	Width               uint32
	Height              uint32
	_                   [16]byte
}

type BitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   FourCC
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type Movi struct {
	ListHeader         // 'movi'
	Data       []Chunk // '00' for the first stream, 'db for uncompressed video frame
}

func WriteAVI(w io.Writer, avi *AVI) error {
	if err := writeHdrl(w, &avi.Hdrl); err != nil {
		return err
	}
	if err := writeMovi(w, &avi.Movi); err != nil {
		return err
	}
	return nil
}

func writeListHeader(w io.Writer, h ListHeader) error {
	return binary.Write(w, binary.LittleEndian, h)
}

func writeHdrl(w io.Writer, hdrl *Hdrl) error {
	if err := writeListHeader(w, hdrl.ListHeader); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, hdrl.AVIH); err != nil {
		return err
	}
	if err := writeStrls(w, hdrl.Strl); err != nil {
		return err
	}
	return nil
}

func writeStrl(w io.Writer, strl *Strl) error {
	return binary.Write(w, binary.LittleEndian, strl)
}

func writeStrls(w io.Writer, strls []Strl) error {
	for i := range strls {
		if err := writeStrl(w, &strls[i]); err != nil {
			return err
		}
	}
	return nil
}

func writeMovi(w io.Writer, movi *Movi) error {
	if err := writeListHeader(w, movi.ListHeader); err != nil {
		return err
	}
	return writeChunks(w, movi.Data)
}

func writeChunkHeader(w io.Writer, h ChunkHeader) error {
	return binary.Write(w, binary.LittleEndian, h)
}

func writeChunk(w io.Writer, chunk Chunk) error {
	if err := writeChunkHeader(w, chunk.ChunkHeader); err != nil {
		return err
	}
	if _, err := w.Write(chunk.Data); err != nil {
		return err
	}
	return nil
}

func writeChunks(w io.Writer, chunks []Chunk) error {
	for _, chunk := range chunks {
		if err := writeChunk(w, chunk); err != nil {
			return err
		}
	}
	return nil
}
