package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"honnef.co/go/xcapture/internal/shm"

	"github.com/BurntSushi/xgb"
	"github.com/BurntSushi/xgb/composite"
	"github.com/BurntSushi/xgb/damage"
	xshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xfixes"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
)

const bytesPerPixel = 4
const numPages = 4

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
	ID int

	mu          sync.RWMutex
	width       int
	height      int
	borderWidth int
}

func (w *Window) SetDimensions(width, height, border int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.width = width
	w.height = height
	w.borderWidth = border
}

func (w *Window) Dimensions() (width, height, border int) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.width, w.height, w.borderWidth
}

type Canvas struct {
	Width  int
	Height int
}

type Frame struct {
	Data []byte
	Time time.Time
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

type EventLoop struct {
	conn *xgb.Conn

	mu        sync.RWMutex
	listeners []chan xgb.Event
}

func NewEventLoop(conn *xgb.Conn) *EventLoop {
	el := &EventLoop{conn: conn}
	go el.start()
	return el
}

func (el *EventLoop) Register(ch chan xgb.Event) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.listeners = append(el.listeners, ch)
}

func (el *EventLoop) start() {
	for {
		ev, err := el.conn.WaitForEvent()
		if err != nil {
			continue
		}
		el.mu.RLock()
		ls := el.listeners
		el.mu.RUnlock()
		for _, l := range ls {
			l <- ev
		}
	}
}

type CaptureEvent struct {
	Resized bool
}

type ResizeMonitor struct {
	C    chan CaptureEvent
	elCh chan xgb.Event
	win  *Window
}

func NewResizeMonitor(el *EventLoop, win *Window) *ResizeMonitor {
	res := &ResizeMonitor{
		C:    make(chan CaptureEvent, 1),
		elCh: make(chan xgb.Event),
		win:  win,
	}
	el.Register(res.elCh)
	go res.start()
	return res
}

func (res *ResizeMonitor) start() {
	for ev := range res.elCh {
		if ev, ok := ev.(xproto.ConfigureNotifyEvent); ok {
			w, h, bw := res.win.Dimensions()
			if int(ev.Width) != w || int(ev.Height) != h || int(ev.BorderWidth) != bw {
				w, h, bw = int(ev.Width), int(ev.Height), int(ev.BorderWidth)
				res.win.SetDimensions(w, h, bw)
				select {
				case res.C <- CaptureEvent{true}:
				default:
				}
			}
		}
	}
}

type DamageMonitor struct {
	C    chan CaptureEvent
	elCh chan xgb.Event
	conn *xgb.Conn
	fps  int
	win  *Window
}

func NewDamageMonitor(conn *xgb.Conn, el *EventLoop, win *Window, fps int) *DamageMonitor {
	dmg := &DamageMonitor{
		C:    make(chan CaptureEvent, 1),
		elCh: make(chan xgb.Event),
		conn: conn,
		fps:  fps,
		win:  win,
	}
	el.Register(dmg.elCh)
	go dmg.startDamage()
	go dmg.startCursor()
	return dmg
}

func (dmg *DamageMonitor) startDamage() {
	xdmg, err := damage.NewDamageId(dmg.conn)
	if err != nil {
		// XXX fall back gracefully
		log.Fatal(err)
	}
	damage.Create(dmg.conn, xdmg, xproto.Drawable(dmg.win.ID), damage.ReportLevelRawRectangles)

	for ev := range dmg.elCh {
		if _, ok := ev.(damage.NotifyEvent); ok {
			select {
			case dmg.C <- CaptureEvent{}:
			default:
			}
		}
	}
}

