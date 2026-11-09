// Package streamcrypt seals a backup stream with AES-256-GCM.
//
// The layout is the same shape Databasus uses for stored backups: a fixed
// header, then length-prefixed chunks of at most 1 MiB. The magic is DMSXBK01,
// so a dmsx file is not presented as a Databasus file. The key is PBKDF2-SHA256
// of the master secret, the file id and a per-file salt. Each chunk gets its
// own nonce counter, so the base nonce is never reused.
package streamcrypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/google/uuid"
	"golang.org/x/crypto/pbkdf2"
)

const (
	magic       = "DMSXBK01"
	saltLen     = 32
	nonceLen    = 12
	fileIDLen   = 16
	reservedLen = 12
	headerLen   = len(magic) + saltLen + nonceLen + fileIDLen + reservedLen
	chunkSize   = 1 << 20
	keyLen      = 32
	iterations  = 100_000
)

// Writer encrypts into w. Close writes the final chunk.
type Writer struct {
	w      io.Writer
	aead   cipher.AEAD
	nonce  []byte
	index  uint32
	plain  []byte
	closed bool
}

func NewWriter(w io.Writer, master string, fileID uuid.UUID) (*Writer, error) {
	if master == "" {
		return nil, fmt.Errorf("master key is empty")
	}
	salt := make([]byte, saltLen)
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	aead, err := derive(master, fileID, salt)
	if err != nil {
		return nil, err
	}
	header := make([]byte, headerLen)
	copy(header, magic)
	copy(header[len(magic):], salt)
	copy(header[len(magic)+saltLen:], nonce)
	copy(header[len(magic)+saltLen+nonceLen:], fileID[:])
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &Writer{w: w, aead: aead, nonce: nonce, plain: make([]byte, 0, chunkSize)}, nil
}

func (e *Writer) Write(p []byte) (int, error) {
	if e.closed {
		return 0, fmt.Errorf("write on closed encrypting writer")
	}
	written := 0
	for len(p) > 0 {
		room := chunkSize - len(e.plain)
		n := min(room, len(p))
		e.plain = append(e.plain, p[:n]...)
		p = p[n:]
		written += n
		if len(e.plain) == chunkSize {
			if err := e.seal(e.plain); err != nil {
				return written, err
			}
			e.plain = e.plain[:0]
		}
	}
	return written, nil
}

func (e *Writer) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	if len(e.plain) == 0 && e.index > 0 {
		return nil
	}
	return e.seal(e.plain)
}

func (e *Writer) seal(plain []byte) error {
	nonce := e.chunkNonce()
	sealed := e.aead.Seal(nil, nonce, plain, nil)
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(len(sealed)))
	if _, err := e.w.Write(prefix[:]); err != nil {
		return err
	}
	_, err := e.w.Write(sealed)
	e.index++
	return err
}

func (e *Writer) chunkNonce() []byte {
	nonce := append([]byte(nil), e.nonce...)
	counter := binary.BigEndian.Uint32(nonce[nonceLen-4:]) + e.index
	binary.BigEndian.PutUint32(nonce[nonceLen-4:], counter)
	return nonce
}

// Reader decrypts a stream produced by Writer.
type Reader struct {
	r     io.Reader
	aead  cipher.AEAD
	nonce []byte
	index uint32
	plain []byte
	off   int
	done  bool
}

func NewReader(r io.Reader, master string) (*Reader, error) {
	header := make([]byte, headerLen)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, fmt.Errorf("read encryption header: %w", err)
	}
	if string(header[:len(magic)]) != magic {
		return nil, fmt.Errorf("not a dmsx encrypted stream")
	}
	salt := header[len(magic) : len(magic)+saltLen]
	nonce := append([]byte(nil), header[len(magic)+saltLen:len(magic)+saltLen+nonceLen]...)
	var fileID uuid.UUID
	copy(fileID[:], header[len(magic)+saltLen+nonceLen:len(magic)+saltLen+nonceLen+fileIDLen])
	aead, err := derive(master, fileID, salt)
	if err != nil {
		return nil, err
	}
	return &Reader{r: r, aead: aead, nonce: nonce}, nil
}

func (d *Reader) Read(p []byte) (int, error) {
	if d.off >= len(d.plain) {
		if d.done {
			return 0, io.EOF
		}
		if err := d.openNext(); err != nil {
			return 0, err
		}
	}
	n := copy(p, d.plain[d.off:])
	d.off += n
	return n, nil
}

func (d *Reader) openNext() error {
	var prefix [4]byte
	_, err := io.ReadFull(d.r, prefix[:])
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		d.done = true
		d.plain = nil
		d.off = 0
		return io.EOF
	}
	if err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n == 0 || n > uint32(chunkSize+d.aead.Overhead()) {
		return fmt.Errorf("encrypted chunk length %d is not valid", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(d.r, buf); err != nil {
		return err
	}
	nonce := append([]byte(nil), d.nonce...)
	counter := binary.BigEndian.Uint32(nonce[nonceLen-4:]) + d.index
	binary.BigEndian.PutUint32(nonce[nonceLen-4:], counter)
	plain, err := d.aead.Open(nil, nonce, buf, nil)
	if err != nil {
		return fmt.Errorf("decrypt chunk %d: %w", d.index, err)
	}
	d.index++
	d.plain = plain
	d.off = 0
	return nil
}

func derive(master string, fileID uuid.UUID, salt []byte) (cipher.AEAD, error) {
	material := []byte(master + fileID.String())
	key := pbkdf2.Key(material, salt, iterations, keyLen, sha256.New)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
