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
	win := flag.Uint("win", 0, "Window ID")
	flag.Parse()

	log.Printf("Rendering at %d FPS", *fps)

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
	seg, err := shm.Create(1920 * 1080 * 4)
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
			log.Fatal("Could not fetch window contents:", err)
		}
		copy(bufs[i], ((*[10920 * 1080 * 4]byte)(data)[:]))
		ch <- bufs[i]
		i = (i + 1) % 2
	}
}
