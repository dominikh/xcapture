package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"honnef.co/go/matroska"
	"honnef.co/go/matroska/ebml"
	"honnef.co/go/xcapture/internal/shm"

	"github.com/BurntSushi/xgb/composite"
	xshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
)

const bytesPerPixel = 4
const numPages = 3

func min(xs ...int) int {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

// TODO(dh): this definition of a window is specific to Linux. On
// Windows, for example, we wouldn't have an integer specifier for the
// window.

type Window struct {
	Width       int
	Height      int
	BorderWidth int
	ID          int
}

type Canvas struct {
	Width  int
	Height int
}

type Buffer struct {
	Pages    int
	PageSize int
	Data     []byte
	ShmID    int
}

func (b Buffer) PageOffset(idx int) int {
	return b.PageSize * idx
}

func (b Buffer) Page(idx int) []byte {
	offset := b.PageOffset(idx)
	size := b.PageSize
	return b.Data[offset : offset+size : offset+size]
}

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

func NewBuffer(pageSize, pages int) (Buffer, error) {
	size := pageSize * pages
	seg, err := shm.Create(size)
	if err != nil {
		return Buffer{}, err
	}
	data, err := seg.Attach()
	if err != nil {
		return Buffer{}, err
	}
	sh := &reflect.SliceHeader{
		Data: uintptr(data),
		Len:  size,
		Cap:  size,
	}
	b := (*(*[]byte)(unsafe.Pointer(sh)))
	return Buffer{
		Pages:    pages,
		PageSize: pageSize,
		Data:     b,
		ShmID:    seg.ID,
	}, nil
}

func parseSize(s string) (width, height int, err error) {
	err = fmt.Errorf("%q is not a valid size specification", s)
	if len(s) < 3 {
		return 0, 0, err
	}
	parts := strings.Split(s, "x")
	if len(parts) != 2 {
		return 0, 0, err
	}
	width, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid width: %s", err)
	}
	height, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid height: %s", err)
	}
	return width, height, err
}

