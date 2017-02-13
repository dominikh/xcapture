// Package ebml implements an encoder for EBML.
package ebml

import (
	"encoding/binary"
	"io"
)

type Int int64
type Uint uint64
type Varint uint64
type Float float64 // XXX what about 10 bytes?
type String string
type UTF8 string
type Date struct{}
type Binary []byte

// XXX return minimal sizes
func (i Int) Size() int    { return 8 }
func (u Uint) Size() int   { return 8 }
func (f Float) Size() int  { return 8 }
func (u UTF8) Size() int   { return len(u) }
func (s String) Size() int { return len(s) }
func (b Binary) Size() int { return len(b) }

// XXX write minimal versions
func (i Int) Write(w io.Writer) error   { return binary.Write(w, binary.BigEndian, i) }
func (u Uint) Write(w io.Writer) error  { return binary.Write(w, binary.BigEndian, u) }
func (f Float) Write(w io.Writer) error { return binary.Write(w, binary.BigEndian, f) }
func (u UTF8) Write(w io.Writer) error {
	_, err := w.Write([]byte(u))
	return err
}
func (s String) Write(w io.Writer) error {
	_, err := w.Write([]byte(s))
	return err
}
func (b Binary) Write(w io.Writer) error {
	_, err := w.Write(b)
	return err
}

func (v Varint) Write(w io.Writer) error {
	// TODO(dh): in theory, there could be more than 8 bytes, which we
	// don't support
	bits := uint(bits(uint64(v)))

	bytes := (bits + 6) / 7
	// All ones has reserved meaning
	if 1<<(bytes*7)-1 == v {
		bytes++
	}
	marker := uint64(128>>(bytes-1)) << (8 * (bytes - 1))
	v |= Varint(marker)
	b := shortest(uint64(v))
	_, err := w.Write(b)
	return err
}

func (v Varint) Length() int {
	// TODO(dh): optimize this function
	bits := uint(bits(uint64(v)))

	bytes := (bits + 6) / 7
	// All ones has reserved meaning
	if 1<<(bytes*7)-1 == v {
		bytes++
	}
	marker := uint64(128>>(bytes-1)) << (8 * (bytes - 1))
	v |= Varint(marker)
	b := shortest(uint64(v))
	return len(b)
}

type Padding int

func (p Padding) Size() int { return int(p) }

func (p Padding) Write(w io.Writer) error {
	if w, ok := w.(io.Seeker); ok {
		_, err := w.Seek(int64(p), io.SeekCurrent)
		return err
	}
	b := make([]byte, p)
	_, err := w.Write(b)
	return err
}

type ElementID func(children ...Object) Element

func EBML(children ...Object) Element                 { return Element{0x1A45DFA3, children} }
func EBMLVersion(children ...Object) Element          { return Element{0x4286, children} }
func EBMLReadVersion(children ...Object) Element      { return Element{0x42F7, children} }
func EBMLMaxIDLength(children ...Object) Element      { return Element{0x42F2, children} }
func EBMLMaxSizeLength(children ...Object) Element    { return Element{0x42F3, children} }
func DocType(children ...Object) Element              { return Element{0x4282, children} }
func DocTypeVersion(children ...Object) Element       { return Element{0x4287, children} }
func DocTypeReadVersion(children ...Object) Element   { return Element{0x4285, children} }
func CRC32(children ...Object) Element                { return Element{0xBF, children} }
func Void(children ...Object) Element                 { return Element{0xEC, children} }
func SignatureSlot(children ...Object) Element        { return Element{0x1B538667, children} }
func SignatureAlgo(children ...Object) Element        { return Element{0x7E8A, children} }
func SignatureHash(children ...Object) Element        { return Element{0x7E9A, children} }
func SignaturePublicKey(children ...Object) Element   { return Element{0x7EA5, children} }
func Signature(children ...Object) Element            { return Element{0x7EB5, children} }
func SignatureElements(children ...Object) Element    { return Element{0x7E5B, children} }
func SignatureElementList(children ...Object) Element { return Element{0x7E7B, children} }
func SignedElement(children ...Object) Element        { return Element{0x6732, children} }

func bits(n uint64) int {
	bits := int(0)
	if n == 0 {
		n = 1
	}
	for n > 0 {
		n >>= 1
		bits++
	}
	return bits
}

func shortest(x uint64) []byte {
	if x == 0 {
		return []byte{}
	}
	bytes := (bits(x)+7)/8 - 1
	b := make([]byte, 0, bytes)
	for ; bytes >= 0; bytes-- {
		b = append(b, byte(x>>uint(bytes*8)))
	}
	return b
}

type Object interface {
	Size() int
	Write(w io.Writer) error
}

type Element struct {
	Class    uint64
	Children []Object
}

func classSize(class uint64) int {
	// TODO(dh): optimize this function
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, class)
	i := 0
	var v byte
	for i, v = range b {
		if v != 0 {
			break
		}
	}
	return 8 - i
}

func (e Element) Write(w io.Writer) error {
	// TODO use shortest()
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, e.Class)
	i := 0
	var v byte
	for i, v = range b {
		if v != 0 {
			break
		}
	}
	if i == len(b) {
		i--
	}
	if _, err := w.Write(b[i:]); err != nil {
		return err
	}
	if err := Varint(e.internalSize()).Write(w); err != nil {
		return err
	}
	for _, c := range e.Children {
		if err := c.Write(w); err != nil {
			return err
		}
	}
	return nil
}

func (e Element) Size() int {
	s := e.internalSize()
	s += Varint(s).Length()
	return classSize(e.Class) + s
}

func (e Element) internalSize() int {
	size := 0
	for _, c := range e.Children {
		size += c.Size()
	}
	return size
}

type Encoder struct {
	Err error
	w   *trackedWriter
}

type trackedWriter struct {
	pos int
	w   io.Writer
}

func (w *trackedWriter) Write(b []byte) (int, error) {
	n, err := w.w.Write(b)
	w.pos += n
	return n, err
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: &trackedWriter{w: w}}
}

func (e *Encoder) Position() int {
	return e.w.pos
}

// Emit writes the object and all of its possible children. It
// calculates the size automatically.
func (e *Encoder) Emit(obj Object) error {
	if e.Err != nil {
		return e.Err
	}
	e.Err = obj.Write(e.w)
	return e.Err
}

// EmitHeader writes an element header with the given ID and size. If
// the size is negative, the special "unknown" size will be used
// instead.
func (e *Encoder) EmitHeader(class ElementID, size int) (Reference, error) {
	if e.Err != nil {
		return Reference{}, e.Err
	}

	idPos := e.Position()

	id := class().Class
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, id)
	i := 0
	var v byte
	for i, v = range b {
		if v != 0 {
			break
		}
	}
	if i == len(b) {
		i--
	}
	if _, err := e.w.Write(b[i:]); err != nil {
		e.Err = err
		return Reference{}, e.Err
	}

	sizePos := e.Position()
	if size < 0 {
		if _, err := e.w.Write([]byte{255}); err != nil {
			e.Err = err
			return Reference{}, e.Err
		}
	} else {
		if err := Varint(size).Write(e.w); err != nil {
			e.Err = err
			return Reference{}, e.Err
		}
	}

	return Reference{
		ID:   idPos,
		Size: sizePos,
		Data: e.Position(),
	}, nil
}

type Reference struct {
	ID   int
	Size int
	Data int
}