func (dmg *DamageMonitor) startCursor() {
	var prevCursor struct{ X, Y int }
	prevInWindow := true
	d := time.Second / time.Duration(dmg.fps)
	t := time.NewTicker(d)
	for range t.C {
		cursor, err := xproto.QueryPointer(dmg.conn, xproto.Window(dmg.win.ID)).Reply()
		if err != nil {
			log.Println("Couldn't query cursor position:", err)
			continue
		}
		c := struct{ X, Y int }{int(cursor.WinX), int(cursor.WinY)}
		if c == prevCursor {
			continue
		}
		prevCursor = c

		damaged := false
		w, h, _ := dmg.win.Dimensions()
		if c.X < 0 || c.Y < 0 || c.X > w || c.Y > h {
			if prevInWindow {
				// cursor moved out of the window, which requires a redraw
				damaged = true
			}
			prevInWindow = false
		} else {
			damaged = true
		}
		if damaged {
			select {
			case dmg.C <- CaptureEvent{}:
			default:
			}
		}
	}
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
	fps := flag.Uint("fps", 30, "FPS")
	winID := flag.Int("win", 0, "Window ID")
	size := flag.String("size", "", "Canvas size in the format WxH in pixels. Defaults to the initial size of the captured window")
	cfr := flag.Bool("cfr", false, "Use a constant frame rate")
	_ = cfr
	flag.Parse()

	win := &Window{ID: *winID}

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

	win.SetDimensions(int(geom.Width), int(geom.Height), int(geom.BorderWidth))
	var canvas Canvas
	if *size != "" {
		width, height, err := parseSize(*size)
		if err != nil {
			log.Fatal(err)
		}
		canvas = Canvas{width, height}
	} else {
		canvas = Canvas{
			Width:  int(geom.Width),
			Height: int(geom.Height),
		}
	}

	buf, err := NewBuffer(canvas.Width*canvas.Height*bytesPerPixel, numPages)
	if err != nil {
		log.Fatal("Could not create shared memory:", err)
	}
	if err := xshm.AttachChecked(xu.Conn(), segID, uint32(buf.ShmID), false).Check(); err != nil {
		log.Fatal("Could not attach shared memory to X server:", err)
	}

	i := 0
	ch := make(chan Frame)

	tags := map[string]string{
		"DATE_RECORDED": time.Now().UTC().Format("2006-01-02 15:04:05.999"),
		"WINDOW_ID":     strconv.Itoa(win.ID),
	}
	vw := NewVideoWriter(canvas, int(*fps), *cfr, tags, os.Stdout)
	if err := vw.Start(); err != nil {
		log.Fatal("Couldn't write output:", err)
	}

	go func() {
		d := time.Second / time.Duration(*fps)
		t := time.NewTicker(d)
		pts := time.Now()
		min := time.Duration(math.MaxInt64)
		var max time.Duration
		dupped := 0

		frames := uint64(0)
		avg := time.Duration(0)
		for ts := range t.C {
			frames++
			dt := ts.Sub(pts)
			if dt < min {
				min = dt
			}
			if dt > max {
				max = dt
			}
			avg = (avg*time.Duration(frames-1) +
				dt) / time.Duration(frames)

			fps := float64(time.Second) / float64(dt)
			fpsMin := float64(time.Second) / float64(max)
			fpsMax := float64(time.Second) / float64(min)
			fpsAvg := float64(time.Second) / float64(avg)
			dt = roundDuration(dt, 10000)
			fmt.Fprintf(os.Stderr, "\rFrame time: %10s (%4.2f FPS, min %4.2f, max %4.2f, avg %4.2f); %5d dup", dt, fps, fpsMin, fpsMax, fpsAvg, dupped)
			pts = ts
			var err error
			select {
			case frame := <-ch:
				err = vw.SendFrame(frame)
			default:
				dupped++
				err = vw.SendFrame(Frame{Time: ts})
			}
			if err != nil {
				log.Fatal("Couldn't write frame:", err)
			}
		}
	}()

	el := NewEventLoop(xu.Conn())
	res := NewResizeMonitor(el, win)
	var other chan CaptureEvent
	captureEvents := make(chan CaptureEvent, 1)
	if *cfr {
		other = make(chan CaptureEvent)
		go func() {
			for {
				other <- CaptureEvent{}
			}
		}()
	} else {
		if err := damage.Init(xu.Conn()); err != nil {
			// XXX fail back gracefully
			log.Fatal(err)
		}
		damage.QueryVersion(xu.Conn(), 1, 1)
		dmg := NewDamageMonitor(xu.Conn(), el, win, int(*fps))
		other = dmg.C
	}
	go func() {
		for {
			var ev CaptureEvent
			select {
			case ev = <-res.C:
				captureEvents <- ev
			case ev = <-other:
				captureEvents <- ev
			}
		}
	}()
	for ev := range captureEvents {
		if ev.Resized {
			// DRY
			xproto.FreePixmap(xu.Conn(), pix)
			var err error
			pix, err = xproto.NewPixmapId(xu.Conn())
			if err != nil {
				log.Fatal("Could not obtain ID for pixmap:", err)
			}
			composite.NameWindowPixmap(xu.Conn(), xproto.Window(win.ID), pix)
		}

		w, h, bw := win.Dimensions()
		offset := buf.PageOffset(i)
		w = min(w, canvas.Width)
		h = min(h, canvas.Height)

		ts := time.Now()
		_, err := xshm.GetImage(xu.Conn(), xproto.Drawable(pix), int16(bw), int16(bw), uint16(w), uint16(h), 0xFFFFFFFF, xproto.ImageFormatZPixmap, segID, uint32(offset)).Reply()
		if err != nil {
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

		ch <- Frame{Data: page, Time: ts}
		i = (i + 1) % numPages
	}
}

func drawCursor(xu *xgbutil.XUtil, win *Window, buf Buffer, page []byte, canvas Canvas) {
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
	w, h, _ := win.Dimensions()
	w = min(w, canvas.Width)
	h = min(h, canvas.Height)
	if pos.DstY < 0 || pos.DstX < 0 || int(pos.DstY) > h || int(pos.DstX) > w {
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
		invAlpha := 256 - (p >> 24)

		page[off+3] = 255
		page[off+2] = byte((alpha*uint32(byte(p>>16)) + invAlpha*uint32(page[off+2])) >> 8)
		page[off+1] = byte((alpha*uint32(byte(p>>8)) + invAlpha*uint32(page[off+1])) >> 8)
		page[off+0] = byte((alpha*uint32(byte(p>>0)) + invAlpha*uint32(page[off+0])) >> 8)
	}
}

func roundDuration(d, m time.Duration) time.Duration {
	if m <= 0 {
		return d
	}
	r := d % m
	if r < 0 {
		r = -r
		if r+r < m {
			return d + r
		}
		if d1 := d - m + r; d1 < d {
			return d1
		}
		return d // overflow
	}
	if r+r < m {
		return d - r
	}
	if d1 := d + m - r; d1 > d {
		return d1
	}
	return d // overflow
}
