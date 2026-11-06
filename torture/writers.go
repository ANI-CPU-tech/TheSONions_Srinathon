// At least two concurrent synchronous writers: seeded op stream (puts,
// deletes, batches), unique tagged values w<writer>o<opid>:, one pipe mutex.
// PR L1.
package main