func main() {
	fps := flag.Uint("fps", 60, "FPS")
	winID := flag.Int("win", 0, "Window ID")
	size := flag.String("size", "", "Canvas size in the format WxH in pixels. Defaults to the initial size of the captured window")
	flag.Parse()

	win := Window{ID: *winID}

	xu, err := xgbutil.NewConn()
	if err != nil {
		log.Fatal("Couldn't connect to X server:", err)
	}
	if err := composite.Init(xu.Conn()); err != nil {
		log.Fatal("COMPOSITE extension is not available:", err)
	}
	if err := xfixes.Init(xu.Conn()); err != nil {
		log.Fatal("XFIXES extension is not available:", err)
	}
	xfixes.QueryVersion(xu.Conn(), 1, 0)
	if err := xshm.Init(xu.Conn()); err != nil {
		// TODO(dh) implement a slower version that is not using SHM
		log.Fatal("MIT-SHM extension is not available:", err)
	}
	if err := composite.RedirectWindowChecked(xu.Conn(), xproto.Window(win.ID), composite.RedirectAutomatic).Check(); err != nil {
		if err, ok := err.(xproto.AccessError); ok {
			log.Fatal("Can't capture window, another program seems to be capturing it already:", err)
		}
		log.Fatal("Can't capture window:", err)
	}
	pix, err := xproto.NewPixmapId(xu.Conn())
	if err != nil {
		log.Fatal("Could not obtain ID for pixmap:", err)
	}
	composite.NameWindowPixmap(xu.Conn(), xproto.Window(win.ID), pix)

	segID, err := xshm.NewSegId(xu.Conn())
	if err != nil {
		log.Fatal("Could not obtain ID for SHM:", err)
	}

	// Register event before we query the window size for the first
	// time. Otherwise we could race and miss a window resize.
	err = xproto.ChangeWindowAttributesChecked(xu.Conn(), xproto.Window(win.ID),
		xproto.CwEventMask, []uint32{uint32(xproto.EventMaskStructureNotify)}).Check()
	if err != nil {
		log.Fatal("Couldn't monitor window for size changes:", err)
	}
	geom, err := xproto.GetGeometry(xu.Conn(), xproto.Drawable(win.ID)).Reply()
	if err != nil {
		log.Fatal("Could not determine window dimensions:", err)
	}

	win.Width = int(geom.Width)
	win.Height = int(geom.Height)
	win.BorderWidth = int(geom.BorderWidth)
	var canvas Canvas
	if *size != "" {
		width, height, err := parseSize(*size)
		if err != nil {
			log.Fatal(err)
		}
		canvas = Canvas{width, height}
	} else {
		canvas = Canvas{
			Width:  win.Width,
			Height: win.Height,
		}
	}

	buf, err := NewBuffer(int(canvas.Width)*int(canvas.Height)*bytesPerPixel, numPages)
	if err != nil {
		log.Fatal("Could not create shared memory:", err)
	}
	if err := xshm.AttachChecked(xu.Conn(), segID, uint32(buf.ShmID), false).Check(); err != nil {
		log.Fatal("Could not attach shared memory to X server:", err)
	}

	i := 0
	ch := make(chan []byte)

	bmp := BitmapInfoHeader{
		Width:    int32(canvas.Width),
		Height:   int32(-canvas.Height),
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
				matroska.DefaultDuration(ebml.Uint(time.Second/time.Duration(*fps))),
				matroska.CodecID(ebml.String("V_MS/VFW/FOURCC")),
				matroska.CodecPrivate(ebml.Binary(codec.Bytes())),
				matroska.Video(
					matroska.PixelWidth(ebml.Uint(canvas.Width)),
					matroska.PixelHeight(ebml.Uint(canvas.Height)),
					matroska.ColourSpace(ebml.Binary("BGRA")),
					matroska.Colour(
						matroska.BitsPerChannel(ebml.Uint(8)))))))

	idx := -1
	block := make([]byte, buf.PageSize+4)
	block[0] = 129
	block[3] = 128
	sendFrame := func(b []byte) {
		idx++
		copy(block[4:], b)
		e.Emit(
			matroska.Cluster(
				matroska.Timecode(ebml.Uint(idx*int(time.Second/time.Duration(*fps)))),
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
			fmt.Fprintf(os.Stderr, "\rFrame time: %14s (%4.2f FPS); %5d dropped          ", ts.Sub(pts), fps, dropped)
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
		var cfgev *xproto.ConfigureNotifyEvent
		for {
			ev, err := xu.Conn().PollForEvent()
			if err != nil {
				continue
			}
			if ev == nil {
				break
			}
			if ev, ok := ev.(xproto.ConfigureNotifyEvent); ok {
				cfgev = &ev
			}
		}
		if cfgev != nil {
			if int(cfgev.Width) != win.Width || int(cfgev.Height) != win.Height || int(cfgev.BorderWidth) != win.BorderWidth {
				win.Width = int(cfgev.Width)
				win.Height = int(cfgev.Height)
				win.BorderWidth = int(cfgev.BorderWidth)

				// DRY
				xproto.FreePixmap(xu.Conn(), pix)
				var err error
				pix, err = xproto.NewPixmapId(xu.Conn())
				if err != nil {
					log.Fatal("Could not obtain ID for pixmap:", err)
				}
				composite.NameWindowPixmap(xu.Conn(), xproto.Window(win.ID), pix)
			}
		}
		offset := buf.PageOffset(i)
		w := min(win.Width, canvas.Width)
		h := min(win.Height, canvas.Height)

		_, err := xshm.GetImage(xu.Conn(), xproto.Drawable(pix), int16(win.BorderWidth), int16(win.BorderWidth), uint16(w), uint16(h), 0xFFFFFFFF, xproto.ImageFormatZPixmap, segID, uint32(offset)).Reply()
		if err != nil {
			log.Println("Could not fetch window contents:", err)
			continue
		}

		page := buf.Page(i)

		if w < canvas.Width || h < canvas.Height {
			i = (i + 1) % numPages
			dest := buf.Page(i)
			for i := range dest {
				dest[i] = 0
			}
			for i := 0; i < h; i++ {
				copy(dest[i*canvas.Width*bytesPerPixel:], page[i*w*bytesPerPixel:(i+1)*w*bytesPerPixel])
			}
			page = dest
		}

		drawCursor(xu, win, buf, page, canvas)

		ch <- page
		i = (i + 1) % numPages
	}
}

func drawCursor(xu *xgbutil.XUtil, win Window, buf Buffer, page []byte, canvas Canvas) {
	// TODO(dh): We don't need to fetch the cursor image every time.
	// We could listen to cursor notify events, fetch the cursor if we
	// haven't seen it yet, then cache the cursor.
	cursor, err := xfixes.GetCursorImage(xu.Conn()).Reply()
	if err != nil {
		return
	}
	pos, err := xproto.TranslateCoordinates(xu.Conn(), xu.RootWin(), xproto.Window(win.ID), cursor.X, cursor.Y).Reply()
	if err != nil {
		return
	}
	maxWidth := min(win.Width, canvas.Width)
	maxHeight := min(win.Height, canvas.Height)
	if pos.DstY < 0 || pos.DstX < 0 || int(pos.DstY) > maxHeight || int(pos.DstX) > maxWidth {
		// cursor outside of our window
		return
	}
	for i, p := range cursor.CursorImage {
		row := i/int(cursor.Width) + int(pos.DstY) - int(cursor.Yhot)
		col := i%int(cursor.Width) + int(pos.DstX) - int(cursor.Xhot)
		if row >= canvas.Height || col >= canvas.Width || row < 0 || col < 0 {
			// cursor is partially off-screen
			break
		}
		off := row*canvas.Width*bytesPerPixel + col*bytesPerPixel
		alpha := (p >> 24) + 1
		invAlpha := uint32(256 - (p >> 24))

		page[off+3] = 255
		page[off+2] = byte((alpha*uint32(byte(p>>16)) + invAlpha*uint32(page[off+2])) >> 8)
		page[off+1] = byte((alpha*uint32(byte(p>>8)) + invAlpha*uint32(page[off+1])) >> 8)
		page[off+0] = byte((alpha*uint32(byte(p>>0)) + invAlpha*uint32(page[off+0])) >> 8)
	}
}
