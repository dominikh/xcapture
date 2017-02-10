package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"time"
	"unsafe"

	"honnef.co/go/matroska"
	"honnef.co/go/matroska/ebml"

	"github.com/BurntSushi/xgb/composite"
	xshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/BurntSushi/xgbutil/xevent"
	"github.com/ghetzel/shmtool/shm" // TODO switch to pure Go implementation
)

type BitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   [4]byte
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func main() {
	fps := flag.Uint("fps", 60, "FPS")
	win := flag.Uint("win", 0, "Window ID")
	flag.Parse()

	xu, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal("Couldn't connect to X server:", err)
	}
	if err := composite.Init(xu.Conn()); err != nil {
		log.Fatal("COMPOSITE extension is not available:", err)
	}
	if err := xshm.Init(xu.Conn()); err != nil {
		// TODO(dh) implement a slower version that is not using SHM
		log.Fatal("MIT-SHM extension is not available:", err)
	}
	if err := composite.RedirectWindowChecked(xu.Conn(), xproto.Window(*win), composite.RedirectAutomatic).Check(); err != nil {
		if err, ok := err.(xproto.AccessError); ok {
			log.Fatal("Can't capture window, another program seems to be capturing it already:", err)
		}
		log.Fatal("Can't capture window:", err)
	}
	pix, err := xproto.NewPixmapId(xu.Conn())
	if err != nil {
		log.Fatal("Could not obtain ID for pixmap:", err)
	}
	composite.NameWindowPixmap(xu.Conn(), xproto.Window(*win), pix)

	// TODO free pixmap if window goes away or is resized, get new pixmap

	segID, err := xshm.NewSegId(xu.Conn())
	if err != nil {
		log.Fatal("Could not obtain ID for SHM:", err)
	}

	geom, err := xproto.GetGeometry(xu.Conn(), xproto.Drawable(*win)).Reply()
	if err != nil {
		log.Fatal("Could not determine window dimensions:", err)
	}
	width := geom.Width
	height := geom.Height
	frameSize := int(width) * int(height) * 4

	seg, err := shm.Create(frameSize * 2)
	if err != nil {
		log.Fatal("Could not create shared memory:", err)
	}
	if err := xshm.AttachChecked(xu.Conn(), segID, uint32(seg.Id), false).Check(); err != nil {
		log.Fatal("Could not attach shared memory to X server:", err)
	}
	data, err := seg.Attach()
	if err != nil {
		log.Fatal("Could not attach shared memory:", err)
	}
	i := 0
	ch := make(chan []byte)

	bmp := BitmapInfoHeader{
		Width:    int32(width),
		Height:   int32(-height),
		Planes:   1,
		BitCount: 32,
	}
	codec := &bytes.Buffer{}
	if err := binary.Write(codec, binary.LittleEndian, bmp); err != nil {
		panic(err)
	}

	e := ebml.NewEncoder(os.Stdout)
	e.Emit(
		ebml.EBML(
			ebml.DocType(ebml.String("matroska")),
			ebml.DocTypeVersion(ebml.Uint(4)),
			ebml.DocTypeReadVersion(ebml.Uint(1))))

	e.EmitHeader(matroska.Segment, -1)
	e.Emit(
		matroska.Info(
			matroska.TimecodeScale(ebml.Uint(1)),
			matroska.MuxingApp(ebml.UTF8("honnef.co/go/mkv")),
			matroska.WritingApp(ebml.UTF8("xcapture"))))

	e.Emit(
		matroska.Tracks(
			matroska.TrackEntry(
				matroska.TrackNumber(ebml.Uint(1)),
				matroska.TrackUID(ebml.Uint(0xDEADBEEF)),
				matroska.TrackType(ebml.Uint(1)),
				matroska.FlagLacing(ebml.Uint(0)),
				matroska.DefaultDuration(ebml.Uint(time.Second/60)),
				matroska.CodecID(ebml.String("V_MS/VFW/FOURCC")),
				matroska.CodecPrivate(ebml.Binary(codec.Bytes())),
				matroska.Video(
					matroska.PixelWidth(ebml.Uint(width)),
					matroska.PixelHeight(ebml.Uint(height)),
					matroska.ColourSpace(ebml.Binary("BGRA")),
					matroska.Colour(
						matroska.BitsPerChannel(ebml.Uint(8)))))))

	go xevent.Main(xu)
	configCb := func(xu *xgbutil.XUtil, ev xevent.ConfigureNotifyEvent) {
		if ev.Width != width || ev.Height != height {
			log.Println("new window size")
		}
	}
	xevent.ConfigureNotifyFun(configCb).Connect(xu, xproto.Window(*win))
	err = xproto.ChangeWindowAttributesChecked(xu.Conn(), xproto.Window(*win),
		xproto.CwEventMask, []uint32{uint32(xproto.EventMaskStructureNotify)}).Check()
	if err != nil {
		log.Fatal("Couldn't monitor window for size changes:", err)
	}

	idx := -1
	var prevFrame []byte
	sendFrame := func(b []byte) {
		idx++
		if b == nil {
			b = prevFrame
		}
		prevFrame = b
		block := []byte{
			129,
			0, 0,
			128,
		}
		block = append(block, b...)
		e.Emit(
			matroska.Cluster(
				matroska.Timecode(ebml.Uint(idx*int(time.Second/60))),
				matroska.Position(ebml.Uint(0)),
				matroska.SimpleBlock(ebml.Binary(block))))

		if e.Err != nil {
			log.Fatal(err)
		}
	}

	go func() {
		d := time.Second / time.Duration(*fps)
		t := time.NewTicker(d)
		pts := time.Now()
		dropped := 0
		for ts := range t.C {
			fps := float64(time.Second) / float64(ts.Sub(pts))
			fmt.Fprintf(os.Stderr, "\rFrame time: %14s (%4.2f FPS); %5d dropped; %4dx%4d -> %4dx%4d          ", ts.Sub(pts), fps, dropped, width, height, width, height)
			pts = ts
			select {
			case b := <-ch:
				sendFrame(b)
			default:
				dropped++
				sendFrame(nil)
			}
		}
	}()

	for {
		// TODO get window's actual dimensions
		offset := i * frameSize
		_, err := xshm.GetImage(xu.Conn(), xproto.Drawable(pix), 0, 0, width, height, 0xFFFFFFFF, xproto.ImageFormatZPixmap, segID, uint32(offset)).Reply()
		if err != nil {
			log.Fatal("Could not fetch window contents:", err)
		}
		sh := reflect.SliceHeader{
			Data: uintptr(data),
			Len:  frameSize * 2,
			Cap:  frameSize * 2,
		}
		b := (*(*[]byte)(unsafe.Pointer(&sh)))[offset : offset+frameSize]
		ch <- b
		i = (i + 1) % 2
	}
}
