package engine

// crashpoint SIGKILLs the process when NINELIVES_CRASH_AT equals name, on
// the Nth hit (N from NINELIVES_CRASH_AFTER, default 1). See docs/SPEC.md
// section 8. Stub only: the real hook lands in PR A5.
func crashpoint(name string) {}
