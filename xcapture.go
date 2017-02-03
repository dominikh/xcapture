package main

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/xgb/composite"
	xshm "github.com/BurntSushi/xgb/shm"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
	"github.com/ghetzel/shmtool/shm" // TODO switch to pure Go implementation
)

func main() {
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
	interval := time.Second / 60
	t := time.NewTicker(interval)
	last := time.Now()
	for range t.C {
		now := time.Now()
		d := now.Sub(last)
		if d-interval > interval/20 {
			fmt.Fprintf(os.Stderr, "%s late\n", d-interval)
		}
		last = now
		// TODO get window's actual dimensions
		r, err := xshm.GetImage(xu.Conn(), xproto.Drawable(pix), 0, 0, 1920, 1080, 0xFFFFFFFF, xproto.ImageFormatZPixmap, segID, 0).Reply()
		_ = r
		//r, err := xproto.GetImage(xu.Conn(), xproto.ImageFormatZPixmap, xproto.Drawable(pix), 0, 0, 1920, 1080, 0xFFFFFFFF).Reply()
		if err != nil {
			// XXX
			panic(err)
		}
		os.Stdout.Write((*[10920 * 1080 * 4]byte)(data)[:])
		// os.Stdout.Write(r.Data)
	}
}
