//go:build linux

package video

import (
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Minimal V4L2 MMAP-streaming capture, hand-rolled on x/sys/unix so it stays pure
// Go (no cgo, no kernel-header coupling) and cross-compiles from any host. We use
// only the handful of ioctls a UVC capture needs: QUERYCAP, S_FMT, REQBUFS,
// QUERYBUF, QBUF/DQBUF, STREAMON/OFF.
//
// The request numbers below are the fixed kernel _IOC encodings (type 'V'); the
// size nibble in each must equal sizeof the matching struct, so Open() asserts the
// Go struct layouts at runtime — a mismatch surfaces as a clear error instead of
// memory corruption.

const (
	reqWidth   = 640
	reqHeight  = 480
	numBuffers = 4

	bufTypeVideoCapture = 1 // V4L2_BUF_TYPE_VIDEO_CAPTURE
	memoryMMAP          = 1 // V4L2_MEMORY_MMAP
	fieldAny            = 0 // V4L2_FIELD_ANY

	capVideoCapture = 0x00000001 // V4L2_CAP_VIDEO_CAPTURE
	capStreaming    = 0x04000000 // V4L2_CAP_STREAMING

	vidiocQUERYCAP  = 0x80685600 // VIDIOC_QUERYCAP    (_IOR 'V',0,  size 104)
	vidiocSFMT      = 0xc0d05605 // VIDIOC_S_FMT       (_IOWR'V',5,  size 208)
	vidiocREQBUFS   = 0xc0145608 // VIDIOC_REQBUFS     (_IOWR'V',8,  size 20)
	vidiocQUERYBUF  = 0xc0585609 // VIDIOC_QUERYBUF    (_IOWR'V',9,  size 88)
	vidiocQBUF      = 0xc058560f // VIDIOC_QBUF        (_IOWR'V',15, size 88)
	vidiocDQBUF     = 0xc0585611 // VIDIOC_DQBUF       (_IOWR'V',17, size 88)
	vidiocSTREAMON  = 0x40045612 // VIDIOC_STREAMON    (_IOW 'V',18, size 4)
	vidiocSTREAMOFF = 0x40045613 // VIDIOC_STREAMOFF   (_IOW 'V',19, size 4)
)

func fourcc(a, b, c, d byte) uint32 {
	return uint32(a) | uint32(b)<<8 | uint32(c)<<16 | uint32(d)<<24
}

var (
	pixFmtMJPEG = fourcc('M', 'J', 'P', 'G')
	pixFmtYUYV  = fourcc('Y', 'U', 'Y', 'V')
)

// --- kernel ABI structs (sizes asserted in Open) ---

type v4l2Capability struct {
	Driver       [16]byte
	Card         [32]byte
	BusInfo      [32]byte
	Version      uint32
	Capabilities uint32
	DeviceCaps   uint32
	Reserved     [3]uint32
} // 104

type v4l2PixFormat struct {
	Width        uint32
	Height       uint32
	PixelFormat  uint32
	Field        uint32
	BytesPerLine uint32
	SizeImage    uint32
	Colorspace   uint32
	Priv         uint32
	Flags        uint32
	Enc          uint32 // ycbcr_enc / hsv_enc union
	Quantization uint32
	XferFunc     uint32
} // 48

type v4l2Format struct {
	Type uint32
	_    uint32 // the fmt union is 8-byte aligned (some members hold pointers)
	pix  v4l2PixFormat
	_    [208 - 8 - 48]byte // pad the 200-byte fmt union tail
} // 208

type v4l2RequestBuffers struct {
	Count        uint32
	Type         uint32
	Memory       uint32
	Capabilities uint32
	Flags        uint8
	Reserved     [3]uint8
} // 20

type v4l2Timeval struct {
	Sec  int64
	Usec int64
} // 16

type v4l2Timecode struct {
	Type     uint32
	Flags    uint32
	Frames   uint8
	Seconds  uint8
	Minutes  uint8
	Hours    uint8
	Userbits [4]uint8
} // 16

type v4l2Buffer struct {
	Index     uint32
	Type      uint32
	BytesUsed uint32
	Flags     uint32
	Field     uint32
	_         uint32 // align Timestamp to 8
	Timestamp v4l2Timeval
	Timecode  v4l2Timecode
	Sequence  uint32
	Memory    uint32
	M         uint64 // union m: offset (MMAP) / userptr / planes / fd
	Length    uint32
	Reserved2 uint32
	RequestFD int32
	_         uint32 // tail pad
} // 88

// Capture owns the V4L2 fd, the mmap'd buffers, and the grab goroutine that
// decodes each dequeued frame into buf.
type Capture struct {
	fd      int
	buf     *Buffer
	buffers [][]byte
	w, h    int
	mjpeg   bool
	stop    chan struct{}
	done    chan struct{}

	mu      sync.Mutex
	lastErr string
}

// ListDevices returns the V4L2 nodes present, e.g. ["/dev/video0", ...]. A UVC
// dongle exposes several /dev/videoN nodes; we list them all and let the user pick
// the right one in Settings (the capture node is usually the lowest-numbered).
func ListDevices() []string {
	paths, _ := filepath.Glob("/dev/video*")
	sort.Strings(paths)
	return paths
}

// Open starts capturing from path. It requests MJPEG @ 640x480, then reads back the
// format the driver actually set and decodes accordingly (MJPEG, or the
// near-universal YUYV fallback). Capture is best-effort: a single bad frame is
// recorded (see Err) and skipped, never fatal.
func Open(path string) (*Capture, error) {
	if unsafe.Sizeof(v4l2Capability{}) != 104 || unsafe.Sizeof(v4l2Format{}) != 208 ||
		unsafe.Sizeof(v4l2RequestBuffers{}) != 20 || unsafe.Sizeof(v4l2Buffer{}) != 88 {
		return nil, fmt.Errorf("v4l2: struct layout mismatch (build bug)")
	}

	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	var bufs [][]byte
	ok := false
	defer func() {
		if !ok {
			for _, b := range bufs {
				unix.Munmap(b)
			}
			unix.Close(fd)
		}
	}()

	// Confirm this node is a streaming capture device — the Deck has several
	// /dev/videoN nodes and only some are the dongle's capture node.
	var capb v4l2Capability
	if err := ioctl(fd, vidiocQUERYCAP, unsafe.Pointer(&capb)); err != nil {
		return nil, fmt.Errorf("querycap %s: %w", path, err)
	}
	caps := capb.Capabilities
	if capb.DeviceCaps != 0 {
		caps = capb.DeviceCaps
	}
	if caps&capVideoCapture == 0 {
		return nil, fmt.Errorf("%s: not a video-capture device", path)
	}
	if caps&capStreaming == 0 {
		return nil, fmt.Errorf("%s: no streaming I/O support", path)
	}

	// Request MJPEG; the driver fills the struct with what it actually set.
	f := v4l2Format{Type: bufTypeVideoCapture}
	f.pix.Width = reqWidth
	f.pix.Height = reqHeight
	f.pix.PixelFormat = pixFmtMJPEG
	f.pix.Field = fieldAny
	if err := ioctl(fd, vidiocSFMT, unsafe.Pointer(&f)); err != nil {
		return nil, fmt.Errorf("set format %s: %w", path, err)
	}
	w, h := int(f.pix.Width), int(f.pix.Height)
	var mjpeg bool
	switch f.pix.PixelFormat {
	case pixFmtMJPEG:
		mjpeg = true
	case pixFmtYUYV:
		mjpeg = false
	default:
		return nil, fmt.Errorf("%s: unsupported pixel format 0x%08x (need MJPEG or YUYV)",
			path, f.pix.PixelFormat)
	}

	// Allocate driver buffers and map them.
	req := v4l2RequestBuffers{Count: numBuffers, Type: bufTypeVideoCapture, Memory: memoryMMAP}
	if err := ioctl(fd, vidiocREQBUFS, unsafe.Pointer(&req)); err != nil {
		return nil, fmt.Errorf("reqbufs %s: %w", path, err)
	}
	if req.Count < 2 {
		return nil, fmt.Errorf("%s: insufficient buffers (%d)", path, req.Count)
	}
	bufs = make([][]byte, req.Count)
	for i := uint32(0); i < req.Count; i++ {
		qb := v4l2Buffer{Index: i, Type: bufTypeVideoCapture, Memory: memoryMMAP}
		if err := ioctl(fd, vidiocQUERYBUF, unsafe.Pointer(&qb)); err != nil {
			return nil, fmt.Errorf("querybuf %s: %w", path, err)
		}
		mm, err := unix.Mmap(fd, int64(qb.M), int(qb.Length),
			unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("mmap %s: %w", path, err)
		}
		bufs[i] = mm
		if err := ioctl(fd, vidiocQBUF, unsafe.Pointer(&qb)); err != nil {
			return nil, fmt.Errorf("qbuf %s: %w", path, err)
		}
	}

	bt := int32(bufTypeVideoCapture)
	if err := ioctl(fd, vidiocSTREAMON, unsafe.Pointer(&bt)); err != nil {
		return nil, fmt.Errorf("streamon %s: %w", path, err)
	}

	c := &Capture{
		fd: fd, buf: &Buffer{}, buffers: bufs, w: w, h: h, mjpeg: mjpeg,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	ok = true
	go c.grab()
	return c, nil
}

// grab polls the device and decodes each dequeued frame until Close signals stop.
// O_NONBLOCK + a polling timeout lets it notice the stop signal promptly without a
// separate wakeup channel into a blocking DQBUF.
func (c *Capture) grab() {
	defer close(c.done)
	for {
		select {
		case <-c.stop:
			return
		default:
		}
		pfd := []unix.PollFd{{Fd: int32(c.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(pfd, 300)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			c.setErr("poll: " + err.Error())
			return
		}
		if n == 0 {
			continue // timeout — loop back to re-check stop
		}

		buf := v4l2Buffer{Type: bufTypeVideoCapture, Memory: memoryMMAP}
		if err := ioctl(c.fd, vidiocDQBUF, unsafe.Pointer(&buf)); err != nil {
			if err == unix.EAGAIN {
				continue
			}
			c.setErr("dqbuf: " + err.Error())
			return
		}

		data := c.buffers[buf.Index][:buf.BytesUsed]
		var f *Frame
		if c.mjpeg {
			if ff, derr := decodeJPEG(data); derr == nil {
				f = ff
			} else {
				c.setErr("jpeg decode: " + derr.Error())
			}
		} else {
			f = decodeYUYV(data, c.w, c.h)
		}
		if f != nil {
			c.buf.Set(f)
		}

		if err := ioctl(c.fd, vidiocQBUF, unsafe.Pointer(&buf)); err != nil {
			c.setErr("qbuf: " + err.Error())
			return
		}
	}
}

func (c *Capture) setErr(msg string) {
	c.mu.Lock()
	c.lastErr = msg
	c.mu.Unlock()
}

// Buffer is where the UI reads the latest decoded frame.
func (c *Capture) Buffer() *Buffer { return c.buf }

// Info reports the negotiated capture format, e.g. "640x480 MJPEG", for the OSD
// debug readout.
func (c *Capture) Info() string {
	codec := "YUYV"
	if c.mjpeg {
		codec = "MJPEG"
	}
	return fmt.Sprintf("%dx%d %s", c.w, c.h, codec)
}

// Err returns the most recent capture error, or "" if none.
func (c *Capture) Err() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Close stops the stream, waits for the grab goroutine, and releases everything.
func (c *Capture) Close() error {
	close(c.stop)
	<-c.done
	bt := int32(bufTypeVideoCapture)
	ioctl(c.fd, vidiocSTREAMOFF, unsafe.Pointer(&bt))
	for _, b := range c.buffers {
		unix.Munmap(b)
	}
	return unix.Close(c.fd)
}

// ioctl issues a V4L2 ioctl with a struct argument. The uintptr(arg) conversion
// happens in the syscall call itself (the vet-approved unsafe.Pointer pattern).
func ioctl(fd int, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
