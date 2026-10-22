package mailarchive

import (
	"encoding/binary"
	"hash/fnv"
	"os"

	bolterrors "go.etcd.io/bbolt/errors"
)

// bbolt v1.5 uses the native-endian Bolt v2 file format. Read fixed-size
// metadata with ReadAt before bbolt can mmap it. A valid metadata checksum
// alone does not guarantee that its committed pages survived truncation.
func openSearchDatabaseFile(path string, flags int, perm os.FileMode) (*os.File, error) {
	f, err := openPrivateCacheFile(path, flags, perm)
	if err != nil {
		return nil, err
	}
	if err := checkSearchDatabaseBounds(f); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

type cacheMeta struct {
	pageSize, root, freelist, highWater, txID uint64
}

func readCacheMeta(f *os.File, offset int64, pageID uint64) (cacheMeta, bool) {
	var data [80]byte // 16-byte page header followed by 64-byte metadata.
	if _, err := f.ReadAt(data[:], offset); err != nil {
		return cacheMeta{}, false
	}
	order := binary.NativeEndian
	if order.Uint64(data[:8]) != pageID || order.Uint16(data[8:10]) != 4 ||
		order.Uint32(data[16:20]) != 0xed0cdaed || order.Uint32(data[20:24]) != 2 {
		return cacheMeta{}, false
	}
	hash := fnv.New64a()
	_, _ = hash.Write(data[16:72])
	if hash.Sum64() != order.Uint64(data[72:80]) {
		return cacheMeta{}, false
	}
	m := cacheMeta{
		pageSize: uint64(order.Uint32(data[24:28])), root: order.Uint64(data[32:40]),
		freelist: order.Uint64(data[48:56]), highWater: order.Uint64(data[56:64]), txID: order.Uint64(data[64:72]),
	}
	// This matches the page-size range bbolt probes when meta page 0 is bad.
	if m.pageSize < 1024 || m.pageSize > 16<<20 || m.pageSize&(m.pageSize-1) != 0 {
		return cacheMeta{}, false
	}
	if pageID == 1 && uint64(offset) != m.pageSize {
		return cacheMeta{}, false
	}
	return m, true
}

func checkSearchDatabaseBounds(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return nil
	} // New files are initialized by bbolt.
	first, validFirst := readCacheMeta(f, 0, 0)
	var metas []cacheMeta
	if validFirst {
		metas = append(metas, first)
		if second, ok := readCacheMeta(f, int64(first.pageSize), 1); ok {
			if second.pageSize != first.pageSize {
				return bolterrors.ErrInvalid
			}
			metas = append(metas, second)
		}
	} else {
		// Preserve bbolt's ability to recover from one damaged meta page, even
		// for databases created on systems with a different OS page size.
		for size := int64(1024); size <= 16<<20 && size+80 <= info.Size(); size <<= 1 {
			if second, ok := readCacheMeta(f, size, 1); ok {
				metas = append(metas, second)
				break
			}
		}
	}
	if len(metas) == 0 {
		return bolterrors.ErrInvalid
	}
	// Match bbolt's choice: highest transaction ID among checksum-valid metas.
	// Older generations may reference pages subsequently reclaimed/reused.
	m := metas[0]
	for _, candidate := range metas[1:] {
		if candidate.txID > m.txID {
			m = candidate
		}
	}
	// Division avoids overflow from corrupt page counts. The high-water
	// mark bounds every page committed by this metadata generation.
	if m.highWater < 4 || m.highWater > uint64(info.Size())/m.pageSize ||
		m.root < 2 || m.root >= m.highWater ||
		(m.freelist != ^uint64(0) && (m.freelist < 2 || m.freelist >= m.highWater)) {
		return bolterrors.ErrInvalid
	}
	for _, id := range []uint64{m.root, m.freelist} {
		if id == ^uint64(0) {
			continue
		}
		var page [16]byte
		if _, err := f.ReadAt(page[:], int64(id*m.pageSize)); err != nil {
			return bolterrors.ErrInvalid
		}
		overflow := uint64(binary.NativeEndian.Uint32(page[12:16]))
		if binary.NativeEndian.Uint64(page[:8]) != id || overflow >= m.highWater-id {
			return bolterrors.ErrInvalid
		}
	}
	return checkSearchDatabaseTree(f, m)
}

// Validate the reachable tree iteratively before bbolt's recursive cursor can
// follow it. Read only headers/elements and bucket roots, never message values.
func checkSearchDatabaseTree(f *os.File, m cacheMeta) error {
	type pageRef struct {
		offset, size, id uint64
		depth            int
		inline           bool
	}
	pending := []pageRef{{id: m.root}}
	seen := make(map[uint64]bool)
	order := binary.NativeEndian
	for len(pending) > 0 {
		ref := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if ref.depth > 64 {
			return bolterrors.ErrInvalid
		}
		var header [16]byte
		if !ref.inline {
			if ref.id < 2 || ref.id >= m.highWater || seen[ref.id] {
				return bolterrors.ErrInvalid
			}
			seen[ref.id] = true
			ref.offset = ref.id * m.pageSize
		}
		if _, err := f.ReadAt(header[:], int64(ref.offset)); err != nil {
			return bolterrors.ErrInvalid
		}
		flags := order.Uint16(header[8:10])
		count := uint64(order.Uint16(header[10:12]))
		overflow := uint64(order.Uint32(header[12:16]))
		if ref.inline {
			if flags != 2 || overflow != 0 || ref.size < 16 {
				return bolterrors.ErrInvalid
			}
		} else {
			if order.Uint64(header[:8]) != ref.id || overflow >= m.highWater-ref.id {
				return bolterrors.ErrInvalid
			}
			ref.size = (overflow + 1) * m.pageSize
		}
		if flags != 1 && flags != 2 || (flags == 1 && count == 0) || count*16 > ref.size-16 {
			return bolterrors.ErrInvalid
		}
		elements := make([]byte, count*16)
		if _, err := f.ReadAt(elements, int64(ref.offset+16)); err != nil {
			return bolterrors.ErrInvalid
		}
		for i := uint64(0); i < count; i++ {
			e := elements[i*16 : (i+1)*16]
			var pos, keySize, valueSize uint64
			if flags == 1 {
				pos, keySize = uint64(order.Uint32(e[:4])), uint64(order.Uint32(e[4:8]))
			} else {
				pos, keySize, valueSize = uint64(order.Uint32(e[4:8])), uint64(order.Uint32(e[8:12])), uint64(order.Uint32(e[12:16]))
			}
			start := 16 + i*16 + pos
			if start < 16+count*16 || start > ref.size || keySize == 0 || keySize > ref.size-start || valueSize > ref.size-start-keySize {
				return bolterrors.ErrInvalid
			}
			if flags == 1 {
				pending = append(pending, pageRef{id: order.Uint64(e[8:16]), depth: ref.depth + 1})
			} else if order.Uint32(e[:4])&1 != 0 {
				if valueSize < 16 {
					return bolterrors.ErrInvalid
				}
				value := ref.offset + start + keySize
				var bucket [16]byte
				if _, err := f.ReadAt(bucket[:], int64(value)); err != nil {
					return bolterrors.ErrInvalid
				}
				root := order.Uint64(bucket[:8])
				if root == 0 {
					if valueSize < 32 {
						return bolterrors.ErrInvalid
					}
					pending = append(pending, pageRef{offset: value + 16, size: valueSize - 16, inline: true, depth: ref.depth + 1})
				} else {
					pending = append(pending, pageRef{id: root, depth: ref.depth + 1})
				}
			}
		}
	}
	return nil
}
