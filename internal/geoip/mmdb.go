package geoip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"os"
)

// A reader for the MaxMind DB format, which is what DB-IP publishes its free
// databases in.
//
// Written here rather than taken as a dependency for the reason the rest of
// olr is stdlib where it can be: the format is small and fixed (a binary
// search tree over address bits, then a typed data section), olr needs only
// the lookup half of it, and a library would bring a writer, reflection-based
// decoding and a module graph for a few hundred lines of reading. The
// specification is https://maxmind.github.io/MaxMind-DB/.
//
// The whole file is read into memory. The two databases olr uses — country
// and network owner — are a few megabytes each, and a router asks them about
// a handful of addresses.

var metadataMarker = []byte("\xab\xcd\xefMaxMind.com")

// DB is one opened database.
type DB struct {
	tree       []byte
	data       []byte
	nodeCount  uint
	recordSize uint
	ipv4Start  uint
	ipVersion  uint

	// Type is the database's own name for what it holds, e.g.
	// "DBIP-Country-Lite".
	Type string
	// Built is when the database was built, in Unix seconds.
	Built uint64
}

// Open reads and checks a database file.
func Open(path string) (*DB, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse checks a database already in memory.
func Parse(b []byte) (*DB, error) {
	// The metadata is at the end, after the last marker. The format caps it at
	// 128 KiB, so the marker is searched for only there.
	from := max(0, len(b)-128*1024)
	i := bytes.LastIndex(b[from:], metadataMarker)
	if i < 0 {
		return nil, errors.New("not a MaxMind DB file: no metadata marker")
	}
	metaStart := from + i + len(metadataMarker)
	meta, _, err := decoder{buf: b[metaStart:]}.decode(0, 0)
	if err != nil {
		return nil, fmt.Errorf("reading metadata: %w", err)
	}
	m, ok := meta.(map[string]any)
	if !ok {
		return nil, errors.New("metadata is not a map")
	}
	db := &DB{
		nodeCount:  uint(asUint(m["node_count"])),
		recordSize: uint(asUint(m["record_size"])),
		ipVersion:  uint(asUint(m["ip_version"])),
		Built:      asUint(m["build_epoch"]),
	}
	db.Type, _ = m["database_type"].(string)
	switch db.recordSize {
	case 24, 28, 32:
	default:
		return nil, fmt.Errorf("unsupported record size %d", db.recordSize)
	}
	treeSize := db.recordSize * 2 / 8 * db.nodeCount
	// 16 bytes of zeroes separate the tree from the data section.
	if treeSize+16 > uint(from+i) {
		return nil, errors.New("search tree runs past the end of the file")
	}
	db.tree = b[:treeSize]
	db.data = b[treeSize+16 : from+i]

	// An IPv6 database holds IPv4 addresses at ::/96, and every IPv4 lookup
	// starts at the node 96 zero bits down.
	if db.ipVersion == 6 {
		node := uint(0)
		for n := 0; n < 96 && node < db.nodeCount; n++ {
			node = db.record(node, 0)
		}
		db.ipv4Start = node
	}
	return db, nil
}

// Lookup returns the record for an address, or false when the database has
// none.
func (db *DB) Lookup(addr netip.Addr) (any, bool, error) {
	addr = addr.Unmap()
	var bits []byte
	node := uint(0)
	switch {
	case addr.Is4():
		a := addr.As4()
		bits = a[:]
		if db.ipVersion == 6 {
			node = db.ipv4Start
		}
	case addr.Is6():
		if db.ipVersion != 6 {
			return nil, false, nil
		}
		a := addr.As16()
		bits = a[:]
	default:
		return nil, false, nil
	}
	for i := 0; i < len(bits)*8 && node < db.nodeCount; i++ {
		bit := uint(bits[i/8]>>(7-uint(i%8))) & 1
		node = db.record(node, bit)
	}
	switch {
	case node == db.nodeCount:
		return nil, false, nil
	case node < db.nodeCount:
		return nil, false, errors.New("search tree ended inside itself")
	}
	off := node - db.nodeCount - 16
	v, _, err := decoder{buf: db.data}.decode(off, 0)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// record reads one of a node's two records: 0 is the left, taken for a zero
// bit, and 1 the right.
func (db *DB) record(node, side uint) uint {
	t := db.tree
	switch db.recordSize {
	case 24:
		b := node*6 + side*3
		if b+3 > uint(len(t)) {
			return db.nodeCount
		}
		return uint(t[b])<<16 | uint(t[b+1])<<8 | uint(t[b+2])
	case 28:
		b := node * 7
		if b+7 > uint(len(t)) {
			return db.nodeCount
		}
		// The middle byte holds the top nibble of both records.
		if side == 0 {
			return uint(t[b+3]&0xf0)<<20 | uint(t[b])<<16 | uint(t[b+1])<<8 | uint(t[b+2])
		}
		return uint(t[b+3]&0x0f)<<24 | uint(t[b+4])<<16 | uint(t[b+5])<<8 | uint(t[b+6])
	default:
		b := node*8 + side*4
		if b+4 > uint(len(t)) {
			return db.nodeCount
		}
		return uint(binary.BigEndian.Uint32(t[b:]))
	}
}

// decoder reads the data section's typed values.
type decoder struct {
	buf []byte
}

const (
	typeExtended = 0
	typePointer  = 1
	typeString   = 2
	typeDouble   = 3
	typeBytes    = 4
	typeUint16   = 5
	typeUint32   = 6
	typeMap      = 7
	typeInt32    = 8
	typeUint64   = 9
	typeUint128  = 10
	typeArray    = 11
	typeBool     = 14
	typeFloat    = 15
)

var errTruncated = errors.New("data section truncated")

// decode reads the value at off and returns it with the offset after it.
// Maps come back as map[string]any, arrays as []any, and every unsigned
// integer as uint64 (a uint128 too wide for one as nil).
func (d decoder) decode(off uint, depth int) (any, uint, error) {
	if depth > 32 {
		return nil, 0, errors.New("data nested too deeply")
	}
	if off >= uint(len(d.buf)) {
		return nil, 0, errTruncated
	}
	ctrl := d.buf[off]
	off++
	typ := int(ctrl >> 5)

	if typ == typePointer {
		ss, vvv := (ctrl>>3)&3, uint(ctrl&7)
		n := uint(ss) + 1
		if off+n > uint(len(d.buf)) {
			return nil, 0, errTruncated
		}
		b := d.buf[off : off+n]
		var p uint
		switch ss {
		case 0:
			p = vvv<<8 | uint(b[0])
		case 1:
			p = (vvv<<16 | uint(b[0])<<8 | uint(b[1])) + 2048
		case 2:
			p = (vvv<<24 | uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2])) + 526336
		default:
			p = uint(binary.BigEndian.Uint32(b))
		}
		v, _, err := d.decode(p, depth+1)
		return v, off + n, err
	}

	if typ == typeExtended {
		if off >= uint(len(d.buf)) {
			return nil, 0, errTruncated
		}
		typ = 7 + int(d.buf[off])
		off++
	}

	size := uint(ctrl & 0x1f)
	if size >= 29 {
		n := size - 28
		if off+n > uint(len(d.buf)) {
			return nil, 0, errTruncated
		}
		b := d.buf[off : off+n]
		switch n {
		case 1:
			size = 29 + uint(b[0])
		case 2:
			size = 285 + (uint(b[0])<<8 | uint(b[1]))
		default:
			size = 65821 + (uint(b[0])<<16 | uint(b[1])<<8 | uint(b[2]))
		}
		off += n
	}

	switch typ {
	case typeMap:
		m := make(map[string]any, min(size, 64))
		for range size {
			k, next, err := d.decode(off, depth+1)
			if err != nil {
				return nil, 0, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, 0, errors.New("map key is not a string")
			}
			v, after, err := d.decode(next, depth+1)
			if err != nil {
				return nil, 0, err
			}
			m[key] = v
			off = after
		}
		return m, off, nil
	case typeArray:
		a := make([]any, 0, min(size, 64))
		for range size {
			v, next, err := d.decode(off, depth+1)
			if err != nil {
				return nil, 0, err
			}
			a = append(a, v)
			off = next
		}
		return a, off, nil
	case typeBool:
		return size != 0, off, nil
	}

	if off+size > uint(len(d.buf)) {
		return nil, 0, errTruncated
	}
	b := d.buf[off : off+size]
	off += size
	switch typ {
	case typeString:
		return string(b), off, nil
	case typeBytes:
		return bytes.Clone(b), off, nil
	case typeDouble:
		if size != 8 {
			return nil, 0, errors.New("double is not 8 bytes")
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), off, nil
	case typeFloat:
		if size != 4 {
			return nil, 0, errors.New("float is not 4 bytes")
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), off, nil
	case typeInt32:
		var u uint32
		for _, c := range b {
			u = u<<8 | uint32(c)
		}
		return int64(int32(u)), off, nil
	case typeUint16, typeUint32, typeUint64, typeUint128:
		if size > 8 {
			// Nothing olr reads is this wide; the value is skipped, not refused.
			return nil, off, nil
		}
		var u uint64
		for _, c := range b {
			u = u<<8 | uint64(c)
		}
		return u, off, nil
	default:
		// The data cache container and the end marker are never a record's
		// value.
		return nil, 0, fmt.Errorf("unexpected data type %d", typ)
	}
}

func asUint(v any) uint64 {
	u, _ := v.(uint64)
	return u
}
