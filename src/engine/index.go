// In-memory hash index: key -> {offset, length, seq} of its latest value,
// plus live-byte accounting. Values are never copied into the index.
// (docs/SPEC.md sections 1 and 6.)
package engine

// entry locates the latest value of one key in the log file.
type entry struct {
	off  int64  // file offset of the first value byte
	size uint32 // value length in bytes
	seq  uint64 // seq of the record that wrote it (compaction keeps it)
	cost uint32 // log bytes this value occupies; live until overwritten
}

// index maps key to entry. live is the sum of cost over all keys, so
// dead bytes = records area - live. Callers hold the DB lock.
type index struct {
	m    map[string]entry
	live int64
}

func newIndex() index { return index{m: make(map[string]entry)} }

// put points key at a new value; the old value's bytes become dead.
func (x *index) put(key []byte, e entry) {
	if old, ok := x.m[string(key)]; ok {
		x.live -= int64(old.cost)
	}
	x.m[string(key)] = e
	x.live += int64(e.cost)
}

// del removes key; its value's bytes become dead.
func (x *index) del(key []byte) {
	if old, ok := x.m[string(key)]; ok {
		x.live -= int64(old.cost)
		delete(x.m, string(key))
	}
}
