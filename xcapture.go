package main

import (
	"os"

	"github.com/BurntSushi/xgb/composite"
	"github.com/BurntSushi/xgb/xproto"
	"github.com/BurntSushi/xgbutil"
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

	for {
		// TODO get window's actual dimensions
		r, err := xproto.GetImage(xu.Conn(), xproto.ImageFormatZPixmap, xproto.Drawable(pix), 0, 0, 1920, 1080, 0xFFFFFFFF).Reply()
		if err != nil {
			// XXX
			panic(err)
		}
		os.Stdout.Write(r.Data)
	}
}
