package main

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/BurntSushi/xgb/composite"
	xshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/ghetzel/shmtool/shm" // TODO switch to pure Go implementation
)

func main() {
	fps := flag.Uint("fps", 60, "FPS")
	flag.Parse()

	log.Printf("Rendering at %d FPS", *fps)

	// XCompositeRedirectWindow update -> true
	xu, err := xgbutil.NewConn()
	if err != nil {
		// XXX proper error handling
		panic(err)
	}
	if err := composite.Init(xu.Conn()); err != nil {
		// XXX proper error handling
		panic(err)
	}
	if err := xshm.Init(xu.Conn()); err != nil {
		panic(err)
	}
	win := xproto.Window(23068730)
	if err := composite.RedirectWindowChecked(xu.Conn(), win, composite.RedirectAutomatic).Check(); err != nil {
		// XXX proper error handling

		// TODO handle BadAccess, triggered if another client is
		// already redirecting the window
		panic(err)
	}
	// XCompositeNameWindowPixmap
	pix, err := xproto.NewPixmapId(xu.Conn())
	if err != nil {
		// XXX
		panic(err)
	}
	composite.NameWindowPixmap(xu.Conn(), win, pix)

	// TODO free pixmap if window goes away or is resized, get new pixmap

	segID, err := xshm.NewSegId(xu.Conn())
	if err != nil {
		panic(err)
	}
	seg, err := shm.Create(1920 * 1080 * 4)
	if err != nil {
		panic(err)
	}
	if err := xshm.AttachChecked(xu.Conn(), segID, uint32(seg.Id), false).Check(); err != nil {
		panic(err)
	}
	data, err := seg.Attach()
	if err != nil {
		panic(err)
	}
	bufs := [][]byte{make([]byte, 1920*1080*4), make([]byte, 1920*1080*4)}
	i := 0
	ch := make(chan []byte)

	empty := make([]byte, 1920*1080*4)
	go func() {
		t := time.NewTicker(time.Second / time.Duration(*fps))
		for range t.C {
			select {
			case b := <-ch:
				os.Stdout.Write(b)
			default:
				log.Println("dropped frame")
				os.Stdout.Write(empty)
			}
		}
	}()

	for {
		// TODO get window's actual dimensions
		_, err := xshm.GetImage(xu.Conn(), xproto.Drawable(pix), 0, 0, 1920, 1080, 0xFFFFFFFF, xproto.ImageFormatZPixmap, segID, 0).Reply()
		if err != nil {
			panic(err)
		}
		copy(bufs[i], ((*[10920 * 1080 * 4]byte)(data)[:]))
		ch <- bufs[i]
		i = (i + 1) % 2
	}
}
