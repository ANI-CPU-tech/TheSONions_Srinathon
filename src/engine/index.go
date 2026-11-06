// In-memory hash index: key -> {offset, length, seq} of its latest value,
// plus dead-byte accounting. Values are never copied into the index.
// PR A2 (docs/SPEC.md sections 1 and 6).
package engine
