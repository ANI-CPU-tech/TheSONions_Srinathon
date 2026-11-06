// Write path (docs/SPEC.md section 4): validate, disk-full guard, encode one
// buffer, WriteAt, Sync, update index, return seq. Failure rule: truncate +
// sync, else enter ErrFailed state. PR A2/A3. Every disk-changing syscall
// gets a // CRASH: comment.
package engine
